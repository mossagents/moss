package product

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type FileChangeStore struct {
	dir string
	mu  sync.Mutex
}

func NewFileChangeStore(dir string) (*FileChangeStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create change store dir: %w", err)
	}
	return &FileChangeStore{dir: dir}, nil
}

func OpenChangeStore() (*FileChangeStore, error) {
	return NewFileChangeStore(ChangeStoreDir())
}

func (fs *FileChangeStore) Save(_ context.Context, item *ChangeOperation) error {
	if item == nil {
		return fmt.Errorf("change operation is required")
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal change operation: %w", err)
	}
	path := fs.path(item.ID)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write change operation tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace change operation: %w", err)
	}
	return nil
}

func (fs *FileChangeStore) Load(_ context.Context, id string) (*ChangeOperation, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := os.ReadFile(fs.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read change operation %s: %w", id, err)
	}
	var item ChangeOperation
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("unmarshal change operation %s: %w", id, err)
	}
	return cloneChangeOperation(&item), nil
}

func (fs *FileChangeStore) Delete(_ context.Context, id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	path := fs.path(id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete change operation %s: %w", id, err)
	}
	return nil
}

func (fs *FileChangeStore) List(_ context.Context) ([]ChangeOperation, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return nil, fmt.Errorf("list change operations: %w", err)
	}
	items := make([]ChangeOperation, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fs.dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read change operation file %s: %w", entry.Name(), err)
		}
		var item ChangeOperation
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, fmt.Errorf("unmarshal change operation file %s: %w", entry.Name(), err)
		}
		items = append(items, *cloneChangeOperation(&item))
	}
	sortChangesNewestFirst(items)
	return items, nil
}

func (fs *FileChangeStore) ListBySession(ctx context.Context, sessionID string, limit int) ([]ChangeOperation, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	items, err := fs.List(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]ChangeOperation, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.SessionID) != sessionID {
			continue
		}
		filtered = append(filtered, item)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (fs *FileChangeStore) CountBySession(ctx context.Context, sessionID string) (int, error) {
	items, err := fs.ListBySession(ctx, sessionID, 0)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func (fs *FileChangeStore) CountsBySession(ctx context.Context) (map[string]int, error) {
	items, err := fs.List(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	for _, item := range items {
		sessionID := strings.TrimSpace(item.SessionID)
		if sessionID == "" {
			continue
		}
		counts[sessionID]++
	}
	return counts, nil
}

func (fs *FileChangeStore) ListByRepoRoot(ctx context.Context, repoRoot string) ([]ChangeOperation, error) {
	items, err := fs.List(ctx)
	if err != nil {
		return nil, err
	}
	return filterChangesByRepoRoot(items, repoRoot), nil
}

func (fs *FileChangeStore) path(id string) string {
	return filepath.Join(fs.dir, sanitizeChangeID(id)+".json")
}

func sanitizeChangeID(id string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, filepath.Base(id))
	if safe == "" || safe == "." || safe == ".." {
		safe = "_invalid_"
	}
	return safe
}

func newChangeID(repoRoot string) string {
	base := sanitizeChangeID(filepath.Base(canonicalRepoRoot(repoRoot)))
	if base == "_invalid_" || base == "" {
		base = "repo"
	}
	return fmt.Sprintf("change-%s-%d", base, time.Now().UnixNano())
}

func changeSortTime(item ChangeOperation) time.Time {
	if !item.RolledBackAt.IsZero() {
		return item.RolledBackAt.UTC()
	}
	return item.CreatedAt.UTC()
}

func sortChangesNewestFirst(items []ChangeOperation) {
	sort.Slice(items, func(i, j int) bool {
		left := changeSortTime(items[i])
		right := changeSortTime(items[j])
		if left.Equal(right) {
			return items[i].ID > items[j].ID
		}
		return left.After(right)
	})
}
