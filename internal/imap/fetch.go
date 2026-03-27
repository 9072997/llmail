package imap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
)

type FullMessage struct {
	UID         uint32            `json:"uid"`
	From        string            `json:"from"`
	To          []string          `json:"to"`
	CC          []string          `json:"cc,omitempty"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	Subject     string            `json:"subject"`
	Date        string            `json:"date"`
	Flags       []string          `json:"flags"`
	TextBody    string            `json:"text_body,omitempty"`
	HTMLBody    string            `json:"html_body,omitempty"`
	Attachments []AttachmentInfo  `json:"attachments,omitempty"`
	RawHeaders  map[string]string `json:"raw_headers,omitempty"`
	Unsubscribe    *UnsubscribeInfo  `json:"unsubscribe,omitempty"`
	LLMailCreated  bool              `json:"llmail_created,omitempty"`
}

type AttachmentInfo struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        uint32 `json:"size"`
	PartNumber  string `json:"part_number"`
	ContentID   string `json:"content_id,omitempty"`
	ResourceURI string `json:"resource_uri,omitempty"`
}

// FetchByLevel fetches messages at the specified detail level.
// If gmailLabels is true, X-GM-LABELS will be requested (requires X-GM-EXT-1).
func FetchByLevel(ctx context.Context, c *imapclient.Client, uidSet imap.UIDSet, level DetailLevel, gmailLabels bool) ([]MessageSummary, error) {
	fetchOptions := FetchOptionsByLevel(level)
	fetchOptions.GmailLabels = gmailLabels

	msgs, err := c.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetching messages: %w", err)
	}

	var summaries []MessageSummary
	for _, msg := range msgs {
		summary := MessageSummary{
			UID: uint32(msg.UID),
		}

		if env := msg.Envelope; env != nil {
			summary.Subject = env.Subject
			summary.Date = env.Date.Format("2006-01-02T15:04:05Z07:00")
			if len(env.From) > 0 {
				summary.From = formatAddress(env.From[0])
			}
			for _, addr := range env.To {
				summary.To = append(summary.To, formatAddress(addr))
			}
			for _, addr := range env.Cc {
				summary.CC = append(summary.CC, formatAddress(addr))
			}
		}

		if len(msg.GmailLabels) > 0 {
			summary.GmailLabels = msg.GmailLabels
		}

		if level == DetailHeaders {
			summaries = append(summaries, summary)
			continue
		}

		// Standard and Full levels include flags
		for _, f := range msg.Flags {
			summary.Flags = append(summary.Flags, string(f))
		}

		for _, bs := range msg.BodySection {
			if bs.Bytes == nil {
				continue
			}
			summary.TextBody = extractFullText(bs.Bytes)
			summary.Unsubscribe = extractUnsubscribe(bs.Bytes)
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}

// FetchSummaries fetches message summaries (standard detail level).
// Kept for backward compatibility.
func FetchSummaries(ctx context.Context, c *imapclient.Client, uidSet imap.UIDSet) ([]MessageSummary, error) {
	return FetchByLevel(ctx, c, uidSet, DetailFull, false)
}

func FetchMessage(ctx context.Context, c *imapclient.Client, folder string, uid uint32, preferHTML bool, includeRawHeaders bool) (*FullMessage, error) {
	selectCmd := c.Select(folder, &imap.SelectOptions{ReadOnly: true})
	if _, err := selectCmd.Wait(); err != nil {
		return nil, fmt.Errorf("selecting folder %q: %w", folder, err)
	}

	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(uid))

	fetchOptions := &imap.FetchOptions{
		Envelope:   true,
		Flags:      true,
		UID:        true,
		RFC822Size: true,
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
	result := &FullMessage{
		UID: uint32(msg.UID),
	}

	if env := msg.Envelope; env != nil {
		result.Subject = env.Subject
		result.Date = env.Date.Format("2006-01-02T15:04:05Z07:00")
		if len(env.From) > 0 {
			result.From = formatAddress(env.From[0])
		}
		if len(env.ReplyTo) > 0 {
			result.ReplyTo = formatAddress(env.ReplyTo[0])
		}
		for _, addr := range env.To {
			result.To = append(result.To, formatAddress(addr))
		}
		for _, addr := range env.Cc {
			result.CC = append(result.CC, formatAddress(addr))
		}
	}

	for _, f := range msg.Flags {
		result.Flags = append(result.Flags, string(f))
	}

	for _, bs := range msg.BodySection {
		if bs.Bytes != nil {
			parseMIME(bs.Bytes, result, includeRawHeaders)
		}
	}

	return result, nil
}

// FetchAttachmentContent fetches a specific attachment part from a message.
func FetchAttachmentContent(ctx context.Context, c *imapclient.Client, folder string, uid uint32, partNumber int) ([]byte, string, string, error) {
	selectCmd := c.Select(folder, &imap.SelectOptions{ReadOnly: true})
	if _, err := selectCmd.Wait(); err != nil {
		return nil, "", "", fmt.Errorf("selecting folder %q: %w", folder, err)
	}

	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(uid))

	// Fetch the full body and parse to find the specific part
	fetchOptions := &imap.FetchOptions{
		UID: true,
		BodySection: []*imap.FetchItemBodySection{
			{Peek: true},
		},
	}

	msgs, err := c.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return nil, "", "", fmt.Errorf("fetching message: %w", err)
	}

	if len(msgs) == 0 {
		return nil, "", "", fmt.Errorf("message UID %d not found", uid)
	}

	for _, bs := range msgs[0].BodySection {
		if bs.Bytes == nil {
			continue
		}
		content, contentType, filename, err := extractPart(bs.Bytes, partNumber)
		if err != nil {
			return nil, "", "", err
		}
		return content, contentType, filename, nil
	}

	return nil, "", "", fmt.Errorf("no body content found for message UID %d", uid)
}

func extractPart(body []byte, targetPart int) ([]byte, string, string, error) {
	reader, err := mail.CreateReader(bytes.NewReader(body))
	if err != nil {
		return nil, "", "", fmt.Errorf("parsing message: %w", err)
	}
	defer reader.Close()

	partNum := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", "", fmt.Errorf("reading part: %w", err)
		}
		partNum++

		if partNum == targetPart {
			var ct, filename string
			switch h := part.Header.(type) {
			case *mail.AttachmentHeader:
				ct, _, _ = h.ContentType()
				filename, _ = h.Filename()
			case *mail.InlineHeader:
				ct, _, _ = h.ContentType()
			}
			content, err := io.ReadAll(part.Body)
			if err != nil {
				return nil, "", "", fmt.Errorf("reading part content: %w", err)
			}
			return content, ct, filename, nil
		}
	}

	return nil, "", "", fmt.Errorf("part %d not found", targetPart)
}

func parseMIME(body []byte, result *FullMessage, includeRawHeaders bool) {
	reader, err := mail.CreateReader(bytes.NewReader(body))
	if err != nil && !message.IsUnknownCharset(err) {
		result.TextBody = string(body)
		return
	}
	defer reader.Close()

	var listUnsub, listUnsubPost string
	h := reader.Header
	fields := h.Fields()
	for fields.Next() {
		key := fields.Key()
		val := fields.Value()
		switch strings.ToLower(key) {
		case "list-unsubscribe":
			listUnsub = val
		case "list-unsubscribe-post":
			listUnsubPost = val
		case strings.ToLower(LLMailCreatedHeader):
			if strings.EqualFold(strings.TrimSpace(val), "true") {
				result.LLMailCreated = true
			}
		}
		if includeRawHeaders {
			if result.RawHeaders == nil {
				result.RawHeaders = make(map[string]string)
			}
			result.RawHeaders[key] = val
		}
	}
	result.Unsubscribe = ParseUnsubscribeHeaders(listUnsub, listUnsubPost)

	partNum := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) {
			break
		}
		partNum++

		switch h := part.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			bodyBytes, err := io.ReadAll(part.Body)
			if err != nil {
				continue
			}
			switch {
			case strings.HasPrefix(ct, "text/plain"):
				result.TextBody = string(bodyBytes)
			case strings.HasPrefix(ct, "text/html"):
				result.HTMLBody = string(bodyBytes)
			default:
				// Inline non-text parts (e.g. images referenced by cid: in HTML)
				info := AttachmentInfo{
					ContentType: ct,
					Size:        uint32(len(bodyBytes)),
					PartNumber:  fmt.Sprintf("%d", partNum),
				}
				if cid := h.Get("Content-Id"); cid != "" {
					info.ContentID = cid
				}
				result.Attachments = append(result.Attachments, info)
			}
		case *mail.AttachmentHeader:
			filename, _ := h.Filename()
			ct, _, _ := h.ContentType()
			bodyBytes, _ := io.ReadAll(part.Body)
			info := AttachmentInfo{
				Filename:    filename,
				ContentType: ct,
				Size:        uint32(len(bodyBytes)),
				PartNumber:  fmt.Sprintf("%d", partNum),
			}
			if cid := h.Get("Content-Id"); cid != "" {
				info.ContentID = cid
			}
			result.Attachments = append(result.Attachments, info)
		}
	}
}

// extractFullText extracts the full plain text body from a message.
// Falls back to raw HTML if no text/plain part exists.
func extractFullText(body []byte) string {
	reader, err := mail.CreateReader(bytes.NewReader(body))
	if err != nil && !message.IsUnknownCharset(err) {
		return string(body)
	}
	defer reader.Close()

	var htmlFallback string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) {
			break
		}
		if h, ok := part.Header.(*mail.InlineHeader); ok {
			ct, _, _ := h.ContentType()
			if strings.HasPrefix(ct, "text/plain") {
				bodyBytes, err := io.ReadAll(part.Body)
				if err != nil {
					break
				}
				return string(bodyBytes)
			}
			if strings.HasPrefix(ct, "text/html") && htmlFallback == "" {
				bodyBytes, err := io.ReadAll(part.Body)
				if err != nil {
					continue
				}
				htmlFallback = string(bodyBytes)
			}
		}
	}
	return htmlFallback
}

// extractUnsubscribe parses List-Unsubscribe headers from raw RFC822 bytes.
func extractUnsubscribe(body []byte) *UnsubscribeInfo {
	reader, err := mail.CreateReader(bytes.NewReader(body))
	if err != nil && !message.IsUnknownCharset(err) {
		return nil
	}
	defer reader.Close()

	var listUnsub, listUnsubPost string
	fields := reader.Header.Fields()
	for fields.Next() {
		switch strings.ToLower(fields.Key()) {
		case "list-unsubscribe":
			listUnsub = fields.Value()
		case "list-unsubscribe-post":
			listUnsubPost = fields.Value()
		}
	}
	return ParseUnsubscribeHeaders(listUnsub, listUnsubPost)
}

func formatAddress(addr imap.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s@%s>", addr.Name, addr.Mailbox, addr.Host)
	}
	return fmt.Sprintf("%s@%s", addr.Mailbox, addr.Host)
}
