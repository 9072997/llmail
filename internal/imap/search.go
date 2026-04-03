package imap

import (
	"context"
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

type SearchResult struct {
	TotalMatches int              `json:"total_matches"`
	Offset       int              `json:"offset"`
	Folder       string           `json:"folder"`
	Messages     []MessageSummary `json:"messages"`
}

type MessageSummary struct {
	UID         uint32           `json:"uid"`
	From        string           `json:"from"`
	To          []string         `json:"to"`
	CC          []string         `json:"cc,omitempty"`
	Subject     string           `json:"subject"`
	Date        string           `json:"date"`
	Flags       []string         `json:"flags,omitempty"`
	GmailLabels []string         `json:"gmail_labels,omitempty"`
	TextBody    string           `json:"text_body,omitempty"`
	Unsubscribe *UnsubscribeInfo `json:"unsubscribe,omitempty"`
}

// RawSearchParams holds parameters for a raw IMAP SEARCH query.
type RawSearchParams struct {
	Folder      string      `json:"folder"`
	Query       string      `json:"query"`
	Limit       int         `json:"limit"`
	Offset      int         `json:"offset"`
	DetailLevel DetailLevel `json:"detail_level"`
}

// RawSearch executes a raw IMAP SEARCH command using RawKeys.
// The query string is passed directly to the IMAP server as search key atoms.
func RawSearch(ctx context.Context, c *imapclient.Client, params RawSearchParams, fp *FetchParams) (*SearchResult, error) {
	folder := params.Folder
	if folder == "" {
		folder = "INBOX"
	}

	selectCmd := c.Select(folder, &imap.SelectOptions{ReadOnly: true})
	if _, err := selectCmd.Wait(); err != nil {
		return nil, fmt.Errorf("selecting folder %q: %w", folder, err)
	}

	criteria := &imap.SearchCriteria{}
	if params.Query != "" {
		criteria.RawKeys = []string{params.Query}
	}

	searchData, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("searching: %w", err)
	}

	allUIDs := searchData.AllUIDs()
	totalMatches := len(allUIDs)

	if totalMatches == 0 {
		return &SearchResult{TotalMatches: 0, Folder: folder, Messages: []MessageSummary{}}, nil
	}

	return paginateAndFetch(ctx, c, allUIDs, totalMatches, params.Limit, params.Offset, params.DetailLevel, folder, fp)
}

// ListMessages lists all messages in a folder with pagination.
func ListMessages(ctx context.Context, c *imapclient.Client, folder string, limit, offset int, level DetailLevel, fp *FetchParams) (*SearchResult, error) {
	if folder == "" {
		folder = "INBOX"
	}

	selectCmd := c.Select(folder, &imap.SelectOptions{ReadOnly: true})
	if _, err := selectCmd.Wait(); err != nil {
		return nil, fmt.Errorf("selecting folder %q: %w", folder, err)
	}

	// Search with empty criteria matches ALL
	criteria := &imap.SearchCriteria{}
	searchData, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}

	allUIDs := searchData.AllUIDs()
	totalMatches := len(allUIDs)

	if totalMatches == 0 {
		return &SearchResult{TotalMatches: 0, Folder: folder, Messages: []MessageSummary{}}, nil
	}

	return paginateAndFetch(ctx, c, allUIDs, totalMatches, limit, offset, level, folder, fp)
}

// FetchParams holds optional parameters for paginateAndFetch.
type FetchParams struct {
	GmailLabels bool
	PreferHTML  bool
}

// paginateAndFetch handles UID reversal, pagination, and fetching at the given detail level.
func paginateAndFetch(ctx context.Context, c *imapclient.Client, allUIDs []imap.UID, totalMatches, limit, offset int, level DetailLevel, folder string, params *FetchParams) (*SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)
	offset = max(offset, 0)

	// Reverse UIDs for newest-first ordering
	for i, j := 0, len(allUIDs)-1; i < j; i, j = i+1, j-1 {
		allUIDs[i], allUIDs[j] = allUIDs[j], allUIDs[i]
	}

	if offset >= len(allUIDs) {
		return &SearchResult{TotalMatches: totalMatches, Offset: offset, Folder: folder, Messages: []MessageSummary{}}, nil
	}

	end := min(offset+limit, len(allUIDs))
	pageUIDs := allUIDs[offset:end]

	uidSet := imap.UIDSet{}
	for _, uid := range pageUIDs {
		uidSet.AddNum(uid)
	}

	gmailLabels := params != nil && params.GmailLabels
	preferHTML := params != nil && params.PreferHTML
	messages, err := FetchByLevel(ctx, c, uidSet, level, gmailLabels, preferHTML)
	if err != nil {
		return nil, err
	}

	return &SearchResult{
		TotalMatches: totalMatches,
		Offset:       offset,
		Folder:       folder,
		Messages:     messages,
	}, nil
}
