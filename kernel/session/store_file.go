package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileStore 是基于文件系统的 SessionStore 实现。
// 每个 Session 保存为独立的 JSON 文件：{dir}/{session_id}.json
type FileStore struct {
	dir string
	mu  sync.Mutex
}

// NewFileStore 创建文件存储，dir 为存储目录（不存在则自动创建）。
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create session store dir: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

func (fs *FileStore) Save(_ context.Context, sess *Session) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	path := fs.path(sess.ID)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write session tmp: %w", err)
	}
	return os.Rename(tmpPath, path)
}

func (fs *FileStore) Load(_ context.Context, id string) (*Session, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	path := fs.path(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session %s: %w", id, err)
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("unmarshal session %s: %w", id, err)
	}
	return &sess, nil
}

func (fs *FileStore) List(_ context.Context) ([]SessionSummary, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	var summaries []SessionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(fs.dir, e.Name()))
		if err != nil {
			continue
		}

		var sess Session
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}
		if !VisibleInHistory(&sess) {
			continue
		}

		endedAt := ""
		if !sess.EndedAt.IsZero() {
			endedAt = sess.EndedAt.Format("2006-01-02 15:04:05")
		}

		summaries = append(summaries, SessionSummary{
			ID:          sess.ID,
			Goal:        sess.Config.Goal,
			Mode:        sess.Config.Mode,
			Status:      sess.Status,
			Recoverable: IsRecoverableStatus(sess.Status),
			Steps:       sess.Budget.UsedSteps,
			CreatedAt:   sess.CreatedAt.Format("2006-01-02 15:04:05"),
			EndedAt:     endedAt,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CreatedAt > summaries[j].CreatedAt
	})
	return summaries, nil
}

func (fs *FileStore) Delete(_ context.Context, id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	path := fs.path(id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session %s: %w", id, err)
	}
	return nil
}

// Watch 不支持文件存储实现，返回 ErrNotSupported。
// 后续 Redis/etcd Store 可提供真正的 Watch 语义。
func (fs *FileStore) Watch(_ context.Context, _ string) (<-chan *Session, error) {
	return nil, ErrNotSupported
}

// sanitizeID 清理 Session ID 防止路径遍历。
// 使用白名单策略：只保留字母、数字、连字符、下划线。
func sanitizeID(id string) string {
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

func (fs *FileStore) path(id string) string {
	return filepath.Join(fs.dir, sanitizeID(id)+".json")
}
