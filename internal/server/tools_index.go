package server

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerIndexTools() {
	s.mcp.AddTool(
		mcp.NewTool("search_local_index",
			mcp.WithDescription(`Full-text search against the local Bleve index. Supports Bleve query syntax:
- Field queries: from:alice, subject:meeting
- Phrases: "exact phrase match"
- Boolean: +required -excluded
- Wildcards: meet*
- Fuzzy: meeting~1
- Date ranges: date:>2024-01-01`),
			mcp.WithString("query", mcp.Required(), mcp.Description("Bleve query string")),
			mcp.WithString("account", mcp.Description("Account to search (omit for all accounts)")),
			mcp.WithString("folder", mcp.Description("Folder to search")),
			mcp.WithString("date_from", mcp.Description("Filter results from date (YYYY-MM-DD)")),
			mcp.WithString("date_to", mcp.Description("Filter results to date (YYYY-MM-DD)")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 20, max 100)")),
			mcp.WithNumber("offset", mcp.Description("Result offset for pagination")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Search Local Index",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleSearchLocalIndex,
	)

	s.mcp.AddTool(
		mcp.NewTool("index_status",
			mcp.WithDescription("Check local index sync progress and coverage."),
			mcp.WithString("account", mcp.Description("Account to check (omit for all)")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Index Status",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleIndexStatus,
	)
}

func (s *Server) handleSearchLocalIndex(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.indexer == nil {
		return errorResult("local index is not enabled"), nil
	}

	args := req.GetArguments()
	query, _ := args["query"].(string)
	if query == "" {
		return errorResult("query parameter is required"), nil
	}

	account := stringFrom(args, "account")
	folder := stringFrom(args, "folder")
	dateFrom := stringFrom(args, "date_from")
	dateTo := stringFrom(args, "date_to")
	limit := intFrom(args, "limit")
	offset := intFrom(args, "offset")

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	results, err := s.indexer.Search(query, account, folder, dateFrom, dateTo, limit, offset)
	if err != nil {
		return errorResult("index search: " + err.Error()), nil
	}

	return s.checkGuard(formatIndexSearchResult(results)), nil
}

func (s *Server) handleIndexStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.indexer == nil {
		return errorResult("local index is not enabled"), nil
	}

	args := req.GetArguments()
	account := stringFrom(args, "account")
	return textResult(s.indexer.StatusText(account)), nil
}
