package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/jpennington/llmail/internal/config"
	"github.com/jpennington/llmail/internal/guard"
	imaplib "github.com/jpennington/llmail/internal/imap"
	"github.com/jpennington/llmail/internal/indexer"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type Server struct {
	cfg     *config.Config
	pool    *imaplib.Pool
	mcp     *server.MCPServer
	indexer *indexer.Indexer
	guard   *guard.Guard

	// trashCache caches detected trash folder names per account
	trashCache sync.Map // map[string]string
}

func New(cfg *config.Config) (*Server, error) {
	mcpServer := server.NewMCPServer(
		"llmail",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
	)

	pool := imaplib.NewPool(imaplib.PoolConfig{
		Config: cfg,
		GetPassword: func(account string) (string, error) {
			acc, ok := cfg.GetAccount(account)
			if !ok {
				return "", fmt.Errorf("unknown account: %s", account)
			}
			return config.RetrievePassword(account, acc.PasswordStorage, acc.EncryptedPassword)
		},
	})

	s := &Server{
		cfg:  cfg,
		pool: pool,
		mcp:  mcpServer,
	}

	// Initialize prompt injection guard
	if cfg.Guard.Enabled {
		g, err := guard.New(cfg.Guard)
		if err != nil {
			slog.Warn("Prompt injection guard failed to initialize, continuing without protection", "error", err)
		} else {
			s.guard = g
			slog.Info("Prompt injection guard enabled", "threshold", cfg.Guard.Threshold)
		}
	}

	// Always register core tools (read-only)
	s.registerCoreTools()

	// Always register write tools
	s.registerWriteTools()

	// Always register thread tool
	s.registerThreadTool()

	// Always register help tool
	s.registerHelpTool()

	// Register attachment resources
	s.registerAttachmentResources()

	// Conditionally register Gmail tools for accounts with Gmail extensions
	hasGmail := false
	for _, name := range cfg.AccountNames() {
		acc, _ := cfg.GetAccount(name)
		if acc.Capabilities.GmailExtensions {
			hasGmail = true
			break
		}
	}
	if hasGmail {
		s.registerGmailTools()
		slog.Info("Gmail search tool registered")
	}

	// Conditionally register index tools and start indexer
	if cfg.Index.Enabled && cfg.Index.Mode != "none" {
		idx, err := indexer.New(cfg)
		if err != nil {
			slog.Warn("Failed to initialize indexer, index tools will not be available", "error", err)
		} else {
			s.indexer = idx
			s.registerIndexTools()
			slog.Info("Local index tools registered")
		}
	}

	return s, nil
}

// MCPServer returns the underlying MCP server for in-process client usage.
func (s *Server) MCPServer() *server.MCPServer { return s.mcp }

// Pool returns the IMAP connection pool.
func (s *Server) Pool() *imaplib.Pool { return s.pool }

// Indexer returns the local indexer, or nil if indexing is disabled.
func (s *Server) Indexer() *indexer.Indexer { return s.indexer }

// Start starts the background indexer without binding stdio.
func (s *Server) Start(ctx context.Context) {
	if s.indexer != nil {
		s.indexer.Start(ctx, s.pool)
		slog.Info("Background indexer started")
	}
}

// Shutdown stops the indexer and closes all IMAP connections.
func (s *Server) Shutdown() { s.shutdown() }

// Serve starts the indexer and listens on stdio for MCP messages.
func (s *Server) Serve(ctx context.Context) error {
	s.Start(ctx)

	stdioServer := server.NewStdioServer(s.mcp)

	slog.Info("llmail MCP server starting on stdio")
	err := stdioServer.Listen(ctx, os.Stdin, os.Stdout)

	s.shutdown()
	return err
}

func (s *Server) shutdown() {
	slog.Info("Shutting down llmail")
	if s.indexer != nil {
		s.indexer.Stop()
	}
	s.guard.Close()
	s.pool.CloseAll()
}

// checkGuard runs the prompt injection guard. If injection is detected,
// returns a blocked errorResult. Otherwise returns a successful textResult
// with guard scan metadata attached in _meta for chat debug display.
// When the guard is nil/disabled, returns a plain textResult.
func (s *Server) checkGuard(text string) *mcp.CallToolResult {
	scan, err := s.guard.Check(text)
	if err != nil {
		return errorResult(err.Error())
	}

	result := textResult(text)
	if s.guard != nil {
		result.Meta = &mcp.Meta{
			AdditionalFields: map[string]any{
				"guardScore": fmt.Sprintf("%.1f%%", scan.Confidence*100),
			},
		}
	}
	return result
}

// getTrashFolder returns the cached trash folder for an account, detecting it if needed.
func (s *Server) getTrashFolder(ctx context.Context, account string, c *imapclient.Client) (string, error) {
	if cached, ok := s.trashCache.Load(account); ok {
		return cached.(string), nil
	}

	trashFolder, err := imaplib.FindTrashFolder(ctx, c)
	if err != nil {
		return "", err
	}

	s.trashCache.Store(account, trashFolder)
	return trashFolder, nil
}
