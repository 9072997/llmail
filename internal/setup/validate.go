package setup

import (
	"context"
	"fmt"

	"github.com/emersion/go-imap/v2"
	imaplib "github.com/jpennington/llmail/internal/imap"
)

type ValidationResult struct {
	Success      bool
	Capabilities Capabilities
	Folders      []string
	Error        string
}

type Capabilities struct {
	GmailExtensions bool
	Sort            bool
	Thread          bool
}

func ValidateConnection(ctx context.Context, host string, port int, useTLS bool, username, password string) (*ValidationResult, error) {
	c, caps, err := imaplib.DialAndCheck(ctx, host, port, useTLS, username, password)
	if err != nil {
		return &ValidationResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}
	defer c.Close()

	result := &ValidationResult{
		Success: true,
	}

	// Detect capabilities
	for cap := range caps {
		switch {
		case cap == imap.Cap("X-GM-EXT-1"):
			result.Capabilities.GmailExtensions = true
		case cap == imap.Cap("SORT"):
			result.Capabilities.Sort = true
		case cap == imap.Cap("THREAD=REFERENCES") || cap == imap.Cap("THREAD=ORDEREDSUBJECT"):
			result.Capabilities.Thread = true
		}
	}

	// List folders
	folders, err := imaplib.ListFolders(ctx, c)
	if err != nil {
		result.Error = fmt.Sprintf("connected but failed to list folders: %v", err)
		return result, nil
	}

	for _, f := range folders {
		result.Folders = append(result.Folders, f.Name)
	}

	return result, nil
}
