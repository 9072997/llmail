package imap

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// SetFlags modifies flags on one or more messages.
// The folder must already be selected in read-write mode.
func SetFlags(c *imapclient.Client, uids []uint32, op imap.StoreFlagsOp, flags []imap.Flag) error {
	uidSet := buildUIDSet(uids)

	storeFlags := &imap.StoreFlags{
		Op:     op,
		Silent: true,
		Flags:  flags,
	}

	_, err := c.Store(uidSet, storeFlags, nil).Collect()
	if err != nil {
		return fmt.Errorf("storing flags: %w", err)
	}
	return nil
}
