package imap

import (
	"context"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// trashFolderNames is the list of common trash folder names to check
// as a fallback when SPECIAL-USE attributes are not available.
var trashFolderNames = []string{
	"Trash",
	"[Gmail]/Trash",
	"Deleted Items",
	"Deleted Messages",
}

// FindTrashFolder detects the trash folder for the given IMAP connection.
// It first checks for the \Trash SPECIAL-USE attribute (RFC 6154),
// then falls back to well-known folder names.
func FindTrashFolder(ctx context.Context, c *imapclient.Client) (string, error) {
	mailboxes, err := c.List("", "*", nil).Collect()
	if err != nil {
		return "", fmt.Errorf("listing folders for trash detection: %w", err)
	}

	// Phase 1: Check for \Trash attribute
	for _, mbox := range mailboxes {
		for _, attr := range mbox.Attrs {
			if attr == imap.MailboxAttrTrash {
				return mbox.Mailbox, nil
			}
		}
	}

	// Phase 2: Case-insensitive name matching
	nameMap := make(map[string]string, len(mailboxes))
	for _, mbox := range mailboxes {
		nameMap[strings.ToLower(mbox.Mailbox)] = mbox.Mailbox
	}

	for _, candidate := range trashFolderNames {
		if name, ok := nameMap[strings.ToLower(candidate)]; ok {
			return name, nil
		}
	}

	return "", fmt.Errorf("could not detect trash folder; use move_message with an explicit dest_folder instead")
}
