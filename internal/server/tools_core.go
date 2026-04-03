package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"strings"

	"github.com/jpennington/llmail/internal/imap"
	"github.com/mark3labs/mcp-go/mcp"
	"golang.org/x/image/draw"

	_ "image/gif"
	_ "image/png"
	_ "golang.org/x/image/webp"
)

func (s *Server) registerCoreTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_accounts",
			mcp.WithDescription("List configured email accounts with server info and capabilities."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "List Accounts",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleListAccounts,
	)

	s.mcp.AddTool(
		mcp.NewTool("list_folders",
			mcp.WithDescription("List mailbox folders for an account with message counts."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "List Folders",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleListFolders,
	)

	s.mcp.AddTool(
		mcp.NewTool("imap_search",
			mcp.WithDescription(`Search emails using raw RFC 9051 IMAP SEARCH syntax. The query is passed directly to the IMAP server.

Search keys: ALL, ANSWERED, DELETED, DRAFT, FLAGGED, NEW, OLD, RECENT, SEEN, UNANSWERED, UNDELETED, UNDRAFT, UNFLAGGED, UNSEEN
Address: FROM "str", TO "str", CC "str", BCC "str"
Content: SUBJECT "str", BODY "str", TEXT "str"
Dates: SINCE 1-Jan-2024, BEFORE 1-Feb-2024, SENTSINCE, SENTBEFORE, SENTON, ON
Size: LARGER 10000, SMALLER 50000
Header: HEADER "field-name" "value"
Flags: KEYWORD flag, UNKEYWORD flag
UID: UID 1:100
Boolean: OR (key1) (key2), NOT (key)

Examples:
  FROM "alice" SINCE 1-Jan-2024
  OR FROM "alice" FROM "bob"
  SUBJECT "meeting" NOT SEEN
  HEADER "X-Mailer" "Thunderbird"
  OR (FROM "alice" SUBJECT "urgent") (FLAGGED)`),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("query", mcp.Required(), mcp.Description("Raw RFC 9051 SEARCH query")),
			mcp.WithString("folder", mcp.Description("Folder to search (default: INBOX)")),
			mcp.WithNumber("limit", mcp.Description("Max results to return (default 20, max 100)")),
			mcp.WithNumber("offset", mcp.Description("Result offset for pagination (default 0)")),
			mcp.WithString("detail_level", mcp.Description("Detail level: headers, full (default: headers)")),
			mcp.WithBoolean("prefer_html", mcp.Description("Prefer HTML body over plain text")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "IMAP Search",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleIMAPSearch,
	)

	s.mcp.AddTool(
		mcp.NewTool("list_messages",
			mcp.WithDescription("List messages in a folder with pagination. Returns newest messages first."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Description("Folder to list (default: INBOX)")),
			mcp.WithNumber("limit", mcp.Description("Max results to return (default 20, max 100)")),
			mcp.WithNumber("offset", mcp.Description("Result offset for pagination (default 0)")),
			mcp.WithString("detail_level", mcp.Description("Detail level: headers, full (default: headers)")),
			mcp.WithBoolean("prefer_html", mcp.Description("Prefer HTML body over plain text")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "List Messages",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleListMessages,
	)

	s.mcp.AddTool(
		mcp.NewTool("get_attachment",
			mcp.WithDescription("Fetch the content of an email attachment. Pass the resource URI from the get_message attachment listing (e.g. llmail://account/folder/uid/attachments/part_number)."),
			mcp.WithString("uri", mcp.Required(), mcp.Description("Attachment resource URI from get_message output")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Get Attachment",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleGetAttachment,
	)

	s.mcp.AddTool(
		mcp.NewTool("save_attachment",
			mcp.WithDescription("Save an email attachment to a file on disk. Pass the resource URI from the get_message attachment listing and a destination file path."),
			mcp.WithString("uri", mcp.Required(), mcp.Description("Attachment resource URI from get_message output")),
			mcp.WithString("path", mcp.Required(), mcp.Description("Destination file path to save the attachment to")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Save Attachment",
				ReadOnlyHint:    mcp.ToBoolPtr(false),
				DestructiveHint: mcp.ToBoolPtr(false),
			}),
		),
		s.handleSaveAttachment,
	)

	s.mcp.AddTool(
		mcp.NewTool("get_message",
			mcp.WithDescription("Fetch full content of a specific email by UID."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Required(), mcp.Description("Folder name")),
			mcp.WithNumber("uid", mcp.Required(), mcp.Description("Message UID")),
			mcp.WithBoolean("prefer_html", mcp.Description("Prefer HTML body over plain text")),
			mcp.WithBoolean("include_raw_headers", mcp.Description("Include all raw headers")),
			mcp.WithString("detail_level", mcp.Description("Detail level: headers, full (default: full)")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Get Message",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleGetMessage,
	)
}

type accountInfo struct {
	Name         string
	Host         string
	Port         int
	Username     string
	Capabilities struct {
		Gmail  bool
		Sort   bool
		Thread bool
	}
	IndexEnabled bool
}

func (s *Server) handleListAccounts(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var accounts []accountInfo
	for _, name := range s.cfg.AccountNames() {
		acc, _ := s.cfg.GetAccount(name)
		info := accountInfo{
			Name:     name,
			Host:     acc.Host,
			Port:     acc.Port,
			Username: acc.Username,
		}
		info.Capabilities.Gmail = acc.Capabilities.GmailExtensions
		info.Capabilities.Sort = acc.Capabilities.Sort
		info.Capabilities.Thread = acc.Capabilities.Thread

		if s.cfg.Index.Enabled {
			switch s.cfg.Index.Mode {
			case "all":
				info.IndexEnabled = true
			case "selected":
				if _, ok := s.cfg.Index.Accounts[name]; ok {
					info.IndexEnabled = true
				}
			}
		}

		accounts = append(accounts, info)
	}

	return textResult(formatAccounts(accounts)), nil
}

func (s *Server) handleListFolders(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account, _ := args["account"].(string)
	if account == "" {
		return errorResult("account parameter is required"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	folders, err := imap.ListFolders(ctx, c)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("listing folders: " + err.Error()), nil
	}

	return textResult(formatFolders(folders)), nil
}

func (s *Server) handleIMAPSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account, _ := args["account"].(string)
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	query, _ := args["query"].(string)
	if query == "" {
		return errorResult("query parameter is required"), nil
	}

	preferHTML, _ := args["prefer_html"].(bool)
	params := imap.RawSearchParams{
		Folder:      stringFrom(args, "folder"),
		Query:       query,
		Limit:       intFrom(args, "limit"),
		Offset:      intFrom(args, "offset"),
		DetailLevel: imap.ParseDetailLevel(stringFrom(args, "detail_level"), imap.DetailHeaders),
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	result, err := imap.RawSearch(ctx, c, params, &imap.FetchParams{PreferHTML: preferHTML})
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("searching: " + err.Error()), nil
	}

	return s.checkGuard(formatSearchResult(result)), nil
}

func (s *Server) handleListMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account, _ := args["account"].(string)
	if account == "" {
		return errorResult("account parameter is required"), nil
	}

	folder := stringFrom(args, "folder")
	limit := intFrom(args, "limit")
	offset := intFrom(args, "offset")
	level := imap.ParseDetailLevel(stringFrom(args, "detail_level"), imap.DetailHeaders)
	preferHTML, _ := args["prefer_html"].(bool)

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	result, err := imap.ListMessages(ctx, c, folder, limit, offset, level, &imap.FetchParams{PreferHTML: preferHTML})
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("listing messages: " + err.Error()), nil
	}

	return s.checkGuard(formatSearchResult(result)), nil
}

func (s *Server) handleGetMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account, _ := args["account"].(string)
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	folder, _ := args["folder"].(string)
	if folder == "" {
		return errorResult("folder parameter is required"), nil
	}
	uidF, _ := args["uid"].(float64)
	uid := uint32(uidF)
	if uid == 0 {
		return errorResult("uid parameter is required"), nil
	}
	includeRawHeaders, _ := args["include_raw_headers"].(bool)
	preferHTML, _ := args["prefer_html"].(bool)

	level := imap.ParseDetailLevel(stringFrom(args, "detail_level"), imap.DetailFull)

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	// For minimal/standard levels, use the summary fetcher
	if level != imap.DetailFull {
		uidSet := imap.UIDSetFromUID(uid)
		messages, err := imap.FetchByLevel(ctx, c, uidSet, level, false, preferHTML)
		if err != nil {
			s.pool.Discard(account, c)
			return errorResult("fetching message: " + err.Error()), nil
		}
		if len(messages) == 0 {
			return errorResult("message not found"), nil
		}
		return s.checkGuard(formatSingleSummary(&messages[0])), nil
	}

	msg, err := imap.FetchMessage(ctx, c, folder, uid, includeRawHeaders)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("fetching message: " + err.Error()), nil
	}

	// Populate resource URIs on attachments
	for i := range msg.Attachments {
		msg.Attachments[i].ResourceURI = attachmentResourceURI(account, folder, uid, msg.Attachments[i].PartNumber)
	}

	return s.checkGuard(formatFullMessage(msg, preferHTML)), nil
}

func (s *Server) handleGetAttachment(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	uri, _ := args["uri"].(string)
	if uri == "" {
		return errorResult("uri parameter is required"), nil
	}

	account, folder, uid, partNumber, err := parseAttachmentURI(uri)
	if err != nil {
		return errorResult("invalid attachment URI: " + err.Error()), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	content, contentType, filename, err := imap.FetchAttachmentContent(ctx, c, folder, uid, partNumber)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("fetching attachment: " + err.Error()), nil
	}

	b64 := base64.StdEncoding.EncodeToString(content)
	desc := fmt.Sprintf("Attachment: %s (%s, %d bytes)", filename, contentType, len(content))

	if strings.HasPrefix(contentType, "text/") {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{Type: "text", Text: desc},
				mcp.NewEmbeddedResource(mcp.TextResourceContents{
					URI:      uri,
					MIMEType: contentType,
					Text:     string(content),
				}),
			},
		}, nil
	}

	if strings.HasPrefix(contentType, "image/") {
		if maxBytes := s.cfg.Image.MaxSizeBytes; maxBytes > 0 && len(content) > maxBytes {
			shrunk, err := shrinkImage(content, maxBytes)
			if err == nil {
				content = shrunk
				contentType = "image/jpeg"
				b64 = base64.StdEncoding.EncodeToString(content)
				desc = fmt.Sprintf("Attachment: %s (re-encoded %s, %d bytes)", filename, contentType, len(content))
			}
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{Type: "text", Text: desc},
				mcp.ImageContent{Type: "image", Data: b64, MIMEType: contentType},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: desc},
			mcp.NewEmbeddedResource(mcp.BlobResourceContents{
				URI:      uri,
				MIMEType: contentType,
				Blob:     b64,
			}),
		},
	}, nil
}

func (s *Server) handleSaveAttachment(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	uri, _ := args["uri"].(string)
	if uri == "" {
		return errorResult("uri parameter is required"), nil
	}
	dest, _ := args["path"].(string)
	if dest == "" {
		return errorResult("path parameter is required"), nil
	}

	account, folder, uid, partNumber, err := parseAttachmentURI(uri)
	if err != nil {
		return errorResult("invalid attachment URI: " + err.Error()), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	content, contentType, filename, err := imap.FetchAttachmentContent(ctx, c, folder, uid, partNumber)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("fetching attachment: " + err.Error()), nil
	}

	if _, err := os.Stat(dest); err == nil {
		return errorResult("file already exists: " + dest), nil
	}

	if err := os.WriteFile(dest, content, 0644); err != nil {
		return errorResult("writing file: " + err.Error()), nil
	}

	desc := fmt.Sprintf("Saved attachment %s (%s, %d bytes) to %s", filename, contentType, len(content), dest)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: desc},
		},
	}, nil
}

func attachmentResourceURI(account, folder string, uid uint32, partNumber string) string {
	return "llmail://" + account + "/" + folder + "/" + stringFromUint32(uid) + "/attachments/" + partNumber
}

func stringFromUint32(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func stringFrom(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func intFrom(args map[string]any, key string) int {
	v, _ := args[key].(float64)
	return int(v)
}

func int64From(args map[string]any, key string) int64 {
	v, _ := args[key].(float64)
	return int64(v)
}

// shrinkImage re-encodes an image as JPEG at 50% quality, progressively
// halving dimensions until the result fits within maxBytes.
func shrinkImage(data []byte, maxBytes int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}

	// First try: just re-encode at 50% quality without resizing.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 50}); err != nil {
		return nil, fmt.Errorf("encoding jpeg: %w", err)
	}
	if buf.Len() <= maxBytes {
		return buf.Bytes(), nil
	}

	// Progressively shrink by 50% until it fits.
	current := img
	for i := 0; i < 10; i++ {
		bounds := current.Bounds()
		newW := bounds.Dx() / 2
		newH := bounds.Dy() / 2
		if newW == 0 || newH == 0 {
			break
		}

		dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
		draw.BiLinear.Scale(dst, dst.Bounds(), current, bounds, draw.Over, nil)
		current = dst

		buf.Reset()
		if err := jpeg.Encode(&buf, current, &jpeg.Options{Quality: 50}); err != nil {
			return nil, fmt.Errorf("encoding jpeg: %w", err)
		}
		if buf.Len() <= maxBytes {
			return buf.Bytes(), nil
		}
	}

	// Return the smallest we managed even if still over limit.
	return buf.Bytes(), nil
}
