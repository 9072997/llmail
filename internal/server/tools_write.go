package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
	imaplib "github.com/jpennington/llmail/internal/imap"
	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerWriteTools() {
	s.mcp.AddTool(
		mcp.NewTool("move_messages",
			mcp.WithDescription("Move one or more messages from one folder to another."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Required(), mcp.Description("Source folder")),
			mcp.WithArray("uids", mcp.Required(), mcp.Description("Message UIDs to move"), mcp.WithNumberItems()),
			mcp.WithString("dest_folder", mcp.Required(), mcp.Description("Destination folder")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Move Messages",
				ReadOnlyHint:    mcp.ToBoolPtr(false),
				DestructiveHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleMoveMessages,
	)

	s.mcp.AddTool(
		mcp.NewTool("copy_messages",
			mcp.WithDescription("Copy one or more messages to another folder."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Required(), mcp.Description("Source folder")),
			mcp.WithArray("uids", mcp.Required(), mcp.Description("Message UIDs to copy"), mcp.WithNumberItems()),
			mcp.WithString("dest_folder", mcp.Required(), mcp.Description("Destination folder")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Copy Messages",
				ReadOnlyHint: mcp.ToBoolPtr(false),
			}),
		),
		s.handleCopyMessages,
	)

	s.mcp.AddTool(
		mcp.NewTool("delete_messages",
			mcp.WithDescription("Move one or more messages to the trash folder. Permanent deletion is not supported."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Required(), mcp.Description("Current folder of the messages")),
			mcp.WithArray("uids", mcp.Required(), mcp.Description("Message UIDs to delete"), mcp.WithNumberItems()),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Delete Messages",
				ReadOnlyHint:    mcp.ToBoolPtr(false),
				DestructiveHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleDeleteMessages,
	)

	s.mcp.AddTool(
		mcp.NewTool("create_folder",
			mcp.WithDescription("Create a new mailbox folder."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Folder name (use delimiter for hierarchy, e.g. 'Projects/Work')")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Create Folder",
				ReadOnlyHint: mcp.ToBoolPtr(false),
			}),
		),
		s.handleCreateFolder,
	)

	s.mcp.AddTool(
		mcp.NewTool("rename_folder",
			mcp.WithDescription("Rename a mailbox folder."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Required(), mcp.Description("Current folder name")),
			mcp.WithString("new_name", mcp.Required(), mcp.Description("New folder name")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Rename Folder",
				ReadOnlyHint: mcp.ToBoolPtr(false),
			}),
		),
		s.handleRenameFolder,
	)

	s.mcp.AddTool(
		mcp.NewTool("trash_folder",
			mcp.WithDescription("Move all messages in a folder to trash, then delete the empty folder. Cannot trash INBOX or the trash folder itself."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Required(), mcp.Description("Folder to trash")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Trash Folder",
				ReadOnlyHint:    mcp.ToBoolPtr(false),
				DestructiveHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleTrashFolder,
	)

	s.mcp.AddTool(
		mcp.NewTool("set_flags",
			mcp.WithDescription("Add or remove flags on one or more messages. Common flags: \\Seen, \\Flagged, \\Answered, \\Draft, \\Deleted."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Required(), mcp.Description("Folder containing the messages")),
			mcp.WithArray("uids", mcp.Required(), mcp.Description("Message UIDs"), mcp.WithNumberItems()),
			mcp.WithArray("add", mcp.Description("Flags to add"), mcp.WithStringItems()),
			mcp.WithArray("remove", mcp.Description("Flags to remove"), mcp.WithStringItems()),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Set Flags",
				ReadOnlyHint: mcp.ToBoolPtr(false),
			}),
		),
		s.handleSetFlags,
	)

	s.mcp.AddTool(
		mcp.NewTool("edit_message",
			mcp.WithDescription("Edit a message that was created by llmail (has X-LLMail-Created header). Replaces the message with an updated version. Only provided fields are changed; omitted fields keep their original values."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Required(), mcp.Description("Folder containing the message")),
			mcp.WithNumber("uid", mcp.Required(), mcp.Description("Message UID")),
			mcp.WithArray("to", mcp.Description("New To addresses (replaces all)"), mcp.WithStringItems()),
			mcp.WithArray("cc", mcp.Description("New CC addresses (replaces all)"), mcp.WithStringItems()),
			mcp.WithString("subject", mcp.Description("New subject")),
			mcp.WithString("body", mcp.Description("New plain text body")),
			mcp.WithString("html_body", mcp.Description("New HTML body")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Edit Message",
				ReadOnlyHint:    mcp.ToBoolPtr(false),
				DestructiveHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleEditMessage,
	)

	s.mcp.AddTool(
		mcp.NewTool("unsubscribe",
			mcp.WithDescription("Unsubscribe from mailing lists using standardized email headers (RFC 2369/8058). Supports one-click unsubscribe (HTTP POST), mailto-based unsubscribe (creates a draft), or returns a link for manual unsubscribe. Works with one or more messages."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Required(), mcp.Description("Folder containing the messages")),
			mcp.WithArray("uids", mcp.Required(), mcp.Description("Message UIDs to unsubscribe from"), mcp.WithNumberItems()),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Unsubscribe",
				ReadOnlyHint: mcp.ToBoolPtr(false),
			}),
		),
		s.handleUnsubscribe,
	)

	s.mcp.AddTool(
		mcp.NewTool("create_draft",
			mcp.WithDescription("Compose a new draft email and save it to the Drafts folder. The draft must be sent manually from an email client."),
			mcp.WithString("account", mcp.Required(), mcp.Description("Account name")),
			mcp.WithString("folder", mcp.Description("Target folder (default: Drafts). Rarely needs to be changed.")),
			mcp.WithString("from", mcp.Required(), mcp.Description("From address (e.g. 'Name <email@example.com>')")),
			mcp.WithArray("to", mcp.Description("To addresses"), mcp.Required(), mcp.WithStringItems()),
			mcp.WithArray("cc", mcp.Description("CC addresses"), mcp.WithStringItems()),
			mcp.WithString("subject", mcp.Required(), mcp.Description("Message subject")),
			mcp.WithString("body", mcp.Required(), mcp.Description("Plain text body")),
			mcp.WithString("html_body", mcp.Description("Optional HTML body (creates multipart/alternative if provided)")),
			mcp.WithArray("flags", mcp.Description("Message flags (default: \\Draft)"), mcp.WithStringItems()),
			mcp.WithString("date", mcp.Description("Message date (YYYY-MM-DD, default: now)")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Create Draft",
				ReadOnlyHint: mcp.ToBoolPtr(false),
			}),
		),
		s.handleCreateMessage,
	)
}

func (s *Server) handleMoveMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	folder := stringFrom(args, "folder")
	if folder == "" {
		return errorResult("folder parameter is required"), nil
	}
	uids := uint32SliceFrom(args, "uids")
	if len(uids) == 0 {
		return errorResult("uids parameter is required"), nil
	}
	destFolder := stringFrom(args, "dest_folder")
	if destFolder == "" {
		return errorResult("dest_folder parameter is required"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	result, err := imaplib.MoveMessages(ctx, c, folder, uids, destFolder)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("moving messages: " + err.Error()), nil
	}

	if s.indexer != nil {
		for i, uid := range uids {
			var destUID uint32
			if i < len(result.DestUIDs) {
				destUID = result.DestUIDs[i]
			}
			if destUID > 0 {
				s.indexer.NotifyMove(account, folder, uid, destFolder, destUID)
			} else {
				s.indexer.NotifyDelete(account, folder, uid)
			}
		}
	}

	return textResult(formatMoveResult(result, "moved", destFolder)), nil
}

func (s *Server) handleCopyMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	folder := stringFrom(args, "folder")
	if folder == "" {
		return errorResult("folder parameter is required"), nil
	}
	uids := uint32SliceFrom(args, "uids")
	if len(uids) == 0 {
		return errorResult("uids parameter is required"), nil
	}
	destFolder := stringFrom(args, "dest_folder")
	if destFolder == "" {
		return errorResult("dest_folder parameter is required"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	result, err := imaplib.CopyMessages(ctx, c, folder, uids, destFolder)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("copying messages: " + err.Error()), nil
	}

	if s.indexer != nil {
		for i, uid := range uids {
			var destUID uint32
			if i < len(result.DestUIDs) {
				destUID = result.DestUIDs[i]
			}
			if destUID > 0 {
				s.indexer.NotifyCopy(account, folder, uid, destFolder, destUID)
			}
		}
	}

	return textResult(formatCopyResult(result, destFolder)), nil
}

func (s *Server) handleDeleteMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	folder := stringFrom(args, "folder")
	if folder == "" {
		return errorResult("folder parameter is required"), nil
	}
	uids := uint32SliceFrom(args, "uids")
	if len(uids) == 0 {
		return errorResult("uids parameter is required"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	trashFolder, err := s.getTrashFolder(ctx, account, c)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	if folder == trashFolder {
		return errorResult("messages are already in trash; permanent deletion is not supported"), nil
	}

	result, err := imaplib.MoveMessages(ctx, c, folder, uids, trashFolder)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("deleting messages: " + err.Error()), nil
	}

	if s.indexer != nil {
		for i, uid := range uids {
			var destUID uint32
			if i < len(result.DestUIDs) {
				destUID = result.DestUIDs[i]
			}
			if destUID > 0 {
				s.indexer.NotifyMove(account, folder, uid, trashFolder, destUID)
			} else {
				s.indexer.NotifyDelete(account, folder, uid)
			}
		}
	}

	return textResult(formatMoveResult(result, "deleted", trashFolder)), nil
}

func (s *Server) handleEditMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	folder := stringFrom(args, "folder")
	if folder == "" {
		return errorResult("folder parameter is required"), nil
	}
	uid := uint32From(args, "uid")
	if uid == 0 {
		return errorResult("uid parameter is required"), nil
	}

	params := imaplib.EditParams{
		To:       stringSliceFrom(args, "to"),
		CC:       stringSliceFrom(args, "cc"),
		Subject:  stringFrom(args, "subject"),
		Body:     stringFrom(args, "body"),
		HTMLBody: stringFrom(args, "html_body"),
	}

	if len(params.To) == 0 && len(params.CC) == 0 && params.Subject == "" && params.Body == "" && params.HTMLBody == "" {
		return errorResult("at least one field to edit must be provided"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	// Select folder read-write
	if _, err := c.Select(folder, nil).Wait(); err != nil {
		s.pool.Discard(account, c)
		return errorResult("selecting folder: " + err.Error()), nil
	}

	result, err := imaplib.EditMessage(ctx, c, folder, uid, params)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("editing message: " + err.Error()), nil
	}

	// Update indexer: delete old doc, index new one
	if s.indexer != nil {
		s.indexer.NotifyDelete(account, folder, uid)
	}

	return textResult(formatEditResult(result)), nil
}

func (s *Server) handleSetFlags(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	folder := stringFrom(args, "folder")
	if folder == "" {
		return errorResult("folder parameter is required"), nil
	}
	uids := uint32SliceFrom(args, "uids")
	if len(uids) == 0 {
		return errorResult("uids parameter is required"), nil
	}
	addFlags := stringSliceFrom(args, "add")
	removeFlags := stringSliceFrom(args, "remove")
	if len(addFlags) == 0 && len(removeFlags) == 0 {
		return errorResult("at least one of add or remove must be non-empty"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	if _, err := c.Select(folder, nil).Wait(); err != nil {
		s.pool.Discard(account, c)
		return errorResult("selecting folder: " + err.Error()), nil
	}

	if len(addFlags) > 0 {
		flags := make([]imap.Flag, len(addFlags))
		for i, f := range addFlags {
			flags[i] = imap.Flag(f)
		}
		if err := imaplib.SetFlags(c, uids, imap.StoreFlagsAdd, flags); err != nil {
			s.pool.Discard(account, c)
			return errorResult("adding flags: " + err.Error()), nil
		}
	}

	if len(removeFlags) > 0 {
		flags := make([]imap.Flag, len(removeFlags))
		for i, f := range removeFlags {
			flags[i] = imap.Flag(f)
		}
		if err := imaplib.SetFlags(c, uids, imap.StoreFlagsDel, flags); err != nil {
			s.pool.Discard(account, c)
			return errorResult("removing flags: " + err.Error()), nil
		}
	}

	// Write-through to indexer
	if s.indexer != nil {
		uidSet := imaplib.UIDSetFromUIDs(uids)
		msgs, err := c.Fetch(uidSet, &imap.FetchOptions{Flags: true, UID: true}).Collect()
		if err == nil {
			for _, msg := range msgs {
				var flags []string
				for _, f := range msg.Flags {
					flags = append(flags, string(f))
				}
				s.indexer.NotifyFlagsChanged(account, folder, uint32(msg.UID), flags)
			}
		}
	}

	return textResult(formatSetFlags(uids, addFlags, removeFlags)), nil
}

func (s *Server) handleCreateMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	from := stringFrom(args, "from")
	if from == "" {
		return errorResult("from parameter is required"), nil
	}
	subject := stringFrom(args, "subject")
	if subject == "" {
		return errorResult("subject parameter is required"), nil
	}
	body := stringFrom(args, "body")
	if body == "" {
		return errorResult("body parameter is required"), nil
	}

	params := imaplib.AppendParams{
		Folder:   stringFrom(args, "folder"),
		From:     from,
		To:       stringSliceFrom(args, "to"),
		CC:       stringSliceFrom(args, "cc"),
		Subject:  subject,
		Body:     body,
		HTMLBody: stringFrom(args, "html_body"),
		Flags:    stringSliceFrom(args, "flags"),
		Date:     stringFrom(args, "date"),
	}

	if len(params.Flags) == 0 {
		params.Flags = []string{"\\Draft"}
	}

	if len(params.To) == 0 {
		return errorResult("to parameter is required"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	result, err := imaplib.AppendMessage(ctx, c, params)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("creating message: " + err.Error()), nil
	}

	if s.indexer != nil && result.UID > 0 {
		s.indexer.NotifyAppend(account, result.Folder, result.UID, params)
	}

	folder := params.Folder
	if folder == "" {
		folder = "Drafts"
	}
	return textResult(formatAppendResult(result, folder)), nil
}

func (s *Server) handleCreateFolder(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	name := stringFrom(args, "name")
	if name == "" {
		return errorResult("name parameter is required"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	if err := imaplib.CreateFolder(ctx, c, name); err != nil {
		s.pool.Discard(account, c)
		return errorResult("creating folder: " + err.Error()), nil
	}

	return textResult(formatCreateFolder(name)), nil
}

func (s *Server) handleRenameFolder(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	folder := stringFrom(args, "folder")
	if folder == "" {
		return errorResult("folder parameter is required"), nil
	}
	newName := stringFrom(args, "new_name")
	if newName == "" {
		return errorResult("new_name parameter is required"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	if err := imaplib.RenameFolder(ctx, c, folder, newName); err != nil {
		s.pool.Discard(account, c)
		return errorResult("renaming folder: " + err.Error()), nil
	}

	if s.indexer != nil {
		s.indexer.NotifyRenameFolder(account, folder, newName)
	}

	return textResult(formatRenameFolder(folder, newName)), nil
}

func (s *Server) handleTrashFolder(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	folder := stringFrom(args, "folder")
	if folder == "" {
		return errorResult("folder parameter is required"), nil
	}

	if folder == "INBOX" {
		return errorResult("cannot trash INBOX"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	trashFolder, err := s.getTrashFolder(ctx, account, c)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	if folder == trashFolder {
		return errorResult("cannot trash the trash folder itself"), nil
	}

	result, err := imaplib.TrashFolder(ctx, c, folder, trashFolder)
	if err != nil {
		s.pool.Discard(account, c)
		return errorResult("trashing folder: " + err.Error()), nil
	}

	if s.indexer != nil {
		s.indexer.NotifyDeleteFolder(account, folder)
	}

	return textResult(formatTrashFolderResult(result, folder)), nil
}

func (s *Server) handleUnsubscribe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	account := stringFrom(args, "account")
	if account == "" {
		return errorResult("account parameter is required"), nil
	}
	folder := stringFrom(args, "folder")
	if folder == "" {
		return errorResult("folder parameter is required"), nil
	}
	uids := uint32SliceFrom(args, "uids")
	if len(uids) == 0 {
		return errorResult("uids parameter is required"), nil
	}

	c, err := s.pool.Get(ctx, account)
	if err != nil {
		return errorResult("connecting to account: " + err.Error()), nil
	}
	defer s.pool.Put(account, c)

	var results []string
	for _, uid := range uids {
		msg, err := imaplib.FetchMessage(ctx, c, folder, uid, false)
		if err != nil {
			s.pool.Discard(account, c)
			results = append(results, fmt.Sprintf("UID %d: error fetching message: %s", uid, err))
			continue
		}

		if msg.LLMailCreated {
			results = append(results, fmt.Sprintf("UID %d: cannot unsubscribe from a message created by llmail", uid))
			continue
		}

		unsub := msg.Unsubscribe
		if unsub == nil || !unsub.CanUnsubscribe {
			results = append(results, fmt.Sprintf("UID %d: no unsubscribe headers found", uid))
			continue
		}

		if unsub.OneClick && unsub.HTTPUrl != "" {
			if err := imaplib.PerformOneClickUnsubscribe(ctx, unsub.HTTPUrl); err != nil {
				results = append(results, fmt.Sprintf("UID %d: one-click unsubscribe failed: %s", uid, err))
			} else {
				results = append(results, fmt.Sprintf("UID %d: one-click unsubscribe successful", uid))
			}
		} else if unsub.MailtoAddress != "" {
			acc, ok := s.cfg.GetAccount(account)
			if !ok {
				results = append(results, fmt.Sprintf("UID %d: unknown account: %s", uid, account))
				continue
			}
			subject := unsub.MailtoSubject
			if subject == "" {
				subject = "Unsubscribe"
			}
			body := unsub.MailtoBody
			if body == "" {
				body = "Unsubscribe"
			}
			params := imaplib.AppendParams{
				Folder:  "Drafts",
				From:    acc.Username,
				To:      []string{unsub.MailtoAddress},
				Subject: subject,
				Body:    body,
				Flags:   []string{"\\Draft", "\\Seen"},
			}
			appendResult, err := imaplib.AppendMessage(ctx, c, params)
			if err != nil {
				results = append(results, fmt.Sprintf("UID %d: failed to create unsubscribe draft: %s", uid, err))
			} else {
				results = append(results, fmt.Sprintf("UID %d: unsubscribe draft created in Drafts (UID: %d) — review and send to complete", uid, appendResult.UID))
			}
		} else if unsub.HTTPUrl != "" {
			results = append(results, fmt.Sprintf("UID %d: manual unsubscribe required — open this URL in a browser: %s", uid, unsub.HTTPUrl))
		}
	}

	return textResult(strings.Join(results, "\n")), nil
}

func uint32From(args map[string]any, key string) uint32 {
	v, _ := args[key].(float64)
	return uint32(v)
}

func uint32SliceFrom(args map[string]any, key string) []uint32 {
	v, ok := args[key].([]any)
	if !ok {
		return nil
	}
	result := make([]uint32, 0, len(v))
	for _, item := range v {
		if f, ok := item.(float64); ok && f > 0 {
			result = append(result, uint32(f))
		}
	}
	return result
}

func stringSliceFrom(args map[string]any, key string) []string {
	v, ok := args[key].([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, item := range v {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
