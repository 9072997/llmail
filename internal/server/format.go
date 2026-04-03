package server

import (
	"fmt"
	"strings"

	imaplib "github.com/jpennington/llmail/internal/imap"
	"github.com/jpennington/llmail/internal/indexer"
	"github.com/mark3labs/mcp-go/mcp"
)

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: msg,
			},
		},
	}
}

func textResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: msg,
			},
		},
	}
}

// --- List/metadata formatters ---

func formatAccounts(accounts []accountInfo) string {
	if len(accounts) == 0 {
		return "no accounts configured"
	}
	var b strings.Builder
	for i, a := range accounts {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "Account: %s\n", a.Name)
		fmt.Fprintf(&b, "  Host: %s:%d\n", a.Host, a.Port)
		fmt.Fprintf(&b, "  User: %s\n", a.Username)
		var caps []string
		if a.Capabilities.Gmail {
			caps = append(caps, "gmail")
		}
		if a.Capabilities.Sort {
			caps = append(caps, "sort")
		}
		if a.Capabilities.Thread {
			caps = append(caps, "thread")
		}
		if len(caps) > 0 {
			fmt.Fprintf(&b, "  Capabilities: %s\n", strings.Join(caps, ", "))
		}
		if a.IndexEnabled {
			b.WriteString("  Local index: enabled\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatFolders(folders []imaplib.FolderInfo) string {
	if len(folders) == 0 {
		return "no folders"
	}
	// Find max folder name length for alignment, capped at 40
	maxName := 6 // len("FOLDER")
	for _, f := range folders {
		if l := len(f.Name); l > maxName {
			maxName = l
		}
	}
	if maxName > 40 {
		maxName = 40
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-*s  %5s  %6s\n", maxName, "FOLDER", "TOTAL", "UNSEEN")
	for _, f := range folders {
		name := f.Name
		if len(name) > 40 {
			name = name[:37] + "..."
		}
		fmt.Fprintf(&b, "%-*s  %5d  %6d\n", maxName, name, f.Total, f.Unseen)
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- Message list formatters ---

func formatSearchResult(result *imaplib.SearchResult) string {
	if result == nil || result.TotalMatches == 0 {
		return "0 matches"
	}
	var b strings.Builder
	count := len(result.Messages)
	fmt.Fprintf(&b, "%d matches (showing %d)\n", result.TotalMatches, count)
	for _, m := range result.Messages {
		b.WriteByte('\n')
		formatMessageSummary(&b, &m)
	}
	if remaining := result.TotalMatches - result.Offset - count; remaining > 0 {
		fmt.Fprintf(&b, "\n(%d more messages after these, use offset/limit to paginate)\n", remaining)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatGmailSearchResult(result *imaplib.SearchResult) string {
	if result == nil || result.TotalMatches == 0 {
		return "0 matches"
	}
	var b strings.Builder
	count := len(result.Messages)
	fmt.Fprintf(&b, "%d matches (showing %d) | Folder: %s\n", result.TotalMatches, count, result.Folder)
	for _, m := range result.Messages {
		b.WriteByte('\n')
		formatMessageSummary(&b, &m)
	}
	if remaining := result.TotalMatches - result.Offset - count; remaining > 0 {
		fmt.Fprintf(&b, "\n(%d more messages after these, use offset/limit to paginate)\n", remaining)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatSingleSummary(m *imaplib.MessageSummary) string {
	var b strings.Builder
	formatMessageSummary(&b, m)
	return strings.TrimRight(b.String(), "\n")
}

func formatMessageSummary(b *strings.Builder, m *imaplib.MessageSummary) {
	fmt.Fprintf(b, "[UID %d] %s From: %s\n", m.UID, m.Date, m.From)
	fmt.Fprintf(b, "  Subject: %s\n", m.Subject)
	if len(m.To) > 0 {
		fmt.Fprintf(b, "  To: %s\n", strings.Join(m.To, ", "))
	}
	if len(m.CC) > 0 {
		fmt.Fprintf(b, "  CC: %s\n", strings.Join(m.CC, ", "))
	}
	if len(m.Flags) > 0 {
		fmt.Fprintf(b, "  Flags: %s\n", strings.Join(m.Flags, " "))
	}
	if len(m.GmailLabels) > 0 {
		fmt.Fprintf(b, "  Labels: %s\n", strings.Join(m.GmailLabels, ", "))
	}
	if unsub := m.Unsubscribe; unsub != nil && unsub.CanUnsubscribe {
		if unsub.OneClick {
			b.WriteString("  Unsubscribe: one-click\n")
		} else if unsub.HTTPUrl != "" {
			b.WriteString("  Unsubscribe: link\n")
		} else if unsub.MailtoAddress != "" {
			b.WriteString("  Unsubscribe: mailto\n")
		}
	}
	if m.TextBody != "" {
		fmt.Fprintf(b, "  Body:\n%s\n", m.TextBody)
	}
}

func formatThreadResult(result *imaplib.ThreadResult) string {
	if result == nil || len(result.Messages) == 0 {
		return "0 messages in thread"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d messages in thread\n", len(result.Messages))
	for _, m := range result.Messages {
		b.WriteByte('\n')
		formatMessageSummary(&b, &m)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatIndexSearchResult(result *indexer.SearchResult) string {
	if result == nil || result.TotalMatches == 0 {
		return "0 matches"
	}
	var b strings.Builder
	count := len(result.Messages)
	fmt.Fprintf(&b, "%d matches (showing %d)\n", result.TotalMatches, count)
	for _, m := range result.Messages {
		fmt.Fprintf(&b, "\n[UID %d] Score: %.2f Account: %s Folder: %s\n", m.UID, m.Score, m.Account, m.Folder)
		fmt.Fprintf(&b, "  %s From: %s\n", m.Date, m.From)
		fmt.Fprintf(&b, "  Subject: %s\n", m.Subject)
		if m.To != "" {
			fmt.Fprintf(&b, "  To: %s\n", m.To)
		}
		if m.Snippet != "" {
			fmt.Fprintf(&b, "  Snippet: %s\n", m.Snippet)
		}
	}
	if remaining := result.TotalMatches - result.Offset - count; remaining > 0 {
		fmt.Fprintf(&b, "\n(%d more messages after these, use offset/limit to paginate)\n", remaining)
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- Single message formatter ---

func formatFullMessage(msg *imaplib.FullMessage, preferHTML bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "UID: %d\n", msg.UID)
	fmt.Fprintf(&b, "Date: %s\n", msg.Date)
	fmt.Fprintf(&b, "From: %s\n", msg.From)
	if len(msg.To) > 0 {
		fmt.Fprintf(&b, "To: %s\n", strings.Join(msg.To, ", "))
	}
	if len(msg.CC) > 0 {
		fmt.Fprintf(&b, "CC: %s\n", strings.Join(msg.CC, ", "))
	}
	if msg.ReplyTo != "" {
		fmt.Fprintf(&b, "Reply-To: %s\n", msg.ReplyTo)
	}
	fmt.Fprintf(&b, "Subject: %s\n", msg.Subject)
	if len(msg.Flags) > 0 {
		fmt.Fprintf(&b, "Flags: %s\n", strings.Join(msg.Flags, " "))
	}
	if unsub := msg.Unsubscribe; unsub != nil && unsub.CanUnsubscribe {
		if unsub.OneClick {
			b.WriteString("Unsubscribe: one-click\n")
		} else if unsub.HTTPUrl != "" {
			b.WriteString("Unsubscribe: link\n")
		} else if unsub.MailtoAddress != "" {
			b.WriteString("Unsubscribe: mailto\n")
		}
	}

	if preferHTML {
		if msg.HTMLBody != "" {
			fmt.Fprintf(&b, "\n%s\n", msg.HTMLBody)
		} else if msg.TextBody != "" {
			fmt.Fprintf(&b, "\n%s\n", msg.TextBody)
		}
	} else {
		if msg.TextBody != "" {
			fmt.Fprintf(&b, "\n%s\n", msg.TextBody)
		} else if msg.HTMLBody != "" {
			fmt.Fprintf(&b, "\n--- HTML Body ---\n%s\n", msg.HTMLBody)
		}
	}

	if len(msg.Attachments) > 0 {
		b.WriteString("\n--- Attachments ---\n")
		for i, a := range msg.Attachments {
			name := a.Filename
			if name == "" {
				name = "(inline)"
			}
			fmt.Fprintf(&b, "%d. %s (%s, %d bytes)\n", i+1, name, a.ContentType, a.Size)
			if a.ContentID != "" {
				fmt.Fprintf(&b, "   Content-ID: %s\n", a.ContentID)
			}
			if a.ResourceURI != "" {
				fmt.Fprintf(&b, "   %s\n", a.ResourceURI)
			}
		}
	}

	if len(msg.RawHeaders) > 0 {
		b.WriteString("\n--- Raw Headers ---\n")
		for k, v := range msg.RawHeaders {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// --- Write/action formatters ---

func formatMoveResult(result *imaplib.MoveResult, action, destFolder string) string {
	n := len(result.DestUIDs)
	if n == 0 {
		return fmt.Sprintf("%s messages to %s", action, destFolder)
	}
	uids := formatUIDList(result.DestUIDs)
	return fmt.Sprintf("%s %d messages to %s (new UIDs: %s)", action, n, destFolder, uids)
}

func formatCopyResult(result *imaplib.CopyResult, destFolder string) string {
	n := len(result.DestUIDs)
	if n == 0 {
		return fmt.Sprintf("copied messages to %s", destFolder)
	}
	uids := formatUIDList(result.DestUIDs)
	return fmt.Sprintf("copied %d messages to %s (new UIDs: %s)", n, destFolder, uids)
}

func formatEditResult(result *imaplib.EditResult) string {
	return fmt.Sprintf("edited message UID %d -> new UID %d", result.OldUID, result.NewUID)
}

func formatAppendResult(result *imaplib.AppendResult, folder string) string {
	if result.UID > 0 {
		return fmt.Sprintf("created message in %s (UID: %d)", folder, result.UID)
	}
	return fmt.Sprintf("created message in %s", folder)
}

func formatCreateFolder(name string) string {
	return fmt.Sprintf("created folder %q", name)
}

func formatRenameFolder(oldName, newName string) string {
	return fmt.Sprintf("renamed folder %q to %q", oldName, newName)
}

func formatTrashFolderResult(result *imaplib.TrashFolderResult, folder string) string {
	return fmt.Sprintf("trashed folder %q (moved %d messages to Trash)", folder, result.MessagesMoved)
}

func formatSetFlags(uids []uint32, added, removed []string) string {
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "added "+strings.Join(added, " "))
	}
	if len(removed) > 0 {
		parts = append(parts, "removed "+strings.Join(removed, " "))
	}
	return fmt.Sprintf("%s on %d messages", strings.Join(parts, ", "), len(uids))
}

func formatUIDList(uids []uint32) string {
	parts := make([]string, len(uids))
	for i, u := range uids {
		parts[i] = fmt.Sprintf("%d", u)
	}
	return strings.Join(parts, ", ")
}
