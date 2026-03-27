package imap

import (
	"context"
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// MoveResult holds the result of a move operation.
type MoveResult struct {
	DestUIDs []uint32 `json:"dest_uids,omitempty"`
}

// MoveMessages moves one or more messages from one folder to another.
func MoveMessages(ctx context.Context, c *imapclient.Client, folder string, uids []uint32, destFolder string) (*MoveResult, error) {
	selectCmd := c.Select(folder, nil)
	if _, err := selectCmd.Wait(); err != nil {
		return nil, fmt.Errorf("selecting folder %q: %w", folder, err)
	}

	uidSet := buildUIDSet(uids)
	moveData, err := c.Move(uidSet, destFolder).Wait()
	if err != nil {
		return nil, fmt.Errorf("moving messages: %w", err)
	}

	result := &MoveResult{}
	if moveData.DestUIDs != nil {
		if destUIDSet, ok := moveData.DestUIDs.(imap.UIDSet); ok {
			if nums, ok := destUIDSet.Nums(); ok {
				for _, u := range nums {
					result.DestUIDs = append(result.DestUIDs, uint32(u))
				}
			}
		}
	}

	return result, nil
}

// CopyResult holds the result of a copy operation.
type CopyResult struct {
	DestUIDs []uint32 `json:"dest_uids,omitempty"`
}

// CopyMessages copies one or more messages to another folder.
func CopyMessages(ctx context.Context, c *imapclient.Client, folder string, uids []uint32, destFolder string) (*CopyResult, error) {
	selectCmd := c.Select(folder, &imap.SelectOptions{ReadOnly: true})
	if _, err := selectCmd.Wait(); err != nil {
		return nil, fmt.Errorf("selecting folder %q: %w", folder, err)
	}

	uidSet := buildUIDSet(uids)
	copyData, err := c.Copy(uidSet, destFolder).Wait()
	if err != nil {
		return nil, fmt.Errorf("copying messages: %w", err)
	}

	result := &CopyResult{}
	if nums, ok := copyData.DestUIDs.Nums(); ok {
		for _, u := range nums {
			result.DestUIDs = append(result.DestUIDs, uint32(u))
		}
	}

	return result, nil
}

func buildUIDSet(uids []uint32) imap.UIDSet {
	uidSet := imap.UIDSet{}
	for _, u := range uids {
		uidSet.AddNum(imap.UID(u))
	}
	return uidSet
}
