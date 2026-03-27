package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jpennington/llmail/internal/config"
	imaplib "github.com/jpennington/llmail/internal/imap"
)

func buildSystemPrompt(ctx context.Context, cfg *config.Config, pool *imaplib.Pool) string {
	var b strings.Builder

	b.WriteString("You are llmail, an email assistant with access to the user's email accounts via IMAP.\n")
	b.WriteString(fmt.Sprintf("Today's date is %s.\n\n", time.Now().Format("Monday January 2, 2006")))

	b.WriteString("Available accounts:\n")
	for _, name := range cfg.AccountNames() {
		acc, _ := cfg.GetAccount(name)
		var caps []string
		if acc.Capabilities.GmailExtensions {
			caps = append(caps, "Gmail")
		}
		if acc.Capabilities.Sort {
			caps = append(caps, "SORT")
		}
		if acc.Capabilities.Thread {
			caps = append(caps, "THREAD")
		}
		capStr := ""
		if len(caps) > 0 {
			capStr = fmt.Sprintf(" [%s]", strings.Join(caps, ", "))
		}
		b.WriteString(fmt.Sprintf("- %s (%s on %s:%d)%s\n", name, acc.Username, acc.Host, acc.Port, capStr))

		// List folders for this account
		if c, err := pool.Get(ctx, name); err == nil {
			if folders, err := imaplib.ListFolders(ctx, c); err == nil {
				var names []string
				for _, f := range folders {
					names = append(names, f.Name)
				}
				b.WriteString(fmt.Sprintf("  Folders: %s\n", strings.Join(names, ", ")))
			}
			pool.Put(name, c)
		}
	}

	b.WriteString("\nYou can search, read, organize, move, copy, flag, draft, and manage emails using the available tools.\n")
	b.WriteString("Always specify the \"account\" parameter when calling tools.\n")
	b.WriteString("Use detail_level \"minimal\" for listing/searching, \"full\" for reading specific messages.\n")

	if cfg.Index.Enabled && cfg.Index.Mode != "none" {
		b.WriteString("A local full-text search index is available - use search_local_index for fast keyword searches.\n")
	}

	return b.String()
}
