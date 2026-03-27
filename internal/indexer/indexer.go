package indexer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	bleveQuery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	imaplib "github.com/jpennington/llmail/internal/imap"

	"github.com/jpennington/llmail/internal/config"
)

type Indexer struct {
	cfg       *config.Config
	index     bleve.Index
	state     *SyncState
	dataDir   string
	stopCh    chan struct{}
	stoppedCh chan struct{}
	mu        sync.Mutex
	running   bool
}

type SearchResult struct {
	TotalMatches int         `json:"total_matches"`
	Offset       int         `json:"offset"`
	Messages     []SearchHit `json:"messages"`
}

type SearchHit struct {
	Score   float64 `json:"score"`
	Account string  `json:"account"`
	Folder  string  `json:"folder"`
	UID     uint32  `json:"uid"`
	From    string  `json:"from"`
	To      string  `json:"to"`
	Subject string  `json:"subject"`
	Date    string  `json:"date"`
	Snippet string  `json:"snippet,omitempty"`
}

func New(cfg *config.Config) (*Indexer, error) {
	dataDir := config.DataDir()
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	indexPath := filepath.Join(dataDir, "index.bleve")

	var idx bleve.Index
	var err error

	if _, statErr := os.Stat(indexPath); os.IsNotExist(statErr) {
		idx, err = bleve.New(indexPath, buildIndexMapping())
	} else {
		idx, err = bleve.Open(indexPath)
	}
	if err != nil {
		return nil, fmt.Errorf("opening index: %w", err)
	}

	state, err := loadSyncState(dataDir)
	if err != nil {
		idx.Close()
		return nil, fmt.Errorf("loading sync state: %w", err)
	}

	return &Indexer{
		cfg:       cfg,
		index:     idx,
		state:     state,
		dataDir:   dataDir,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}, nil
}

func (idx *Indexer) Start(ctx context.Context, pool *imaplib.Pool) {
	idx.mu.Lock()
	if idx.running {
		idx.mu.Unlock()
		return
	}
	idx.running = true
	idx.mu.Unlock()

	go idx.syncLoop(ctx, pool)
}

func (idx *Indexer) Stop() {
	idx.mu.Lock()
	if !idx.running {
		idx.mu.Unlock()
		return
	}
	idx.mu.Unlock()

	close(idx.stopCh)
	<-idx.stoppedCh
	idx.index.Close()
}

// --- Write-through methods (called by MCP tool handlers) ---

// NotifyAppend updates the index after a message was appended via create_draft.
func (idx *Indexer) NotifyAppend(account, folder string, uid uint32, params imaplib.AppendParams) {
	doc := IndexDocument{
		Account: account,
		Folder:  folder,
		UID:     uid,
		From:    params.From,
		To:      strings.Join(params.To, ", "),
		CC:      strings.Join(params.CC, ", "),
		Subject: params.Subject,
		Body:    params.Body,
		Date:    time.Now().Format(time.RFC3339),
		Flags:   strings.Join(params.Flags, " "),
	}

	if params.Date != "" {
		if t, err := time.Parse("2006-01-02", params.Date); err == nil {
			doc.Date = t.Format(time.RFC3339)
		}
	}

	docID := fmt.Sprintf("%s/%s/%d", account, folder, uid)
	if err := idx.index.Index(docID, doc); err != nil {
		slog.Error("Write-through index failed", "docID", docID, "error", err)
		return
	}

	state := idx.state.GetFolder(account, folder)
	state.AddUID(uid)
	state.IndexedCount = len(state.IndexedUIDs)
	if uid > state.LastUID {
		state.LastUID = uid
	}
	idx.state.SetFolder(account, folder, state)
	_ = idx.state.Save()
}

// NotifyDelete removes a message from the index after it was moved/deleted.
func (idx *Indexer) NotifyDelete(account, folder string, uid uint32) {
	docID := fmt.Sprintf("%s/%s/%d", account, folder, uid)
	if err := idx.index.Delete(docID); err != nil {
		slog.Error("Write-through delete failed", "docID", docID, "error", err)
		return
	}

	state := idx.state.GetFolder(account, folder)
	state.RemoveUID(uid)
	state.IndexedCount = len(state.IndexedUIDs)
	idx.state.SetFolder(account, folder, state)
	_ = idx.state.Save()
}

// NotifyMove updates the index after a message was moved between folders.
// It re-keys the document from source to destination.
func (idx *Indexer) NotifyMove(account, srcFolder string, srcUID uint32, destFolder string, destUID uint32) {
	oldID := fmt.Sprintf("%s/%s/%d", account, srcFolder, srcUID)
	newID := fmt.Sprintf("%s/%s/%d", account, destFolder, destUID)

	// Read the existing document's indexed fields via search
	q := bleve.NewDocIDQuery([]string{oldID})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"from", "to", "cc", "subject", "body", "date", "flags", "message_id", "size"}
	req.Size = 1

	results, err := idx.index.Search(req)
	if err != nil || results.Total == 0 {
		// Doc not indexed (maybe folder isn't tracked) - just delete old
		_ = idx.index.Delete(oldID)
		idx.removeUIDFromState(account, srcFolder, srcUID)
		return
	}

	hit := results.Hits[0]
	doc := IndexDocument{
		Account: account,
		Folder:  destFolder,
		UID:     destUID,
	}
	if v, ok := hit.Fields["from"].(string); ok {
		doc.From = v
	}
	if v, ok := hit.Fields["to"].(string); ok {
		doc.To = v
	}
	if v, ok := hit.Fields["cc"].(string); ok {
		doc.CC = v
	}
	if v, ok := hit.Fields["subject"].(string); ok {
		doc.Subject = v
	}
	if v, ok := hit.Fields["body"].(string); ok {
		doc.Body = v
	}
	if v, ok := hit.Fields["date"].(string); ok {
		doc.Date = v
	}
	if v, ok := hit.Fields["flags"].(string); ok {
		doc.Flags = v
	}
	if v, ok := hit.Fields["message_id"].(string); ok {
		doc.MessageID = v
	}
	if v, ok := hit.Fields["size"].(float64); ok {
		doc.Size = uint32(v)
	}

	batch := idx.index.NewBatch()
	batch.Delete(oldID)
	batch.Index(newID, doc)
	if err := idx.index.Batch(batch); err != nil {
		slog.Error("Write-through move failed", "from", oldID, "to", newID, "error", err)
		return
	}

	// Update sync state for both folders
	idx.removeUIDFromState(account, srcFolder, srcUID)
	idx.addUIDToState(account, destFolder, destUID)
}

// NotifyCopy updates the index after a message was copied to another folder.
func (idx *Indexer) NotifyCopy(account, srcFolder string, srcUID uint32, destFolder string, destUID uint32) {
	oldID := fmt.Sprintf("%s/%s/%d", account, srcFolder, srcUID)
	newID := fmt.Sprintf("%s/%s/%d", account, destFolder, destUID)

	// Read source doc
	q := bleve.NewDocIDQuery([]string{oldID})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"from", "to", "cc", "subject", "body", "date", "flags", "message_id", "size"}
	req.Size = 1

	results, err := idx.index.Search(req)
	if err != nil || results.Total == 0 {
		return // Source not indexed - nothing to copy
	}

	hit := results.Hits[0]
	doc := IndexDocument{
		Account: account,
		Folder:  destFolder,
		UID:     destUID,
	}
	if v, ok := hit.Fields["from"].(string); ok {
		doc.From = v
	}
	if v, ok := hit.Fields["to"].(string); ok {
		doc.To = v
	}
	if v, ok := hit.Fields["cc"].(string); ok {
		doc.CC = v
	}
	if v, ok := hit.Fields["subject"].(string); ok {
		doc.Subject = v
	}
	if v, ok := hit.Fields["body"].(string); ok {
		doc.Body = v
	}
	if v, ok := hit.Fields["date"].(string); ok {
		doc.Date = v
	}
	if v, ok := hit.Fields["flags"].(string); ok {
		doc.Flags = v
	}
	if v, ok := hit.Fields["message_id"].(string); ok {
		doc.MessageID = v
	}
	if v, ok := hit.Fields["size"].(float64); ok {
		doc.Size = uint32(v)
	}

	if err := idx.index.Index(newID, doc); err != nil {
		slog.Error("Write-through copy failed", "docID", newID, "error", err)
		return
	}

	idx.addUIDToState(account, destFolder, destUID)
}

// NotifyFlagsChanged updates the flags field on an indexed document.
func (idx *Indexer) NotifyFlagsChanged(account, folder string, uid uint32, flags []string) {
	docID := fmt.Sprintf("%s/%s/%d", account, folder, uid)

	// Read existing doc
	q := bleve.NewDocIDQuery([]string{docID})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"from", "to", "cc", "subject", "body", "date", "message_id", "size"}
	req.Size = 1

	results, err := idx.index.Search(req)
	if err != nil || results.Total == 0 {
		return // Not indexed
	}

	hit := results.Hits[0]
	doc := IndexDocument{
		Account: account,
		Folder:  folder,
		UID:     uid,
		Flags:   strings.Join(flags, " "),
	}
	if v, ok := hit.Fields["from"].(string); ok {
		doc.From = v
	}
	if v, ok := hit.Fields["to"].(string); ok {
		doc.To = v
	}
	if v, ok := hit.Fields["cc"].(string); ok {
		doc.CC = v
	}
	if v, ok := hit.Fields["subject"].(string); ok {
		doc.Subject = v
	}
	if v, ok := hit.Fields["body"].(string); ok {
		doc.Body = v
	}
	if v, ok := hit.Fields["date"].(string); ok {
		doc.Date = v
	}
	if v, ok := hit.Fields["message_id"].(string); ok {
		doc.MessageID = v
	}
	if v, ok := hit.Fields["size"].(float64); ok {
		doc.Size = uint32(v)
	}

	if err := idx.index.Index(docID, doc); err != nil {
		slog.Error("Write-through flags update failed", "docID", docID, "error", err)
	}
}

// NotifyRenameFolder re-keys all indexed documents from old folder name to new.
func (idx *Indexer) NotifyRenameFolder(account, oldName, newName string) {
	prefix := fmt.Sprintf("%s/%s/", account, oldName)
	q := bleve.NewPrefixQuery(prefix)
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Fields = []string{"uid", "from", "to", "cc", "subject", "body", "date", "flags", "message_id", "size"}
	searchReq.Size = 1000

	for {
		results, err := idx.index.Search(searchReq)
		if err != nil || results.Total == 0 {
			break
		}

		batch := idx.index.NewBatch()
		for _, hit := range results.Hits {
			batch.Delete(hit.ID)
			doc := IndexDocument{
				Account: account,
				Folder:  newName,
			}
			if v, ok := hit.Fields["uid"].(float64); ok {
				doc.UID = uint32(v)
			}
			if v, ok := hit.Fields["from"].(string); ok {
				doc.From = v
			}
			if v, ok := hit.Fields["to"].(string); ok {
				doc.To = v
			}
			if v, ok := hit.Fields["cc"].(string); ok {
				doc.CC = v
			}
			if v, ok := hit.Fields["subject"].(string); ok {
				doc.Subject = v
			}
			if v, ok := hit.Fields["body"].(string); ok {
				doc.Body = v
			}
			if v, ok := hit.Fields["date"].(string); ok {
				doc.Date = v
			}
			if v, ok := hit.Fields["flags"].(string); ok {
				doc.Flags = v
			}
			if v, ok := hit.Fields["message_id"].(string); ok {
				doc.MessageID = v
			}
			if v, ok := hit.Fields["size"].(float64); ok {
				doc.Size = uint32(v)
			}

			newID := fmt.Sprintf("%s/%s/%d", account, newName, doc.UID)
			batch.Index(newID, doc)
		}
		if err := idx.index.Batch(batch); err != nil {
			slog.Error("Write-through rename batch failed", "error", err)
			break
		}

		if results.Total <= uint64(searchReq.Size) {
			break
		}
	}

	idx.state.RenameFolder(account, oldName, newName)
	_ = idx.state.Save()
}

// NotifyDeleteFolder removes all indexed documents for a folder.
func (idx *Indexer) NotifyDeleteFolder(account, folder string) {
	idx.deleteFolder(account, folder)
	_ = idx.state.Save()
}

func (idx *Indexer) removeUIDFromState(account, folder string, uid uint32) {
	state := idx.state.GetFolder(account, folder)
	state.RemoveUID(uid)
	state.IndexedCount = len(state.IndexedUIDs)
	idx.state.SetFolder(account, folder, state)
	_ = idx.state.Save()
}

func (idx *Indexer) addUIDToState(account, folder string, uid uint32) {
	state := idx.state.GetFolder(account, folder)
	state.AddUID(uid)
	state.IndexedCount = len(state.IndexedUIDs)
	if uid > state.LastUID {
		state.LastUID = uid
	}
	idx.state.SetFolder(account, folder, state)
	_ = idx.state.Save()
}

// --- Background sync loop ---

func (idx *Indexer) syncLoop(ctx context.Context, pool *imaplib.Pool) {
	defer close(idx.stoppedCh)

	// Initial sync
	idx.syncAll(ctx, pool)

	interval := time.Duration(idx.cfg.Index.SyncIntervalMinutes) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-idx.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			idx.syncAll(ctx, pool)
		}
	}
}

func (idx *Indexer) syncAll(ctx context.Context, pool *imaplib.Pool) {
	for _, account := range idx.cfg.AccountNames() {
		select {
		case <-idx.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		folders := foldersToSync(idx.cfg, account)
		if folders == nil && idx.cfg.Index.Mode == "all" {
			c, err := pool.Get(ctx, account)
			if err != nil {
				slog.Error("Failed to get connection for folder listing", "account", account, "error", err)
				continue
			}
			folderInfos, err := imaplib.ListFolders(ctx, c)
			pool.Put(account, c)
			if err != nil {
				slog.Error("Failed to list folders", "account", account, "error", err)
				continue
			}
			for _, fi := range folderInfos {
				folders = append(folders, fi.Name)
			}
		}

		for _, folder := range folders {
			select {
			case <-idx.stopCh:
				return
			case <-ctx.Done():
				return
			default:
			}

			if err := idx.syncFolder(ctx, pool, account, folder); err != nil {
				slog.Error("Failed to sync folder", "account", account, "folder", folder, "error", err)
			}
		}
	}
}

func (idx *Indexer) syncFolder(ctx context.Context, pool *imaplib.Pool, account, folder string) error {
	c, err := pool.Get(ctx, account)
	if err != nil {
		return fmt.Errorf("getting connection: %w", err)
	}
	defer pool.Put(account, c)

	mbox, err := c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		pool.Discard(account, c)
		return fmt.Errorf("selecting folder: %w", err)
	}

	currentValidity := mbox.UIDValidity
	currentUIDNext := uint32(mbox.UIDNext)
	currentCount := mbox.NumMessages
	state := idx.state.GetFolder(account, folder)

	// UIDVALIDITY changed - all UIDs are invalid, must re-index
	if state.UIDValidity != 0 && state.UIDValidity != currentValidity {
		slog.Info("UIDVALIDITY changed, re-indexing folder", "account", account, "folder", folder)
		idx.deleteFolder(account, folder)
		state = &FolderSyncState{Status: "new"}
	}

	// Fast path: nothing changed at all
	if state.UIDNext > 0 && currentUIDNext == state.UIDNext && currentCount == uint32(state.IndexedCount) {
		state.LastSyncTime = time.Now()
		idx.state.SetFolder(account, folder, state)
		return nil
	}

	// Check for new messages (UIDNEXT increased)
	if currentUIDNext > state.UIDNext || state.UIDNext == 0 {
		if err := idx.syncNewMessages(ctx, c, account, folder, state, currentValidity, currentUIDNext); err != nil {
			return err
		}
	}

	// Check for deletions (server count < our indexed count)
	if currentCount < uint32(len(state.IndexedUIDs)) && len(state.IndexedUIDs) > 0 {
		if err := idx.reconcileDeletedMessages(ctx, c, account, folder, state); err != nil {
			slog.Warn("Deletion reconciliation failed", "account", account, "folder", folder, "error", err)
			// Non-fatal - stale entries will be cleaned up next cycle
		}
	}

	state.UIDValidity = currentValidity
	state.UIDNext = currentUIDNext
	state.Status = "synced"
	state.LastSyncTime = time.Now()
	state.IndexedCount = len(state.IndexedUIDs)
	idx.state.SetFolder(account, folder, state)
	_ = idx.state.Save()

	return nil
}

// syncNewMessages fetches and indexes messages with UIDs > LastUID.
func (idx *Indexer) syncNewMessages(ctx context.Context, c *imapclient.Client, account, folder string, state *FolderSyncState, validity, uidNext uint32) error {
	criteria := &imap.SearchCriteria{}
	if state.LastUID > 0 {
		uidSet := imap.UIDSet{}
		uidSet.AddRange(imap.UID(state.LastUID+1), 0)
		criteria.UID = []imap.UIDSet{uidSet}
	}

	searchData, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return fmt.Errorf("searching for new messages: %w", err)
	}

	newUIDs := searchData.AllUIDs()
	if len(newUIDs) == 0 {
		return nil
	}

	slog.Info("Indexing new messages", "account", account, "folder", folder, "count", len(newUIDs))
	state.Status = "syncing"
	idx.state.SetFolder(account, folder, state)

	batchSize := idx.cfg.Index.BatchSize
	for i := 0; i < len(newUIDs); i += batchSize {
		select {
		case <-idx.stopCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := i + batchSize
		if end > len(newUIDs) {
			end = len(newUIDs)
		}
		batch := newUIDs[i:end]

		uidSet := imap.UIDSet{}
		for _, uid := range batch {
			uidSet.AddNum(uid)
		}

		if err := idx.fetchAndIndex(ctx, c, account, folder, uidSet); err != nil {
			slog.Error("Failed to index batch", "account", account, "folder", folder, "error", err)
			continue
		}

		// Track indexed UIDs
		for _, uid := range batch {
			state.AddUID(uint32(uid))
		}

		state.LastUID = uint32(batch[len(batch)-1])
		state.IndexedCount = len(state.IndexedUIDs)
		state.UIDValidity = validity
		state.UIDNext = uidNext
		state.LastSyncTime = time.Now()
		idx.state.SetFolder(account, folder, state)
		_ = idx.state.Save()
	}

	return nil
}

// reconcileDeletedMessages finds UIDs we have indexed that no longer exist on
// the server and removes them from the index. Only called when server message
// count is lower than our indexed UID count.
func (idx *Indexer) reconcileDeletedMessages(ctx context.Context, c *imapclient.Client, account, folder string, state *FolderSyncState) error {
	// Fetch current UID list from server - just the numbers, no message data
	criteria := &imap.SearchCriteria{}
	searchData, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return fmt.Errorf("searching all UIDs: %w", err)
	}

	// Build a set of server UIDs for fast lookup
	serverUIDs := make(map[uint32]bool, len(searchData.AllUIDs()))
	for _, uid := range searchData.AllUIDs() {
		serverUIDs[uint32(uid)] = true
	}

	// Find UIDs in our index that are no longer on the server
	var staleUIDs []uint32
	for _, uid := range state.IndexedUIDs {
		if !serverUIDs[uid] {
			staleUIDs = append(staleUIDs, uid)
		}
	}

	if len(staleUIDs) == 0 {
		return nil
	}

	slog.Info("Removing stale index entries", "account", account, "folder", folder, "count", len(staleUIDs))

	// Delete stale docs in batches
	batch := idx.index.NewBatch()
	for i, uid := range staleUIDs {
		docID := fmt.Sprintf("%s/%s/%d", account, folder, uid)
		batch.Delete(docID)
		state.RemoveUID(uid)

		if (i+1)%1000 == 0 {
			if err := idx.index.Batch(batch); err != nil {
				return fmt.Errorf("deleting stale batch: %w", err)
			}
			batch = idx.index.NewBatch()
		}
	}
	if err := idx.index.Batch(batch); err != nil {
		return fmt.Errorf("deleting stale batch: %w", err)
	}

	state.IndexedCount = len(state.IndexedUIDs)
	return nil
}

func (idx *Indexer) fetchAndIndex(ctx context.Context, c *imapclient.Client, account, folder string, uidSet imap.UIDSet) error {
	fetchOptions := &imap.FetchOptions{
		Envelope:   true,
		Flags:      true,
		UID:        true,
		RFC822Size: true,
		BodySection: []*imap.FetchItemBodySection{
			{Peek: true},
		},
	}

	msgs, err := c.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return fmt.Errorf("fetching messages: %w", err)
	}

	batch := idx.index.NewBatch()

	for _, msg := range msgs {
		doc := IndexDocument{
			Account: account,
			Folder:  folder,
			UID:     uint32(msg.UID),
			Size:    uint32(msg.RFC822Size),
		}

		if env := msg.Envelope; env != nil {
			doc.Subject = env.Subject
			doc.MessageID = env.MessageID
			doc.Date = env.Date.Format(time.RFC3339)
			if len(env.From) > 0 {
				doc.From = formatAddr(env.From[0])
			}
			var toAddrs []string
			for _, addr := range env.To {
				toAddrs = append(toAddrs, formatAddr(addr))
			}
			doc.To = strings.Join(toAddrs, ", ")
			var ccAddrs []string
			for _, addr := range env.Cc {
				ccAddrs = append(ccAddrs, formatAddr(addr))
			}
			doc.CC = strings.Join(ccAddrs, ", ")
		}

		var flagStrs []string
		for _, f := range msg.Flags {
			flagStrs = append(flagStrs, string(f))
		}
		doc.Flags = strings.Join(flagStrs, " ")

		for _, bs := range msg.BodySection {
			if bs.Bytes != nil {
				doc.Body = extractText(bs.Bytes)
			}
		}

		docID := fmt.Sprintf("%s/%s/%d", account, folder, doc.UID)
		batch.Index(docID, doc)
	}

	if err := idx.index.Batch(batch); err != nil {
		return fmt.Errorf("indexing batch: %w", err)
	}

	return nil
}

func (idx *Indexer) deleteFolder(account, folder string) {
	prefix := fmt.Sprintf("%s/%s/", account, folder)
	q := bleve.NewPrefixQuery(prefix)
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = 10000

	for {
		results, err := idx.index.Search(searchReq)
		if err != nil || results.Total == 0 {
			break
		}
		batch := idx.index.NewBatch()
		for _, hit := range results.Hits {
			batch.Delete(hit.ID)
		}
		idx.index.Batch(batch)

		if results.Total <= uint64(searchReq.Size) {
			break
		}
	}

	idx.state.DeleteFolder(account, folder)
}

// --- Search ---

func (idx *Indexer) Search(queryStr, account, folder, dateFrom, dateTo string, limit, offset int) (*SearchResult, error) {
	var queries []bleveQuery.Query

	mainQuery := bleve.NewQueryStringQuery(queryStr)
	queries = append(queries, mainQuery)

	if account != "" {
		q := bleve.NewTermQuery(account)
		q.SetField("account")
		queries = append(queries, q)
	}

	if folder != "" {
		q := bleve.NewTermQuery(folder)
		q.SetField("folder")
		queries = append(queries, q)
	}

	if dateFrom != "" || dateTo != "" {
		start := time.Time{}
		end := time.Time{}
		if dateFrom != "" {
			if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
				start = t
			}
		}
		if dateTo != "" {
			if t, err := time.Parse("2006-01-02", dateTo); err == nil {
				end = t.Add(24*time.Hour - time.Second)
			}
		}
		q := bleve.NewDateRangeQuery(start, end)
		q.SetField("date")
		queries = append(queries, q)
	}

	var finalQuery bleveQuery.Query
	if len(queries) == 1 {
		finalQuery = queries[0]
	} else {
		finalQuery = bleve.NewConjunctionQuery(queries...)
	}

	searchReq := bleve.NewSearchRequestOptions(finalQuery, limit, offset, false)
	searchReq.Fields = []string{"account", "folder", "uid", "message_id", "from", "to", "subject", "date"}
	searchReq.Highlight = bleve.NewHighlightWithStyle(markdownHighlighterName)
	searchReq.Highlight.AddField("subject")
	searchReq.Highlight.AddField("body")

	results, err := idx.index.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("searching index: %w", err)
	}

	var hits []SearchHit
	for _, hit := range results.Hits {
		h := SearchHit{
			Score: hit.Score,
		}
		if v, ok := hit.Fields["account"].(string); ok {
			h.Account = v
		}
		if v, ok := hit.Fields["folder"].(string); ok {
			h.Folder = v
		}
		if v, ok := hit.Fields["uid"].(float64); ok {
			h.UID = uint32(v)
		}
		if v, ok := hit.Fields["from"].(string); ok {
			h.From = v
		}
		if v, ok := hit.Fields["to"].(string); ok {
			h.To = v
		}
		if v, ok := hit.Fields["subject"].(string); ok {
			h.Subject = v
		}
		if v, ok := hit.Fields["date"].(string); ok {
			h.Date = v
		}

		if fragments, ok := hit.Fragments["body"]; ok && len(fragments) > 0 {
			h.Snippet = strings.Join(fragments, " ... ")
		} else if fragments, ok := hit.Fragments["subject"]; ok && len(fragments) > 0 {
			h.Snippet = strings.Join(fragments, " ... ")
		}

		hits = append(hits, h)
	}

	return &SearchResult{
		TotalMatches: int(results.Total),
		Offset:       offset,
		Messages:     hits,
	}, nil
}

// --- Status & Reindex ---

// StatusText returns a human-readable plain text summary.
func (idx *Indexer) StatusText(account string) string {
	idx.state.mu.RLock()
	defer idx.state.mu.RUnlock()

	var b strings.Builder
	b.WriteString("ACCOUNT:FOLDER                              STATUS\n")

	for acct, folders := range idx.state.Accounts {
		if account != "" && acct != account {
			continue
		}
		for folder, state := range folders {
			label := acct + ":" + folder
			var status string
			switch state.Status {
			case "synced":
				status = fmt.Sprintf("synced (%d msgs)", state.IndexedCount)
			case "syncing":
				status = fmt.Sprintf("syncing (%d msgs so far)", state.IndexedCount)
			case "error":
				status = "error"
			case "stale":
				status = fmt.Sprintf("stale (%d msgs, last sync %s)", state.IndexedCount, state.LastSyncTime.Format("2006-01-02 15:04"))
			default:
				status = state.Status
			}
			fmt.Fprintf(&b, "%-40s %s\n", label, status)
		}
	}

	indexPath := filepath.Join(idx.dataDir, "index.bleve")
	fmt.Fprintf(&b, "\nIndex size: %s\n", formatBytes(dirSize(indexPath)))
	return b.String()
}

// SyncNow triggers an immediate sync of all accounts without clearing state.
func (idx *Indexer) SyncNow(ctx context.Context, pool *imaplib.Pool) {
	idx.syncAll(ctx, pool)
}

func (idx *Indexer) Reindex(ctx context.Context, pool *imaplib.Pool) error {
	slog.Info("Starting full reindex")

	idx.state.mu.Lock()
	idx.state.Accounts = make(map[string]map[string]*FolderSyncState)
	idx.state.mu.Unlock()
	_ = idx.state.Save()

	idx.syncAll(ctx, pool)
	return nil
}

// --- Helpers ---

func extractText(body []byte) string {
	reader, err := mail.CreateReader(bytes.NewReader(body))
	if err != nil && !message.IsUnknownCharset(err) {
		return ""
	}
	defer reader.Close()

	var htmlFallback string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) {
			break
		}
		if h, ok := part.Header.(*mail.InlineHeader); ok {
			ct, _, _ := h.ContentType()
			if strings.HasPrefix(ct, "text/plain") {
				bodyBytes, err := io.ReadAll(part.Body)
				if err != nil {
					continue
				}
				return string(bodyBytes)
			}
			if strings.HasPrefix(ct, "text/html") && htmlFallback == "" {
				bodyBytes, err := io.ReadAll(part.Body)
				if err != nil {
					continue
				}
				htmlFallback = string(bodyBytes)
			}
		}
	}
	return htmlFallback
}

func formatAddr(addr imap.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s@%s>", addr.Name, addr.Mailbox, addr.Host)
	}
	return fmt.Sprintf("%s@%s", addr.Mailbox, addr.Host)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func dirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}
