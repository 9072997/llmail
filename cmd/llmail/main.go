package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jpennington/llmail/internal/chat"
	"github.com/jpennington/llmail/internal/config"
	"github.com/jpennington/llmail/internal/guard"
	"github.com/jpennington/llmail/internal/imap"
	"github.com/jpennington/llmail/internal/indexer"
	"github.com/jpennington/llmail/internal/llm"
	"github.com/jpennington/llmail/internal/server"
	"github.com/jpennington/llmail/internal/setup"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"
)

var (
	cfgPath string
	profile string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "llmail",
		Short: "MCP server for IMAP email search",
		Long: "llmail is an MCP server that exposes email search tools over IMAP,\n" +
			"supporting multiple accounts, Gmail extensions, and local full-text indexing.\n\n" +
			"License: GPL-2.0-or-later <https://www.gnu.org/licenses/gpl-2.0.html>\n",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			config.SetProfile(profile)
		},
	}

	rootCmd.SilenceUsage = true

	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path (default: $XDG_CONFIG_HOME/llmail/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&profile, "profile", "", "named profile for isolated config, credentials, and index (default: \"default\")")

	rootCmd.AddCommand(serveCmd())
	rootCmd.AddCommand(chatCmd())
	rootCmd.AddCommand(setupCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(reindexCmd())
	rootCmd.AddCommand(guardCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio transport)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Set up logging to stderr (stdout is for MCP stdio transport)
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))
			slog.SetDefault(logger)

			srv, err := server.New(cfg)
			if err != nil {
				return fmt.Errorf("creating server: %w", err)
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			return srv.Serve(ctx)
		},
	}
}

func chatCmd() *cobra.Command {
	var debug bool
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Interactive chat with an LLM that can manage your email",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if cfg.LLM.Provider == "" {
				return fmt.Errorf("no LLM configured; run 'llmail setup' to configure")
			}

			logLevel := slog.LevelWarn
			if debug {
				logLevel = slog.LevelDebug
			}
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: logLevel,
			}))
			slog.SetDefault(logger)

			srv, err := server.New(cfg)
			if err != nil {
				return fmt.Errorf("creating server: %w", err)
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			srv.Start(ctx)
			defer srv.Shutdown()

			mcpClient, err := client.NewInProcessClient(srv.MCPServer())
			if err != nil {
				return fmt.Errorf("creating MCP client: %w", err)
			}
			defer mcpClient.Close()

			if err := mcpClient.Start(ctx); err != nil {
				return fmt.Errorf("starting MCP client: %w", err)
			}

			initReq := mcp.InitializeRequest{}
			initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
			initReq.Params.ClientInfo = mcp.Implementation{
				Name:    "llmail-chat",
				Version: "1.0.0",
			}
			if _, err := mcpClient.Initialize(ctx, initReq); err != nil {
				return fmt.Errorf("initializing MCP client: %w", err)
			}

			provider, err := llm.NewProvider(cfg.LLM)
			if err != nil {
				return fmt.Errorf("creating LLM provider: %w", err)
			}

			session := chat.NewChatSession(provider, mcpClient, cfg, srv.Indexer(), srv.Pool(), debug)
			return session.Run(ctx)
		},
	}
	cmd.Flags().BoolVar(&debug, "debug", false, "Print full MCP tool request/response payloads")
	return cmd
}

func setupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive setup wizard for configuring accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := setup.RunWizard()
			return err
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show index status and account info",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			fmt.Println("Accounts:")
			for _, name := range cfg.AccountNames() {
				acc, _ := cfg.GetAccount(name)
				gmail := ""
				if acc.Capabilities.GmailExtensions {
					gmail = " [Gmail]"
				}
				fmt.Printf("  %s: %s@%s:%d%s\n", name, acc.Username, acc.Host, acc.Port, gmail)
			}

			if !cfg.Index.Enabled || cfg.Index.Mode == "none" {
				fmt.Println("\nLocal index: disabled")
				return nil
			}

			idx, err := indexer.New(cfg)
			if err != nil {
				return fmt.Errorf("opening index: %w", err)
			}

			fmt.Print(idx.StatusText(""))

			return nil
		},
	}
}

func reindexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the local search index from scratch",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if !cfg.Index.Enabled || cfg.Index.Mode == "none" {
				return fmt.Errorf("local index is not enabled in config")
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))
			slog.SetDefault(logger)

			idx, err := indexer.New(cfg)
			if err != nil {
				return fmt.Errorf("creating indexer: %w", err)
			}

			pool := imap.NewPool(imap.PoolConfig{
				Config: cfg,
				GetPassword: func(account string) (string, error) {
					acc, ok := cfg.GetAccount(account)
					if !ok {
						return "", fmt.Errorf("unknown account: %s", account)
					}
					return config.RetrievePassword(account, acc.PasswordStorage, acc.EncryptedPassword)
				},
			})
			defer pool.CloseAll()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			fmt.Println("Starting full reindex...")
			if err := idx.Reindex(ctx, pool); err != nil {
				return fmt.Errorf("reindex: %w", err)
			}

			fmt.Println("Reindex complete.")
			return nil
		},
	}
}

func guardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guard",
		Short: "Manage the prompt injection guard model",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "download",
		Short: "Download the prompt injection guard model",
		RunE: func(cmd *cobra.Command, args []string) error {
			modelPath := guard.DefaultModelPath()
			if cfg, err := config.Load(cfgPath); err == nil && cfg.Guard.ModelPath != "" {
				modelPath = cfg.Guard.ModelPath
			}

			fmt.Printf("Downloading Prompt Guard 2 22M to %s...\n", modelPath)
			err := guard.DownloadModel(modelPath, func(file string) {
				fmt.Printf("  Downloading %s...\n", file)
			})
			if err != nil {
				return fmt.Errorf("downloading model: %w", err)
			}

			fmt.Println("Done. Enable the guard in config.yaml with:\n\nguard:\n  enabled: true")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "test [text]",
		Short: "Test the guard with sample text",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			guardCfg := config.GuardConfig{
				Enabled:   true,
				Threshold: 0.80,
			}
			if cfg, err := config.Load(cfgPath); err == nil {
				if cfg.Guard.ModelPath != "" {
					guardCfg.ModelPath = cfg.Guard.ModelPath
				}
				if cfg.Guard.Threshold > 0 {
					guardCfg.Threshold = cfg.Guard.Threshold
				}
			}

			g, err := guard.New(guardCfg)
			if err != nil {
				return fmt.Errorf("loading guard model: %w", err)
			}
			defer g.Close()

			result, err := g.Scan(args[0])
			if err != nil {
				return fmt.Errorf("scanning: %w", err)
			}

			if result.IsMalicious {
				fmt.Printf("MALICIOUS (confidence: %.1f%%)\n", result.Confidence*100)
			} else {
				fmt.Printf("BENIGN (confidence: %.1f%%)\n", result.Confidence*100)
			}
			return nil
		},
	})

	return cmd
}
