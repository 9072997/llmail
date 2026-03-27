package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jpennington/llmail/internal/config"
)

type FolderSyncState struct {
	LastUID      uint32    `json:"last_uid"`
	UIDValidity  uint32    `json:"uid_validity"`
	UIDNext      uint32    `json:"uid_next"`
	LastSyncTime time.Time `json:"last_sync_time"`
	IndexedCount int       `json:"indexed_count"`
	Status       string    `json:"status"` // "synced", "syncing", "error", "stale"
	IndexedUIDs  []uint32  `json:"indexed_uids,omitempty"`
}

// AddUID inserts a UID into the sorted IndexedUIDs set.
func (fs *FolderSyncState) AddUID(uid uint32) {
	i := sort.Search(len(fs.IndexedUIDs), func(i int) bool { return fs.IndexedUIDs[i] >= uid })
	if i < len(fs.IndexedUIDs) && fs.IndexedUIDs[i] == uid {
		return // already present
	}
	fs.IndexedUIDs = append(fs.IndexedUIDs, 0)
	copy(fs.IndexedUIDs[i+1:], fs.IndexedUIDs[i:])
	fs.IndexedUIDs[i] = uid
}

// RemoveUID removes a UID from the sorted IndexedUIDs set.
func (fs *FolderSyncState) RemoveUID(uid uint32) {
	i := sort.Search(len(fs.IndexedUIDs), func(i int) bool { return fs.IndexedUIDs[i] >= uid })
	if i < len(fs.IndexedUIDs) && fs.IndexedUIDs[i] == uid {
		fs.IndexedUIDs = append(fs.IndexedUIDs[:i], fs.IndexedUIDs[i+1:]...)
	}
}

// HasUID checks if a UID is in the sorted IndexedUIDs set.
func (fs *FolderSyncState) HasUID(uid uint32) bool {
	i := sort.Search(len(fs.IndexedUIDs), func(i int) bool { return fs.IndexedUIDs[i] >= uid })
	return i < len(fs.IndexedUIDs) && fs.IndexedUIDs[i] == uid
}

type SyncState struct {
	mu       sync.RWMutex
	path     string
	Accounts map[string]map[string]*FolderSyncState `json:"accounts"` // account -> folder -> state
}

func loadSyncState(dataDir string) (*SyncState, error) {
	path := filepath.Join(dataDir, "sync_state.json")
	state := &SyncState{
		path:     path,
		Accounts: make(map[string]map[string]*FolderSyncState),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return nil, fmt.Errorf("reading sync state: %w", err)
	}

	if err := json.Unmarshal(data, &state.Accounts); err != nil {
		return nil, fmt.Errorf("parsing sync state: %w", err)
	}

	return state, nil
}

func (s *SyncState) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating sync state directory: %w", err)
	}

	data, err := json.MarshalIndent(s.Accounts, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling sync state: %w", err)
	}

	return os.WriteFile(s.path, data, 0600)
}

func (s *SyncState) GetFolder(account, folder string) *FolderSyncState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if acct, ok := s.Accounts[account]; ok {
		if fs, ok := acct[folder]; ok {
			return fs
		}
	}
	return &FolderSyncState{Status: "new"}
}

func (s *SyncState) SetFolder(account, folder string, state *FolderSyncState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.Accounts[account]; !ok {
		s.Accounts[account] = make(map[string]*FolderSyncState)
	}
	s.Accounts[account][folder] = state
}

func (s *SyncState) DeleteFolder(account, folder string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if acct, ok := s.Accounts[account]; ok {
		delete(acct, folder)
	}
}

func (s *SyncState) RenameFolder(account, oldName, newName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if acct, ok := s.Accounts[account]; ok {
		if fs, ok := acct[oldName]; ok {
			acct[newName] = fs
			delete(acct, oldName)
		}
	}
}

func foldersToSync(cfg *config.Config, account string) []string {
	if cfg.Index.Mode == "all" {
		return nil // signal to sync all folders
	}
	if cfg.Index.Mode == "selected" {
		if acct, ok := cfg.Index.Accounts[account]; ok {
			return acct.Folders
		}
	}
	return nil
}
