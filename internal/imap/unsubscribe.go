package imap

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UnsubscribeInfo holds parsed unsubscribe metadata from email headers.
type UnsubscribeInfo struct {
	CanUnsubscribe bool   `json:"can_unsubscribe"`
	OneClick       bool   `json:"one_click"`
	HTTPUrl        string `json:"http_url,omitempty"`
	MailtoAddress  string `json:"mailto_address,omitempty"`
	MailtoSubject  string `json:"mailto_subject,omitempty"`
	MailtoBody     string `json:"mailto_body,omitempty"`
}

// ParseUnsubscribeHeaders parses List-Unsubscribe and List-Unsubscribe-Post
// headers per RFC 2369 and RFC 8058.
func ParseUnsubscribeHeaders(listUnsub, listUnsubPost string) *UnsubscribeInfo {
	if listUnsub == "" {
		return nil
	}

	info := &UnsubscribeInfo{}

	// Parse angle-bracket-enclosed URIs separated by commas
	for _, part := range strings.Split(listUnsub, ",") {
		part = strings.TrimSpace(part)
		// Strip angle brackets
		if len(part) >= 2 && part[0] == '<' && part[len(part)-1] == '>' {
			part = part[1 : len(part)-1]
		}
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.HasPrefix(part, "https://") || strings.HasPrefix(part, "http://") {
			if info.HTTPUrl == "" {
				info.HTTPUrl = part
			}
		} else if strings.HasPrefix(part, "mailto:") {
			if info.MailtoAddress == "" {
				parseMailto(part, info)
			}
		}
	}

	// RFC 8058: one-click requires List-Unsubscribe-Post header
	if strings.Contains(listUnsubPost, "List-Unsubscribe=One-Click") && info.HTTPUrl != "" {
		info.OneClick = true
	}

	info.CanUnsubscribe = info.HTTPUrl != "" || info.MailtoAddress != ""
	if !info.CanUnsubscribe {
		return nil
	}

	return info
}

func parseMailto(uri string, info *UnsubscribeInfo) {
	// Strip "mailto:" prefix
	rest := uri[len("mailto:"):]

	// Split on ? for query params
	parts := strings.SplitN(rest, "?", 2)
	info.MailtoAddress = parts[0]

	if len(parts) == 2 {
		params, err := url.ParseQuery(parts[1])
		if err == nil {
			if v := params.Get("subject"); v != "" {
				info.MailtoSubject = v
			}
			if v := params.Get("body"); v != "" {
				info.MailtoBody = v
			}
		}
	}
}

// PerformOneClickUnsubscribe sends the RFC 8058 one-click unsubscribe POST request.
func PerformOneClickUnsubscribe(ctx context.Context, httpUrl string) error {
	body := strings.NewReader("List-Unsubscribe=One-Click")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpUrl, body)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending unsubscribe request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unsubscribe request returned status %d", resp.StatusCode)
	}

	return nil
}
