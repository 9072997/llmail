package server

import (
	"context"

	imaplib "github.com/jpennington/llmail/internal/imap"
	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerGmailTools() {
	s.mcp.AddTool(
		mcp.NewTool("gmail_search",
			mcp.WithDescription(`Search using Gmail's native query syntax via X-GM-RAW. Supports Gmail search operators:
- from:, to:, subject:, cc:, bcc:
- has:attachment, has:drive, has:document, has:spreadsheet
- in:inbox, in:sent, in:trash, in:anywhere
- is:unread, is:read, is:starred, is:important
- label:, category:
- newer_than:2d, older_than:1y
- filename:pdf, filename:doc
- size:, larger:, smaller:
- Boolean: OR, AND, NOT, - (exclude), "" (exact phrase)
- {term1 term2} (group OR), -(exclude)`),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name (must be a Gmail account)")),
			mcp.WithString("query", mcp.Required(), mcp.Description("Gmail search query string")),
			mcp.WithString("folder", mcp.Description("Folder to search (default: [Gmail]/All Mail)")),
			mcp.WithNumber("limit", mcp.Description("Max results to return (default 20, max 100)")),
			mcp.WithNumber("offset", mcp.Description("Result offset for pagination (default 0)")),
			mcp.WithString("detail_level", mcp.Description("Detail level: headers, full (default: headers)")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Gmail Search",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleGmailSearch,
	)
}

func (s *Server) handleGmailSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account, _ := args["account"].(string)
	if account == "" {
		return errorResult("account parameter is required"), nil
	}

	acc, ok := s.cfg.GetAccount(account)
	if !ok {
		return errorResult("unknown account: " + account), nil
	}
	if !acc.Capabilities.GmailExtensions {
		return errorResult("account " + account + " does not have Gmail extensions enabled"), nil
	}

	query, _ := args["query"].(string)
	if query == "" {
		return errorResult("query parameter is required"), nil
	}

	params := imaplib.GmailSearchParams{
		Query:       query,
		Folder:      stringFrom(args, "folder"),
		Limit:       intFrom(args, "limit"),
		Offset:      intFrom(args, "offset"),
		DetailLevel: imaplib.ParseDetailLevel(stringFrom(args, "detail_level"), imaplib.DetailHeaders),
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	result, err := imaplib.GmailSearch(ctx, c, params)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("gmail search: " + err.Error()), nil
	}

	return s.checkGuard(formatGmailSearchResult(result)), nil
}
