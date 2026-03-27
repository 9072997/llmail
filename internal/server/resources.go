package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	imaplib "github.com/jpennington/llmail/internal/imap"
	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerAttachmentResources() {
	s.mcp.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"llmail://{account}/{folder}/{uid}/attachments/{part_number}",
			"Email Attachment",
			mcp.WithTemplateDescription("Fetch an email attachment by account, folder, message UID, and MIME part number."),
		),
		s.handleAttachmentResource,
	)
}

func (s *Server) handleAttachmentResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	uri := req.Params.URI

	// Parse the URI: llmail://{account}/{folder}/{uid}/attachments/{part_number}
	account, folder, uid, partNumber, err := parseAttachmentURI(uri)
	if err != nil {
		return nil, err
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("connecting to account: %w", err)
	}
	defer s.pool.Put(account, c)

	content, contentType, _, err := imaplib.FetchAttachmentContent(ctx, c, folder, uid, partNumber)
	if err != nil {
		s.pool.Discard(account, c)
		return nil, fmt.Errorf("fetching attachment: %w", err)
	}

	// Return as text for text/* types, binary for everything else
	if strings.HasPrefix(contentType, "text/") {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      uri,
				MIMEType: contentType,
				Text:     string(content),
			},
		}, nil
	}

	return []mcp.ResourceContents{
		mcp.BlobResourceContents{
			URI:      uri,
			MIMEType: contentType,
			Blob:     base64.StdEncoding.EncodeToString(content),
		},
	}, nil
}

// parseAttachmentURI parses a llmail:// attachment URI.
// Format: llmail://{account}/{folder}/{uid}/attachments/{part_number}
// The folder component may contain slashes (e.g. [Gmail]/All Mail).
func parseAttachmentURI(uri string) (account, folder string, uid uint32, partNumber int, err error) {
	const prefix = "llmail://"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", 0, 0, fmt.Errorf("invalid URI scheme: %s", uri)
	}

	path := uri[len(prefix):]

	// Find the /attachments/ segment from the end
	attachIdx := strings.LastIndex(path, "/attachments/")
	if attachIdx < 0 {
		return "", "", 0, 0, fmt.Errorf("invalid attachment URI: missing /attachments/ segment")
	}

	partStr := path[attachIdx+len("/attachments/"):]
	partNumber, err = strconv.Atoi(partStr)
	if err != nil {
		return "", "", 0, 0, fmt.Errorf("invalid part number: %s", partStr)
	}

	beforeAttachments := path[:attachIdx]

	// The last segment before /attachments/ is the UID
	lastSlash := strings.LastIndex(beforeAttachments, "/")
	if lastSlash < 0 {
		return "", "", 0, 0, fmt.Errorf("invalid attachment URI: missing UID")
	}

	uidStr := beforeAttachments[lastSlash+1:]
	uidVal, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		return "", "", 0, 0, fmt.Errorf("invalid UID: %s", uidStr)
	}

	accountAndFolder := beforeAttachments[:lastSlash]

	// The first segment is the account, the rest is the folder
	firstSlash := strings.Index(accountAndFolder, "/")
	if firstSlash < 0 {
		return "", "", 0, 0, fmt.Errorf("invalid attachment URI: missing folder")
	}

	account = accountAndFolder[:firstSlash]
	folder = accountAndFolder[firstSlash+1:]

	return account, folder, uint32(uidVal), partNumber, nil
}
