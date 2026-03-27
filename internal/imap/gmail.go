package imap

import (
	"context"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const GmailExtensionCap imap.Cap = "X-GM-EXT-1"

func HasGmailExtensions(caps imap.CapSet) bool {
	_, ok := caps[GmailExtensionCap]
	return ok
}

type GmailSearchParams struct {
	Query       string      `json:"query"`
	Folder      string      `json:"folder,omitempty"`
	Limit       int         `json:"limit"`
	Offset      int         `json:"offset"`
	DetailLevel DetailLevel `json:"detail_level"`
}

// GmailSearch performs a Gmail X-GM-RAW search using the vendored RawKeys support.
func GmailSearch(ctx context.Context, c *imapclient.Client, params GmailSearchParams) (*SearchResult, error) {
	folder := params.Folder
	if folder == "" {
		folder = "[Gmail]/All Mail"
	}

	selectCmd := c.Select(folder, &imap.SelectOptions{ReadOnly: true})
	if _, err := selectCmd.Wait(); err != nil {
		return nil, fmt.Errorf("selecting folder %q: %w", folder, err)
	}

	// Use RawKeys to emit X-GM-RAW directly in the SEARCH command
	criteria := &imap.SearchCriteria{
		RawKeys: []string{fmt.Sprintf(`X-GM-RAW "%s"`, escapeGmailQuery(params.Query))},
	}

	searchData, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("gmail search: %w", err)
	}

	allUIDs := searchData.AllUIDs()
	totalMatches := len(allUIDs)

	if totalMatches == 0 {
		return &SearchResult{TotalMatches: 0, Folder: folder, Messages: []MessageSummary{}}, nil
	}

	return paginateAndFetch(ctx, c, allUIDs, totalMatches, params.Limit, params.Offset, params.DetailLevel, folder, &FetchParams{GmailLabels: true})
}

func escapeGmailQuery(query string) string {
	return strings.ReplaceAll(query, `"`, `\"`)
}
