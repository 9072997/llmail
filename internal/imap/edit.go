package imap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"
)

// LLMailCreatedHeader is the header used to mark messages created by llmail.
const LLMailCreatedHeader = "X-LLMail-Created"

// EditParams holds the fields that can be updated when editing a message.
// Only non-zero fields are applied; omitted fields retain their original values.
type EditParams struct {
	To       []string `json:"to,omitempty"`
	CC       []string `json:"cc,omitempty"`
	Subject  string   `json:"subject,omitempty"`
	Body     string   `json:"body,omitempty"`
	HTMLBody string   `json:"html_body,omitempty"`
}

// EditResult holds the result of an edit operation.
type EditResult struct {
	OldUID uint32 `json:"old_uid"`
	NewUID uint32 `json:"new_uid"`
}

// EditMessage replaces a message with an edited version.
// The folder must already be selected in read-write mode.
// Only messages with the X-LLMail-Created header can be edited.
func EditMessage(ctx context.Context, c *imapclient.Client, folder string, uid uint32, params EditParams) (*EditResult, error) {
	// Fetch the original message (full body + flags)
	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(uid))

	fetchOptions := &imap.FetchOptions{
		Envelope: true,
		Flags:    true,
		UID:      true,
		BodySection: []*imap.FetchItemBodySection{
			{Peek: true},
		},
	}

	msgs, err := c.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetching message: %w", err)
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("message UID %d not found", uid)
	}

	msg := msgs[0]

	// Find the raw body bytes
	var rawBody []byte
	for _, bs := range msg.BodySection {
		if bs.Bytes != nil {
			rawBody = bs.Bytes
			break
		}
	}
	if rawBody == nil {
		return nil, fmt.Errorf("message UID %d has no body", uid)
	}

	// Verify X-LLMail-Created header
	if !hasLLMailHeader(rawBody) {
		return nil, fmt.Errorf("message was not created by llmail and cannot be edited")
	}

	// Build the original AppendParams from the fetched message
	env := msg.Envelope
	if env == nil {
		return nil, fmt.Errorf("message UID %d has no envelope", uid)
	}

	original := AppendParams{
		Folder:  folder,
		Subject: env.Subject,
		Date:    env.Date.Format("2006-01-02"),
	}
	if len(env.From) > 0 {
		original.From = formatAddress(env.From[0])
	}
	for _, addr := range env.To {
		original.To = append(original.To, formatAddress(addr))
	}
	for _, addr := range env.Cc {
		original.CC = append(original.CC, formatAddress(addr))
	}

	// Extract text and HTML bodies from the original
	original.Body, original.HTMLBody = extractBodies(rawBody)

	// Apply edits - only override fields that are provided
	if len(params.To) > 0 {
		original.To = params.To
	}
	if len(params.CC) > 0 {
		original.CC = params.CC
	}
	if params.Subject != "" {
		original.Subject = params.Subject
	}
	if params.Body != "" {
		original.Body = params.Body
	}
	if params.HTMLBody != "" {
		original.HTMLBody = params.HTMLBody
	}

	// Preserve original flags
	var flagStrs []string
	for _, f := range msg.Flags {
		flagStrs = append(flagStrs, string(f))
	}
	original.Flags = flagStrs

	// Append the edited message
	appendResult, err := AppendMessage(ctx, c, original)
	if err != nil {
		return nil, fmt.Errorf("appending edited message: %w", err)
	}

	// Delete the old message (STORE \Deleted + UID EXPUNGE)
	storeFlags := &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagDeleted},
	}
	if _, err := c.Store(uidSet, storeFlags, nil).Collect(); err != nil {
		return nil, fmt.Errorf("marking old message deleted: %w", err)
	}

	if _, err := c.UIDExpunge(uidSet).Collect(); err != nil {
		return nil, fmt.Errorf("expunging old message: %w", err)
	}

	return &EditResult{
		OldUID: uid,
		NewUID: appendResult.UID,
	}, nil
}

// hasLLMailHeader checks if raw message bytes contain the X-LLMail-Created header.
func hasLLMailHeader(body []byte) bool {
	reader, err := mail.CreateReader(bytes.NewReader(body))
	if err != nil {
		return false
	}
	defer reader.Close()

	value := reader.Header.Get(LLMailCreatedHeader)
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

// extractBodies extracts text/plain and text/html bodies from raw message bytes.
func extractBodies(body []byte) (textBody, htmlBody string) {
	reader, err := mail.CreateReader(bytes.NewReader(body))
	if err != nil {
		return string(body), ""
	}
	defer reader.Close()

	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		if h, ok := part.Header.(*mail.InlineHeader); ok {
			ct, _, _ := h.ContentType()
			data, err := io.ReadAll(part.Body)
			if err != nil {
				continue
			}
			switch {
			case strings.HasPrefix(ct, "text/plain"):
				textBody = string(data)
			case strings.HasPrefix(ct, "text/html"):
				htmlBody = string(data)
			}
		}
	}
	return
}
