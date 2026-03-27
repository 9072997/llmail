package imap

import (
	"context"
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

type FolderInfo struct {
	Name   string `json:"name"`
	Total  uint32 `json:"total"`
	Unseen uint32 `json:"unseen"`
}

func ListFolders(ctx context.Context, c *imapclient.Client) ([]FolderInfo, error) {
	mailboxes, err := c.List("", "*", &imap.ListOptions{
		ReturnStatus: &imap.StatusOptions{
			NumMessages: true,
			NumUnseen:   true,
		},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("listing folders: %w", err)
	}

	var folders []FolderInfo
	for _, mbox := range mailboxes {
		fi := FolderInfo{
			Name: mbox.Mailbox,
		}

		if mbox.Status != nil {
			if mbox.Status.NumMessages != nil {
				fi.Total = *mbox.Status.NumMessages
			}
			if mbox.Status.NumUnseen != nil {
				fi.Unseen = *mbox.Status.NumUnseen
			}
		}

		folders = append(folders, fi)
	}

	return folders, nil
}

// CreateFolder creates a new mailbox folder.
func CreateFolder(ctx context.Context, c *imapclient.Client, name string) error {
	if err := c.Create(name, nil).Wait(); err != nil {
		return fmt.Errorf("creating folder %q: %w", name, err)
	}
	return nil
}

// RenameFolder renames a mailbox folder.
func RenameFolder(ctx context.Context, c *imapclient.Client, oldName, newName string) error {
	if err := c.Rename(oldName, newName, nil).Wait(); err != nil {
		return fmt.Errorf("renaming folder %q to %q: %w", oldName, newName, err)
	}
	return nil
}

// TrashFolder moves a folder's contents to trash and deletes the folder.
// It moves all messages to the trash folder, then removes the now-empty folder.
func TrashFolder(ctx context.Context, c *imapclient.Client, folder, trashFolder string) (*TrashFolderResult, error) {
	// Select the folder to get message count
	selectData, err := c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("selecting folder %q: %w", folder, err)
	}

	result := &TrashFolderResult{}

	if selectData.NumMessages > 0 {
		// Re-select read-write to move messages
		if _, err := c.Select(folder, nil).Wait(); err != nil {
			return nil, fmt.Errorf("selecting folder %q read-write: %w", folder, err)
		}

		// Move all messages (UID 1:*)
		uidSet := imap.UIDSet{}
		uidSet.AddRange(1, 0) // 1:* means all UIDs
		_, err := c.Move(uidSet, trashFolder).Wait()
		if err != nil {
			return nil, fmt.Errorf("moving messages to trash: %w", err)
		}
		result.MessagesMoved = int(selectData.NumMessages)
	}

	// Delete the now-empty folder
	if err := c.Delete(folder).Wait(); err != nil {
		return nil, fmt.Errorf("deleting folder %q: %w", folder, err)
	}

	return result, nil
}

// TrashFolderResult holds the result of trashing a folder.
type TrashFolderResult struct {
	MessagesMoved int `json:"messages_moved"`
}
