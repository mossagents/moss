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
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
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
