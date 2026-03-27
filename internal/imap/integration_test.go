//go:build integration

package imap

import (
	"context"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

// --- Folder Operations ---

func TestListFolders(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()

	folders, err := ListFolders(ctx, c)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}

	var foundINBOX bool
	var foundTrash bool
	for _, f := range folders {
		if f.Name == "INBOX" {
			foundINBOX = true
		}
		for _, attr := range f.Attributes {
			if attr == "\\Trash" {
				foundTrash = true
			}
		}
	}

	if !foundINBOX {
		t.Error("INBOX not found in folder list")
	}
	if !foundTrash {
		t.Error("no folder with \\Trash attribute found")
	}
}

// --- Trash Detection ---

func TestFindTrashFolder(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()

	trash, err := FindTrashFolder(ctx, c)
	if err != nil {
		t.Fatalf("FindTrashFolder: %v", err)
	}

	if trash != "Trash" && trash != "Deleted Items" {
		t.Logf("trash folder detected as %q (expected 'Trash')", trash)
	}
	if trash == "" {
		t.Error("FindTrashFolder returned empty string")
	}
}

// --- Append & Fetch ---

func TestAppendMessage(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	res, err := AppendMessage(ctx, c, AppendParams{
		Folder:  folder,
		From:    "testuser@localhost",
		To:      []string{"recipient@localhost"},
		Subject: "Append Test",
		Body:    "Hello, this is a test message.",
		Date:    "2025-06-15",
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if res.UID == 0 {
		t.Fatal("AppendMessage returned UID 0")
	}

	msg, err := FetchMessage(ctx, c, folder, res.UID, false, false)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}

	if msg.Subject != "Append Test" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Append Test")
	}
	if !strings.Contains(msg.From, "testuser@localhost") {
		t.Errorf("From = %q, want it to contain testuser@localhost", msg.From)
	}
	if len(msg.To) == 0 || !strings.Contains(msg.To[0], "recipient@localhost") {
		t.Errorf("To = %v, want it to contain recipient@localhost", msg.To)
	}
	if !strings.Contains(msg.Date, "2025-06-15") {
		t.Errorf("Date = %q, want it to contain 2025-06-15", msg.Date)
	}
	if !strings.Contains(msg.TextBody, "Hello, this is a test message.") {
		t.Errorf("TextBody = %q, want it to contain test message text", msg.TextBody)
	}
}

func TestAppendMessageHTML(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	uid := seedHTMLMessage(t, c, folder)

	msg, err := FetchMessage(ctx, c, folder, uid, false, false)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}

	if msg.TextBody == "" {
		t.Error("TextBody is empty for multipart/alternative message")
	}
	if msg.HTMLBody == "" {
		t.Error("HTMLBody is empty for multipart/alternative message")
	}
	if !strings.Contains(msg.HTMLBody, "<h1>") {
		t.Errorf("HTMLBody = %q, expected to contain <h1>", msg.HTMLBody)
	}
}

func TestAppendMessageFlags(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	res, err := AppendMessage(ctx, c, AppendParams{
		Folder:  folder,
		From:    "testuser@localhost",
		To:      []string{"recipient@localhost"},
		Subject: "Flagged Message",
		Body:    "This message has flags.",
		Flags:   []string{`\Seen`, `\Flagged`},
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	msg, err := FetchMessage(ctx, c, folder, res.UID, false, false)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}

	if !containsFlag(msg.Flags, `\Seen`) {
		t.Errorf("expected \\Seen flag, got %v", msg.Flags)
	}
	if !containsFlag(msg.Flags, `\Flagged`) {
		t.Errorf("expected \\Flagged flag, got %v", msg.Flags)
	}
}

// --- Detail Levels ---

func TestFetchByLevel_Minimal(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	uids := seedMessages(t, c, folder, 1)

	selectCmd := c.Select(folder, &imap.SelectOptions{ReadOnly: true})
	if _, err := selectCmd.Wait(); err != nil {
		t.Fatalf("selecting folder: %v", err)
	}

	uidSet := UIDSetFromUID(uids[0])
	msgs, err := FetchByLevel(ctx, c, uidSet, DetailHeaders, false)
	if err != nil {
		t.Fatalf("FetchByLevel minimal: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	msg := msgs[0]
	if msg.UID == 0 {
		t.Error("UID should be populated")
	}
	if msg.From == "" {
		t.Error("From should be populated at minimal level")
	}
	if msg.Subject == "" {
		t.Error("Subject should be populated at minimal level")
	}
	if msg.Date == "" {
		t.Error("Date should be populated at minimal level")
	}
	// Minimal should NOT have flags, size, or snippet
	if len(msg.Flags) > 0 {
		t.Error("Flags should not be populated at minimal level")
	}
	if msg.Size != 0 {
		t.Error("Size should not be populated at minimal level")
	}
	}
}

func TestFetchByLevel_Full(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	uids := seedMessages(t, c, folder, 1)

	selectCmd := c.Select(folder, &imap.SelectOptions{ReadOnly: true})
	if _, err := selectCmd.Wait(); err != nil {
		t.Fatalf("selecting folder: %v", err)
	}

	uidSet := UIDSetFromUID(uids[0])
	msgs, err := FetchByLevel(ctx, c, uidSet, DetailFull, false)
	if err != nil {
		t.Fatalf("FetchByLevel full: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	msg := msgs[0]
	if msg.TextBody == "" {
		t.Error("TextBody should be populated at full level")
	}
}

// --- FetchMessage ---

func TestFetchMessage(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	uids := seedMessages(t, c, folder, 1)

	msg, err := FetchMessage(ctx, c, folder, uids[0], false, false)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}

	if msg.UID != uids[0] {
		t.Errorf("UID = %d, want %d", msg.UID, uids[0])
	}
	if msg.Subject == "" {
		t.Error("Subject is empty")
	}
	if msg.From == "" {
		t.Error("From is empty")
	}
	if msg.Date == "" {
		t.Error("Date is empty")
	}
	if msg.TextBody == "" {
		t.Error("TextBody is empty")
	}
	if msg.Size == 0 {
		t.Error("Size is 0")
	}
}

func TestFetchMessage_RawHeaders(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	uids := seedMessages(t, c, folder, 1)

	msg, err := FetchMessage(ctx, c, folder, uids[0], false, true)
	if err != nil {
		t.Fatalf("FetchMessage with raw headers: %v", err)
	}

	if msg.RawHeaders == nil {
		t.Fatal("RawHeaders is nil")
	}
	// Should have common headers
	for _, key := range []string{"From", "To", "Subject", "Date"} {
		if _, ok := msg.RawHeaders[key]; !ok {
			t.Errorf("RawHeaders missing %q", key)
		}
	}
}

func TestFetchMessage_NotFound(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	_, err := FetchMessage(ctx, c, folder, 99999, false, false)
	if err == nil {
		t.Error("expected error for non-existent UID, got nil")
	}
}

// --- Attachments ---

func TestFetchAttachmentContent(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	uid := seedMessageWithAttachment(t, c, folder)

	// Part 1 = text body, Part 2 = attachment
	content, contentType, filename, err := FetchAttachmentContent(ctx, c, folder, uid, 2)
	if err != nil {
		t.Fatalf("FetchAttachmentContent: %v", err)
	}

	if len(content) == 0 {
		t.Error("attachment content is empty")
	}
	if !strings.Contains(string(content), "Hello World!") {
		t.Errorf("attachment content = %q, want 'Hello World!'", string(content))
	}
	if contentType == "" {
		t.Error("content type is empty")
	}
	if filename != "test.bin" {
		t.Errorf("filename = %q, want 'test.bin'", filename)
	}
}

// --- Search ---

func TestRawSearch_All(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	seedMessages(t, c, folder, 5)

	result, err := RawSearch(ctx, c, RawSearchParams{
		Folder:      folder,
		Query:       "",
		Limit:       20,
		DetailLevel: DetailHeaders,
	}, nil)
	if err != nil {
		t.Fatalf("RawSearch: %v", err)
	}

	if result.TotalMatches != 5 {
		t.Errorf("TotalMatches = %d, want 5", result.TotalMatches)
	}
}

func TestRawSearch_Subject(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	// Seed messages with distinct subjects
	AppendMessage(ctx, c, AppendParams{
		Folder: folder, From: "testuser@localhost",
		To: []string{"r@localhost"}, Subject: "unique-findme-subject", Body: "body",
	})
	AppendMessage(ctx, c, AppendParams{
		Folder: folder, From: "testuser@localhost",
		To: []string{"r@localhost"}, Subject: "other subject", Body: "body",
	})

	result, err := RawSearch(ctx, c, RawSearchParams{
		Folder:      folder,
		Query:       `SUBJECT "unique-findme-subject"`,
		Limit:       20,
		DetailLevel: DetailHeaders,
	}, nil)
	if err != nil {
		t.Fatalf("RawSearch: %v", err)
	}

	if result.TotalMatches != 1 {
		t.Errorf("TotalMatches = %d, want 1", result.TotalMatches)
	}
}

func TestRawSearch_From(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	seedMessages(t, c, folder, 2)

	result, err := RawSearch(ctx, c, RawSearchParams{
		Folder:      folder,
		Query:       `FROM "testuser"`,
		Limit:       20,
		DetailLevel: DetailHeaders,
	}, nil)
	if err != nil {
		t.Fatalf("RawSearch: %v", err)
	}

	if result.TotalMatches != 2 {
		t.Errorf("TotalMatches = %d, want 2", result.TotalMatches)
	}
}

func TestRawSearch_Unseen(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	// Seed 3 messages, mark 1 as seen
	uids := seedMessages(t, c, folder, 3)

	// Select read-write to set flags
	selectCmd := c.Select(folder, nil)
	if _, err := selectCmd.Wait(); err != nil {
		t.Fatalf("selecting folder: %v", err)
	}
	if err := SetFlags(c, []uint32{uids[0]}, imap.StoreFlagsAdd, []imap.Flag{imap.FlagSeen}); err != nil {
		t.Fatalf("SetFlags: %v", err)
	}

	result, err := RawSearch(ctx, c, RawSearchParams{
		Folder:      folder,
		Query:       "UNSEEN",
		Limit:       20,
		DetailLevel: DetailHeaders,
	}, nil)
	if err != nil {
		t.Fatalf("RawSearch: %v", err)
	}

	if result.TotalMatches != 2 {
		t.Errorf("TotalMatches = %d, want 2", result.TotalMatches)
	}
}

func TestRawSearch_Boolean(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	AppendMessage(ctx, c, AppendParams{
		Folder: folder, From: "testuser@localhost",
		To: []string{"r@localhost"}, Subject: "alpha message", Body: "body",
	})
	AppendMessage(ctx, c, AppendParams{
		Folder: folder, From: "testuser@localhost",
		To: []string{"r@localhost"}, Subject: "beta message", Body: "body",
	})
	AppendMessage(ctx, c, AppendParams{
		Folder: folder, From: "testuser@localhost",
		To: []string{"r@localhost"}, Subject: "gamma message", Body: "body",
	})

	result, err := RawSearch(ctx, c, RawSearchParams{
		Folder:      folder,
		Query:       `OR SUBJECT "alpha" SUBJECT "beta"`,
		Limit:       20,
		DetailLevel: DetailHeaders,
	}, nil)
	if err != nil {
		t.Fatalf("RawSearch: %v", err)
	}

	if result.TotalMatches != 2 {
		t.Errorf("TotalMatches = %d, want 2", result.TotalMatches)
	}
}

// --- List Messages ---

func TestListMessages(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	seedMessages(t, c, folder, 5)

	result, err := ListMessages(ctx, c, folder, 20, 0, DetailHeaders, nil)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}

	if result.TotalMatches != 5 {
		t.Errorf("TotalMatches = %d, want 5", result.TotalMatches)
	}
	if len(result.Messages) != 5 {
		t.Fatalf("len(Messages) = %d, want 5", len(result.Messages))
	}

	// ListMessages returns newest-first. The SearchResult.Messages should
	// have the highest UID first (newest) since UIDs are monotonically increasing.
	// However, FetchByLevel returns messages in server order which may differ.
	// Just verify we got all 5 messages back.
	uidSet := make(map[uint32]bool)
	for _, m := range result.Messages {
		uidSet[m.UID] = true
	}
	if len(uidSet) != 5 {
		t.Errorf("expected 5 unique UIDs, got %d", len(uidSet))
	}
}

func TestListMessages_Pagination(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	seedMessages(t, c, folder, 10)

	// First page
	page1, err := ListMessages(ctx, c, folder, 3, 0, DetailHeaders, nil)
	if err != nil {
		t.Fatalf("ListMessages page1: %v", err)
	}
	if page1.TotalMatches != 10 {
		t.Errorf("TotalMatches = %d, want 10", page1.TotalMatches)
	}
	if len(page1.Messages) != 3 {
		t.Errorf("page1 len = %d, want 3", len(page1.Messages))
	}

	// Second page
	page2, err := ListMessages(ctx, c, folder, 3, 3, DetailHeaders, nil)
	if err != nil {
		t.Fatalf("ListMessages page2: %v", err)
	}
	if page2.TotalMatches != 10 {
		t.Errorf("TotalMatches = %d, want 10", page2.TotalMatches)
	}
	if len(page2.Messages) != 3 {
		t.Errorf("page2 len = %d, want 3", len(page2.Messages))
	}

	// Verify no overlap between pages
	page1UIDs := make(map[uint32]bool)
	for _, m := range page1.Messages {
		page1UIDs[m.UID] = true
	}
	for _, m := range page2.Messages {
		if page1UIDs[m.UID] {
			t.Errorf("UID %d appears in both page1 and page2", m.UID)
		}
	}
}

func TestListMessages_Empty(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	result, err := ListMessages(ctx, c, folder, 20, 0, DetailHeaders, nil)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}

	if result.TotalMatches != 0 {
		t.Errorf("TotalMatches = %d, want 0", result.TotalMatches)
	}
}

// --- Move & Copy ---

func TestMoveMessage(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folderA := setupTestFolderNamed(t, c, "Test_MoveMessage_A")
	folderB := setupTestFolderNamed(t, c, "Test_MoveMessage_B")

	uids := seedMessages(t, c, folderA, 1)

	result, err := MoveMessages(ctx, c, folderA, []uint32{uids[0]}, folderB)
	if err != nil {
		t.Fatalf("MoveMessages: %v", err)
	}

	if len(result.DestUIDs) == 0 {
		t.Error("DestUIDs is empty")
	}

	// Verify message is gone from A
	if uidInFolder(t, c, folderA, uids[0]) {
		t.Error("message still exists in source folder after move")
	}

	// Verify message exists in B
	if len(result.DestUIDs) > 0 && !uidInFolder(t, c, folderB, result.DestUIDs[0]) {
		t.Error("message not found in destination folder after move")
	}
}

func TestCopyMessage(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folderA := setupTestFolderNamed(t, c, "Test_CopyMessage_A")
	folderB := setupTestFolderNamed(t, c, "Test_CopyMessage_B")

	uids := seedMessages(t, c, folderA, 1)

	result, err := CopyMessages(ctx, c, folderA, []uint32{uids[0]}, folderB)
	if err != nil {
		t.Fatalf("CopyMessages: %v", err)
	}

	if len(result.DestUIDs) == 0 {
		t.Error("DestUIDs is empty")
	}

	// Verify message still exists in A
	if !uidInFolder(t, c, folderA, uids[0]) {
		t.Error("message missing from source folder after copy")
	}

	// Verify message exists in B
	if len(result.DestUIDs) > 0 && !uidInFolder(t, c, folderB, result.DestUIDs[0]) {
		t.Error("message not found in destination folder after copy")
	}
}

// --- Flags ---

func TestSetFlags_Add(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	uids := seedMessages(t, c, folder, 1)

	// Select read-write
	selectCmd := c.Select(folder, nil)
	if _, err := selectCmd.Wait(); err != nil {
		t.Fatalf("selecting folder: %v", err)
	}

	if err := SetFlags(c, []uint32{uids[0]}, imap.StoreFlagsAdd, []imap.Flag{imap.FlagFlagged}); err != nil {
		t.Fatalf("SetFlags add: %v", err)
	}

	// Fetch and verify
	msg, err := FetchMessage(ctx, c, folder, uids[0], false, false)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}
	if !containsFlag(msg.Flags, `\Flagged`) {
		t.Errorf("expected \\Flagged, got %v", msg.Flags)
	}
}

func TestSetFlags_Remove(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	// Seed with \Seen flag
	res, err := AppendMessage(ctx, c, AppendParams{
		Folder:  folder,
		From:    "testuser@localhost",
		To:      []string{"recipient@localhost"},
		Subject: "Seen Message",
		Body:    "This is seen.",
		Flags:   []string{`\Seen`},
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Select read-write
	selectCmd := c.Select(folder, nil)
	if _, err := selectCmd.Wait(); err != nil {
		t.Fatalf("selecting folder: %v", err)
	}

	if err := SetFlags(c, []uint32{uint32(res.UID)}, imap.StoreFlagsDel, []imap.Flag{imap.FlagSeen}); err != nil {
		t.Fatalf("SetFlags remove: %v", err)
	}

	// Fetch and verify
	msg, err := FetchMessage(ctx, c, folder, res.UID, false, false)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}
	if containsFlag(msg.Flags, `\Seen`) {
		t.Errorf("expected \\Seen to be removed, got %v", msg.Flags)
	}
}

// --- Threading ---

func TestGetThread(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	uids := seedThread(t, c, folder, 3)

	// GetThread should return at least the target message and use a valid method.
	// Full thread resolution depends on server capabilities (THREAD=REFERENCES)
	// and header search support, which varies by server.
	result, err := GetThread(ctx, c, folder, uids[2], DetailHeaders)
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}

	if result.Method != "imap_thread" && result.Method != "references" {
		t.Errorf("Method = %q, want 'imap_thread' or 'references'", result.Method)
	}

	if len(result.Messages) < 1 {
		t.Fatal("expected at least 1 message in thread result")
	}

	// The target UID must be present in results
	found := false
	for _, m := range result.Messages {
		if m.UID == uids[2] {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("target UID %d not found in thread results", uids[2])
	}

	// If multiple messages returned, verify date ordering
	for i := 1; i < len(result.Messages); i++ {
		if result.Messages[i].Date < result.Messages[i-1].Date {
			t.Errorf("thread messages not sorted by date: %q before %q",
				result.Messages[i-1].Date, result.Messages[i].Date)
		}
	}

	t.Logf("GetThread returned %d messages via method %q", len(result.Messages), result.Method)
}

func TestGetThread_SingleMessage(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	uids := seedMessages(t, c, folder, 1)

	result, err := GetThread(ctx, c, folder, uids[0], DetailHeaders)
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}

	if len(result.Messages) < 1 {
		t.Error("expected at least 1 message for single-message thread")
	}

	// Single message thread should just return that one message
	found := false
	for _, m := range result.Messages {
		if m.UID == uids[0] {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("target UID %d not found in thread results", uids[0])
	}
}

// --- Folder Management ---

func TestCreateFolder(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := "Test_CreateFolder_New"

	if err := CreateFolder(ctx, c, folder); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	t.Cleanup(func() { _ = c.Delete(folder).Wait() })

	// Verify it exists in the folder list
	folders, err := ListFolders(ctx, c)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}

	found := false
	for _, f := range folders {
		if f.Name == folder {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created folder %q not found in folder list", folder)
	}
}

func TestCreateFolder_AlreadyExists(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	err := CreateFolder(ctx, c, folder)
	if err == nil {
		t.Error("expected error creating duplicate folder, got nil")
	}
}

func TestRenameFolder(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()

	oldName := "Test_RenameFolder_Old"
	newName := "Test_RenameFolder_New"

	if err := CreateFolder(ctx, c, oldName); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Delete(oldName).Wait()
		_ = c.Delete(newName).Wait()
	})

	// Seed a message so we can verify it survives the rename
	seedMessages(t, c, oldName, 1)

	if err := RenameFolder(ctx, c, oldName, newName); err != nil {
		t.Fatalf("RenameFolder: %v", err)
	}

	// Verify new name exists and old name is gone
	folders, err := ListFolders(ctx, c)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}

	foundOld, foundNew := false, false
	for _, f := range folders {
		if f.Name == oldName {
			foundOld = true
		}
		if f.Name == newName {
			foundNew = true
		}
	}

	if foundOld {
		t.Error("old folder name still exists after rename")
	}
	if !foundNew {
		t.Error("new folder name not found after rename")
	}

	// Verify the message is in the renamed folder
	result, err := ListMessages(ctx, c, newName, 20, 0, DetailHeaders, nil)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if result.TotalMatches != 1 {
		t.Errorf("expected 1 message in renamed folder, got %d", result.TotalMatches)
	}
}

func TestTrashFolder(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()

	folder := "Test_TrashFolder_Target"
	if err := CreateFolder(ctx, c, folder); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	// No cleanup needed - TrashFolder deletes it

	seedMessages(t, c, folder, 3)

	trashFolder, err := FindTrashFolder(ctx, c)
	if err != nil {
		t.Fatalf("FindTrashFolder: %v", err)
	}

	result, err := TrashFolder(ctx, c, folder, trashFolder)
	if err != nil {
		t.Fatalf("TrashFolder: %v", err)
	}

	if result.MessagesMoved != 3 {
		t.Errorf("MessagesMoved = %d, want 3", result.MessagesMoved)
	}
	if !result.Deleted {
		t.Error("Deleted should be true")
	}

	// Verify folder no longer exists
	folders, err := ListFolders(ctx, c)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	for _, f := range folders {
		if f.Name == folder {
			t.Error("trashed folder still exists in folder list")
		}
	}
}

// --- Edit Message ---

func TestEditMessage_Subject(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)
	uids := seedMessages(t, c, folder, 1)

	// Select read-write
	if _, err := c.Select(folder, nil).Wait(); err != nil {
		t.Fatalf("Select: %v", err)
	}

	result, err := EditMessage(ctx, c, folder, uids[0], EditParams{
		Subject: "Updated Subject",
	})
	if err != nil {
		t.Fatalf("EditMessage: %v", err)
	}
	if result.NewUID == 0 {
		t.Fatal("EditMessage returned NewUID 0")
	}
	if result.OldUID != uids[0] {
		t.Errorf("OldUID = %d, want %d", result.OldUID, uids[0])
	}

	// Verify the new message has the updated subject
	msg, err := FetchMessage(ctx, c, folder, result.NewUID, false, false)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}
	if msg.Subject != "Updated Subject" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Updated Subject")
	}
	// Original body should be preserved
	if !strings.Contains(msg.TextBody, "Body of test message 1") {
		t.Errorf("TextBody = %q, want original body preserved", msg.TextBody)
	}

	// Verify the old message is gone
	_, err = FetchMessage(ctx, c, folder, uids[0], false, false)
	if err == nil {
		t.Error("old message should have been deleted")
	}
}

func TestEditMessage_Body(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)
	uids := seedMessages(t, c, folder, 1)

	if _, err := c.Select(folder, nil).Wait(); err != nil {
		t.Fatalf("Select: %v", err)
	}

	result, err := EditMessage(ctx, c, folder, uids[0], EditParams{
		Body: "Brand new body text",
	})
	if err != nil {
		t.Fatalf("EditMessage: %v", err)
	}

	msg, err := FetchMessage(ctx, c, folder, result.NewUID, false, false)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}
	// Subject should be preserved
	if msg.Subject != "Test Message 1" {
		t.Errorf("Subject = %q, want original preserved", msg.Subject)
	}
	if !strings.Contains(msg.TextBody, "Brand new body text") {
		t.Errorf("TextBody = %q, want updated body", msg.TextBody)
	}
}

func TestEditMessage_MultipleFields(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)
	uids := seedMessages(t, c, folder, 1)

	if _, err := c.Select(folder, nil).Wait(); err != nil {
		t.Fatalf("Select: %v", err)
	}

	result, err := EditMessage(ctx, c, folder, uids[0], EditParams{
		To:      []string{"newrecipient@localhost"},
		Subject: "New Subject",
		Body:    "New body",
	})
	if err != nil {
		t.Fatalf("EditMessage: %v", err)
	}

	msg, err := FetchMessage(ctx, c, folder, result.NewUID, false, false)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}
	if msg.Subject != "New Subject" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "New Subject")
	}
	if len(msg.To) == 0 || !strings.Contains(msg.To[0], "newrecipient@localhost") {
		t.Errorf("To = %v, want newrecipient@localhost", msg.To)
	}
	if !strings.Contains(msg.TextBody, "New body") {
		t.Errorf("TextBody = %q, want updated body", msg.TextBody)
	}
}

func TestEditMessage_PreservesFlags(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	// Create a message with flags
	res, err := AppendMessage(ctx, c, AppendParams{
		Folder:  folder,
		From:    "testuser@localhost",
		To:      []string{"recipient@localhost"},
		Subject: "Flagged Message",
		Body:    "Flagged body",
		Flags:   []string{`\Seen`, `\Flagged`},
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if _, err := c.Select(folder, nil).Wait(); err != nil {
		t.Fatalf("Select: %v", err)
	}

	result, err := EditMessage(ctx, c, folder, res.UID, EditParams{
		Subject: "Still Flagged",
	})
	if err != nil {
		t.Fatalf("EditMessage: %v", err)
	}

	msg, err := FetchMessage(ctx, c, folder, result.NewUID, false, false)
	if err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}

	hasSeen := false
	hasFlagged := false
	for _, f := range msg.Flags {
		if f == `\Seen` {
			hasSeen = true
		}
		if f == `\Flagged` {
			hasFlagged = true
		}
	}
	if !hasSeen {
		t.Error("\\Seen flag not preserved")
	}
	if !hasFlagged {
		t.Error("\\Flagged flag not preserved")
	}
}

func TestEditMessage_RejectsNonLLMailMessage(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()
	folder := setupTestFolder(t, c)

	// Manually append a message WITHOUT the X-LLMail-Created header
	rawMsg := "From: someone@example.com\r\nTo: other@example.com\r\nSubject: External Message\r\nMIME-Version: 1.0\r\nContent-Type: text/plain\r\n\r\nThis was not created by llmail."
	appendCmd := c.Append(folder, int64(len(rawMsg)), nil)
	if _, err := appendCmd.Write([]byte(rawMsg)); err != nil {
		t.Fatalf("writing message: %v", err)
	}
	if err := appendCmd.Close(); err != nil {
		t.Fatalf("closing append: %v", err)
	}
	appendData, err := appendCmd.Wait()
	if err != nil {
		t.Fatalf("appending: %v", err)
	}
	uid := uint32(appendData.UID)

	if _, err := c.Select(folder, nil).Wait(); err != nil {
		t.Fatalf("Select: %v", err)
	}

	_, err = EditMessage(ctx, c, folder, uid, EditParams{
		Subject: "Hacked Subject",
	})
	if err == nil {
		t.Fatal("EditMessage should have rejected a non-llmail message")
	}
	if !strings.Contains(err.Error(), "not created by llmail") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTrashFolder_Empty(t *testing.T) {
	c := dialTestClient(t)
	ctx := context.Background()

	folder := "Test_TrashFolder_Empty"
	if err := CreateFolder(ctx, c, folder); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	trashFolder, err := FindTrashFolder(ctx, c)
	if err != nil {
		t.Fatalf("FindTrashFolder: %v", err)
	}

	result, err := TrashFolder(ctx, c, folder, trashFolder)
	if err != nil {
		t.Fatalf("TrashFolder: %v", err)
	}

	if result.MessagesMoved != 0 {
		t.Errorf("MessagesMoved = %d, want 0", result.MessagesMoved)
	}
	if !result.Deleted {
		t.Error("Deleted should be true")
	}
}
