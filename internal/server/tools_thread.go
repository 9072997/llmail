package server

import (
	"context"

	imaplib "github.com/jpennington/llmail/internal/imap"
	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerThreadTool() {
	s.mcp.AddTool(
		mcp.NewTool("get_thread",
			mcp.WithDescription(`Retrieve all messages in a conversation thread. Provide any message UID from the thread.
Uses IMAP THREAD command when available, otherwise falls back to References/In-Reply-To header chasing.
Note: Only searches within the specified folder. For Gmail, use [Gmail]/All Mail for cross-label threading.`),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Required(), mcp.Description("Folder containing the message")),
			mcp.WithNumber("uid", mcp.Required(), mcp.Description("UID of any message in the thread")),
			mcp.WithString("detail_level", mcp.Description("Detail level: headers, full (default: full)")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Get Thread",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleGetThread,
	)
}

func (s *Server) handleGetThread(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	folder := stringFrom(args, "folder")
	if folder == "" {
		return errorResult("folder parameter is required"), nil
	}
	uid := uint32From(args, "uid")
	if uid == 0 {
		return errorResult("uid parameter is required"), nil
	}

	level := imaplib.ParseDetailLevel(stringFrom(args, "detail_level"), imaplib.DetailFull)

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	result, err := imaplib.GetThread(ctx, c, folder, uid, level)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("getting thread: " + err.Error()), nil
	}

	return s.checkGuard(formatThreadResult(result)), nil
}
