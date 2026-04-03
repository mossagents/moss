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
	"time"

	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/logging"
)

// FileStore 是基于文件系统的 SessionStore 实现。
// 每个 Session 保存为独立的 JSON 文件：{dir}/{session_id}.json
type FileStore struct {
	dir string
	mu  sync.RWMutex
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
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	path := fs.path(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session %s: %w", id, err)
	}

	var raw persistedSession
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal session %s: %w", id, err)
	}
	sess, err := raw.toSession(id)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (fs *FileStore) List(_ context.Context) ([]SessionSummary, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

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

		profile, effectiveTrust, effectiveApproval, taskMode := ProfileMetadataValues(&sess)
		summaries = append(summaries, SessionSummary{
			ID:                sess.ID,
			Goal:              sess.Config.Goal,
			Mode:              sess.Config.Mode,
			Profile:           profile,
			EffectiveTrust:    effectiveTrust,
			EffectiveApproval: effectiveApproval,
			TaskMode:          taskMode,
			Status:            sess.Status,
			Recoverable:       IsRecoverableStatus(sess.Status),
			Steps:             sess.Budget.UsedStepsValue(),
			CreatedAt:         sess.CreatedAt.Format("2006-01-02 15:04:05"),
			EndedAt:           endedAt,
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

// sanitizeID清理 Session ID 防止路径遍历。
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

type persistedSession struct {
	ID        string             `json:"id"`
	Status    SessionStatus      `json:"status"`
	Config    SessionConfig      `json:"config"`
	Messages  []persistedMessage `json:"messages"`
	State     map[string]any     `json:"state,omitempty"`
	Budget    Budget             `json:"budget"`
	CreatedAt time.Time          `json:"created_at"`
	EndedAt   time.Time          `json:"ended_at,omitempty"`
}

type persistedMessage struct {
	Role         port.Role             `json:"role"`
	ContentParts []port.ContentPart    `json:"content_parts,omitempty"`
	Content      json.RawMessage       `json:"content,omitempty"`
	ToolCalls    []port.ToolCall       `json:"tool_calls,omitempty"`
	ToolResults  []persistedToolResult `json:"tool_results,omitempty"`
}

type persistedToolResult struct {
	CallID       string             `json:"call_id"`
	ContentParts []port.ContentPart `json:"content_parts,omitempty"`
	Content      json.RawMessage    `json:"content,omitempty"`
	IsError      bool               `json:"is_error,omitempty"`
}

func (ps persistedSession) toSession(id string) (Session, error) {
	sess := Session{
		ID:        ps.ID,
		Status:    ps.Status,
		Config:    ps.Config,
		State:     ps.State,
		Budget:    ps.Budget,
		CreatedAt: ps.CreatedAt,
		EndedAt:   ps.EndedAt,
	}
	if len(ps.Messages) == 0 {
		return sess, nil
	}
	sess.Messages = make([]port.Message, 0, len(ps.Messages))
	for i, m := range ps.Messages {
		converted, migrated, err := migrateMessage(m)
		if err != nil {
			return Session{}, fmt.Errorf("unmarshal session %s: message %d: %w", id, i, err)
		}
		if migrated {
			logging.GetLogger().Warn("session store migrated legacy message content field",
				"session_id", ps.ID, "message_index", i)
		}
		sess.Messages = append(sess.Messages, converted)
	}
	return sess, nil
}

func migrateMessage(m persistedMessage) (port.Message, bool, error) {
	out := port.Message{
		Role:      m.Role,
		ToolCalls: m.ToolCalls,
	}
	var migrated bool
	var err error
	out.ContentParts, migrated, err = migrateContentParts(m.ContentParts, m.Content)
	if err != nil {
		return port.Message{}, false, err
	}
	if len(m.ToolResults) == 0 {
		return out, migrated, nil
	}
	out.ToolResults = make([]port.ToolResult, 0, len(m.ToolResults))
	for i, tr := range m.ToolResults {
		contentParts, trMigrated, err := migrateContentParts(tr.ContentParts, tr.Content)
		if err != nil {
			return port.Message{}, false, fmt.Errorf("tool_results[%d]: %w", i, err)
		}
		if trMigrated {
			migrated = true
		}
		out.ToolResults = append(out.ToolResults, port.ToolResult{
			CallID:       tr.CallID,
			ContentParts: contentParts,
			IsError:      tr.IsError,
		})
	}
	return out, migrated, nil
}

func migrateContentParts(parts []port.ContentPart, legacy json.RawMessage) ([]port.ContentPart, bool, error) {
	if len(parts) > 0 {
		if len(legacy) > 0 {
			return parts, true, nil
		}
		return parts, false, nil
	}
	if len(legacy) == 0 {
		return nil, false, nil
	}
	var text string
	if err := json.Unmarshal(legacy, &text); err != nil {
		return nil, false, fmt.Errorf("legacy content must be string: %w", err)
	}
	return []port.ContentPart{port.TextPart(text)}, true, nil
}
