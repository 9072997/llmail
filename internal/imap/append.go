package imap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
)

// AppendParams holds parameters for creating and appending a message.
type AppendParams struct {
	Folder   string   `json:"folder"`
	From     string   `json:"from"`
	To       []string `json:"to"`
	CC       []string `json:"cc,omitempty"`
	Subject  string   `json:"subject"`
	Body     string   `json:"body"`
	HTMLBody string   `json:"html_body,omitempty"`
	Flags    []string `json:"flags,omitempty"`
	Date     string   `json:"date,omitempty"` // YYYY-MM-DD
}

// AppendResult holds the result of an APPEND operation.
type AppendResult struct {
	UID    uint32 `json:"uid,omitempty"`
	Folder string `json:"-"`
}

// AppendMessage builds an RFC 5322 message and appends it to the given folder.
func AppendMessage(ctx context.Context, c *imapclient.Client, params AppendParams) (*AppendResult, error) {
	folder := params.Folder
	if folder == "" {
		folder = "Drafts"
	}

	msgDate := time.Now()
	if params.Date != "" {
		t, err := time.Parse("2006-01-02", params.Date)
		if err != nil {
			return nil, fmt.Errorf("invalid date format: %w", err)
		}
		msgDate = t
	}

	// Build the message
	msgBuf, err := buildMessage(params, msgDate)
	if err != nil {
		return nil, fmt.Errorf("building message: %w", err)
	}

	// Convert string flags to imap.Flag
	var imapFlags []imap.Flag
	for _, f := range params.Flags {
		imapFlags = append(imapFlags, imap.Flag(f))
	}

	appendOpts := &imap.AppendOptions{
		Flags: imapFlags,
		Time:  msgDate,
	}

	appendCmd := c.Append(folder, int64(msgBuf.Len()), appendOpts)
	if _, err := io.Copy(appendCmd, msgBuf); err != nil {
		return nil, fmt.Errorf("writing message data: %w", err)
	}
	if err := appendCmd.Close(); err != nil {
		return nil, fmt.Errorf("closing append: %w", err)
	}

	appendData, err := appendCmd.Wait()
	if err != nil {
		return nil, fmt.Errorf("appending message: %w", err)
	}

	result := &AppendResult{
		UID:    uint32(appendData.UID),
		Folder: folder,
	}

	return result, nil
}

func buildMessage(params AppendParams, msgDate time.Time) (*bytes.Buffer, error) {
	var buf bytes.Buffer

	fromAddrs, err := mail.ParseAddressList(params.From)
	if err != nil {
		return nil, fmt.Errorf("parsing from address: %w", err)
	}

	var toAddrs []*mail.Address
	for _, addr := range params.To {
		parsed, err := mail.ParseAddressList(addr)
		if err != nil {
			return nil, fmt.Errorf("parsing to address %q: %w", addr, err)
		}
		toAddrs = append(toAddrs, parsed...)
	}

	var ccAddrs []*mail.Address
	for _, addr := range params.CC {
		parsed, err := mail.ParseAddressList(addr)
		if err != nil {
			return nil, fmt.Errorf("parsing cc address %q: %w", addr, err)
		}
		ccAddrs = append(ccAddrs, parsed...)
	}

	hasHTML := strings.TrimSpace(params.HTMLBody) != ""

	if hasHTML {
		// Multipart/alternative message
		header := mail.Header{}
		header.SetDate(msgDate)
		header.SetAddressList("From", fromAddrs)
		header.SetAddressList("To", toAddrs)
		if len(ccAddrs) > 0 {
			header.SetAddressList("Cc", ccAddrs)
		}
		header.SetSubject(params.Subject)
		header.SetMessageID(generateMessageID(params.From))
		header.Set("X-LLMail-Created", "true")

		mw, err := mail.CreateWriter(&buf, header)
		if err != nil {
			return nil, fmt.Errorf("creating mail writer: %w", err)
		}

		// Plain text part
		th := mail.InlineHeader{}
		th.SetContentType("text/plain", map[string]string{"charset": "UTF-8"})
		tw, err := mw.CreateSingleInline(th)
		if err != nil {
			return nil, fmt.Errorf("creating text part: %w", err)
		}
		if _, err := io.WriteString(tw, params.Body); err != nil {
			return nil, err
		}
		if err := tw.Close(); err != nil {
			return nil, err
		}

		// HTML part
		hh := mail.InlineHeader{}
		hh.SetContentType("text/html", map[string]string{"charset": "UTF-8"})
		hw, err := mw.CreateSingleInline(hh)
		if err != nil {
			return nil, fmt.Errorf("creating html part: %w", err)
		}
		if _, err := io.WriteString(hw, params.HTMLBody); err != nil {
			return nil, err
		}
		if err := hw.Close(); err != nil {
			return nil, err
		}

		if err := mw.Close(); err != nil {
			return nil, err
		}
	} else {
		// Simple text message
		header := gomessage.Header{}
		header.Set("Date", msgDate.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
		header.Set("From", params.From)
		header.Set("To", strings.Join(params.To, ", "))
		if len(params.CC) > 0 {
			header.Set("Cc", strings.Join(params.CC, ", "))
		}
		header.Set("Subject", params.Subject)
		header.Set("Message-ID", generateMessageID(params.From))
		header.Set("X-LLMail-Created", "true")
		header.Set("MIME-Version", "1.0")
		header.Set("Content-Type", "text/plain; charset=UTF-8")

		w, err := gomessage.CreateWriter(&buf, header)
		if err != nil {
			return nil, fmt.Errorf("creating message writer: %w", err)
		}
		if _, err := io.WriteString(w, params.Body); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	}

	return &buf, nil
}

func generateMessageID(from string) string {
	// Extract domain from the from address
	domain := "llmail.local"
	if idx := strings.LastIndex(from, "@"); idx >= 0 {
		rest := from[idx+1:]
		rest = strings.TrimRight(rest, ">")
		if rest != "" {
			// Validate it looks like a domain
			if !strings.ContainsAny(rest, " \t") {
				domain = rest
			}
		}
	}
	return fmt.Sprintf("<%d.llmail@%s>", time.Now().UnixNano(), domain)
}

