package knowledge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// EpisodeKind 枚举事件类型。
type EpisodeKind string

const (
	EpisodeToolCall  EpisodeKind = "tool_call"
	EpisodeDecision  EpisodeKind = "decision"
	EpisodeError     EpisodeKind = "error"
	EpisodeUserMsg   EpisodeKind = "user_message"
	EpisodeCheckpint EpisodeKind = "checkpoint"
)

// Episode 表示 Agent 经历的一个事件记录。
type Episode struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"session_id"`
	Timestamp  time.Time      `json:"timestamp"`
	Kind       EpisodeKind    `json:"kind"`
	Summary    string         `json:"summary"`
	Importance float64        `json:"importance"`           // 0.0-1.0
	ExpiresAt  time.Time      `json:"expires_at,omitempty"` // 零值 = 永不过期
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// EpisodeFilter 过滤条件。
type EpisodeFilter struct {
	SessionID      string
	Kinds          []EpisodeKind
	MinImportance  float64
	Since          time.Time
	Until          time.Time
	ExcludeExpired bool // true = 过滤掉 ExpiresAt 已过期的 Episode
}

// EpisodicStore 提供历史事件的追加存储与检索能力。
type EpisodicStore interface {
	// Append 追加一条事件。
	Append(ctx context.Context, ep Episode) error
	// Recent 返回最近 limit 条事件。
	Recent(ctx context.Context, sessionID string, limit int) ([]Episode, error)
	// Search 按关键词和过滤条件检索事件。
	Search(ctx context.Context, query string, filter EpisodeFilter) ([]Episode, error)
}

// ---- 内存实现 (测试/轻量场景) -------------------------------------------

// MemoryEpisodicStore 基于内存的 EpisodicStore，进程重启后数据丢失。
type MemoryEpisodicStore struct {
	mu       sync.RWMutex
	episodes []Episode
}

// NewMemoryEpisodicStore 创建内存 EpisodicStore。
func NewMemoryEpisodicStore() *MemoryEpisodicStore {
	return &MemoryEpisodicStore{}
}

func (s *MemoryEpisodicStore) Append(_ context.Context, ep Episode) error {
	if ep.ID == "" {
		ep.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if ep.Timestamp.IsZero() {
		ep.Timestamp = time.Now()
	}
	s.mu.Lock()
	s.episodes = append(s.episodes, ep)
	s.mu.Unlock()
	return nil
}

func (s *MemoryEpisodicStore) Recent(_ context.Context, sessionID string, limit int) ([]Episode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []Episode
	for _, ep := range s.episodes {
		if sessionID != "" && ep.SessionID != sessionID {
			continue
		}
		filtered = append(filtered, ep)
	}
	// 按时间倒序，取最近 limit 条
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (s *MemoryEpisodicStore) Search(_ context.Context, query string, filter EpisodeFilter) ([]Episode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Episode
	for _, ep := range s.episodes {
		if !matchesFilter(ep, filter) {
			continue
		}
		if query != "" && !containsCI(ep.Summary, query) {
			continue
		}
		results = append(results, ep)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})
	return results, nil
}

// ---- JSONL 文件实现 (持久化) --------------------------------------------

// FileEpisodicStore 将 Episode 以 JSONL 格式追加写入文件（持久化实现）。
type FileEpisodicStore struct {
	mu   sync.Mutex
	path string
}

// NewFileEpisodicStore 创建基于 JSONL 文件的 EpisodicStore。
// path 是存储文件路径，若目录不存在则自动创建。
func NewFileEpisodicStore(path string) (*FileEpisodicStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("episodic store: mkdir: %w", err)
	}
	return &FileEpisodicStore{path: path}, nil
}

func (s *FileEpisodicStore) Append(_ context.Context, ep Episode) error {
	if ep.ID == "" {
		ep.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if ep.Timestamp.IsZero() {
		ep.Timestamp = time.Now()
	}

	data, err := json.Marshal(ep)
	if err != nil {
		return fmt.Errorf("episodic store: marshal: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("episodic store: open: %w", err)
	}
	defer f.Close()

	_, err = f.WriteString(string(data) + "\n")
	return err
}

func (s *FileEpisodicStore) Recent(ctx context.Context, sessionID string, limit int) ([]Episode, error) {
	all, err := s.loadAll()
	if err != nil {
		return nil, err
	}
	var filtered []Episode
	for _, ep := range all {
		if sessionID != "" && ep.SessionID != sessionID {
			continue
		}
		filtered = append(filtered, ep)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (s *FileEpisodicStore) Search(_ context.Context, query string, filter EpisodeFilter) ([]Episode, error) {
	all, err := s.loadAll()
	if err != nil {
		return nil, err
	}
	var results []Episode
	for _, ep := range all {
		if !matchesFilter(ep, filter) {
			continue
		}
		if query != "" && !containsCI(ep.Summary, query) {
			continue
		}
		results = append(results, ep)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})
	return results, nil
}

func (s *FileEpisodicStore) loadAll() ([]Episode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

// readLocked reads all episodes from disk. Caller must hold s.mu.
func (s *FileEpisodicStore) readLocked() ([]Episode, error) {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("episodic store: open for read: %w", err)
	}
	defer f.Close()

	var episodes []Episode
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ep Episode
		if err := json.Unmarshal(line, &ep); err != nil {
			continue // 跳过损坏行
		}
		episodes = append(episodes, ep)
	}
	return episodes, scanner.Err()
}

// ---- 辅助函数 -----------------------------------------------------------

func matchesFilter(ep Episode, f EpisodeFilter) bool {
	if f.SessionID != "" && ep.SessionID != f.SessionID {
		return false
	}
	if f.ExcludeExpired && !ep.ExpiresAt.IsZero() && time.Now().After(ep.ExpiresAt) {
		return false
	}
	if len(f.Kinds) > 0 {
		found := false
		for _, k := range f.Kinds {
			if ep.Kind == k {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if f.MinImportance > 0 && ep.Importance < f.MinImportance {
		return false
	}
	if !f.Since.IsZero() && ep.Timestamp.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && ep.Timestamp.After(f.Until) {
		return false
	}
	return true
}

func containsCI(s, sub string) bool {
	if sub == "" {
		return true
	}
	// 简单大小写不敏感搜索
	return len(s) >= len(sub) && containsInsensitive(s, sub)
}

func containsInsensitive(s, sub string) bool {
	sLen, subLen := len(s), len(sub)
	if subLen == 0 {
		return true
	}
	for i := 0; i <= sLen-subLen; i++ {
		match := true
		for j := 0; j < subLen; j++ {
			if toLower(s[i+j]) != toLower(sub[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}
