package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/logging"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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

	return fs.saveLocked(sess)
}

func (fs *FileStore) Load(_ context.Context, id string) (*Session, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	return fs.loadLocked(id)
}

func (fs *FileStore) loadLocked(id string) (*Session, error) {
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
	if reasoningChanged(sess.Messages) {
		sess.Messages = sanitizePersistedMessages(sess.Messages)
		if err := fs.saveLocked(sess); err != nil {
			return nil, err
		}
	}
	return sess, nil
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

		source, parentID, taskID, preview, activityKind, archived, activityAt := ThreadMetadataValues(&sess)
		endedAt := formatSessionTime(sess.EndedAt)
		updatedAt := formatSessionTime(activityAt)
		if updatedAt == "" {
			updatedAt = formatSessionTime(sess.CreatedAt)
		}
		profile, effectiveTrust, effectiveApproval, taskMode := ProfileMetadataValues(&sess)
		summaries = append(summaries, SessionSummary{
			ID:                sess.ID,
			Title:             sess.Title,
			Goal:              sess.Config.Goal,
			Mode:              sess.Config.Mode,
			Profile:           profile,
			EffectiveTrust:    effectiveTrust,
			EffectiveApproval: effectiveApproval,
			TaskMode:          taskMode,
			Source:            source,
			ParentID:          parentID,
			TaskID:            taskID,
			Preview:           preview,
			ActivityKind:      activityKind,
			Status:            sess.Status,
			Recoverable:       IsRecoverableStatus(sess.Status),
			Archived:          archived,
			Steps:             sess.Budget.UsedStepsValue(),
			CreatedAt:         formatSessionTime(sess.CreatedAt),
			UpdatedAt:         updatedAt,
			EndedAt:           endedAt,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		left := firstNonEmpty(summaries[i].UpdatedAt, summaries[i].CreatedAt)
		right := firstNonEmpty(summaries[j].UpdatedAt, summaries[j].CreatedAt)
		if left == right {
			return summaries[i].ID < summaries[j].ID
		}
		return left > right
	})
	return summaries, nil
}

func formatSessionTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format("2006-01-02 15:04:05")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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

func (fs *FileStore) LoadByRouteKey(_ context.Context, key string) (*Session, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	sessionID, err := fs.loadRouteIDLocked(key)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read route key %q: %w", key, err)
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	sess, err := fs.loadLocked(sessionID)
	if err != nil {
		return nil, err
	}
	if sess != nil {
		return sess, nil
	}
	if err := fs.deleteRouteLocked(key); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return nil, nil
}

func (fs *FileStore) SaveRouteKey(_ context.Context, key, sessionID string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if err := os.MkdirAll(fs.routesDir(), 0o700); err != nil {
		return fmt.Errorf("create route dir: %w", err)
	}
	return os.WriteFile(fs.routePath(key), []byte(strings.TrimSpace(sessionID)), 0o600)
}

func (fs *FileStore) DeleteRouteKey(_ context.Context, key string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if err := fs.deleteRouteLocked(key); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete route key %q: %w", key, err)
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

func (fs *FileStore) routesDir() string {
	return filepath.Join(fs.dir, "routes")
}

func (fs *FileStore) routePath(key string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(key)))
	if encoded == "" {
		encoded = "_"
	}
	return filepath.Join(fs.routesDir(), encoded+".route")
}

func (fs *FileStore) loadRouteIDLocked(key string) (string, error) {
	data, err := os.ReadFile(fs.routePath(key))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (fs *FileStore) deleteRouteLocked(key string) error {
	return os.Remove(fs.routePath(key))
}

func (fs *FileStore) saveLocked(sess *Session) error {
	data, err := json.MarshalIndent(persistedSessionFromSession(sess), "", "  ")
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

type persistedSession struct {
	ID        string             `json:"id"`
	Status    SessionStatus      `json:"status"`
	Config    SessionConfig      `json:"config"`
	Messages  []persistedMessage `json:"messages"`
	State     map[string]any     `json:"state,omitempty"`
	Budget    persistedBudget    `json:"budget"`
	CreatedAt time.Time          `json:"created_at"`
	EndedAt   time.Time          `json:"ended_at,omitempty"`
}

type persistedBudget struct {
	MaxTokens  int `json:"max_tokens"`
	MaxSteps   int `json:"max_steps"`
	UsedTokens int `json:"used_tokens"`
	UsedSteps  int `json:"used_steps"`
}

type persistedMessage struct {
	Role         mdl.Role              `json:"role"`
	ContentParts []mdl.ContentPart     `json:"content_parts,omitempty"`
	Content      json.RawMessage       `json:"content,omitempty"`
	ToolCalls    []mdl.ToolCall        `json:"tool_calls,omitempty"`
	ToolResults  []persistedToolResult `json:"tool_results,omitempty"`
}

type persistedToolResult struct {
	CallID       string            `json:"call_id"`
	ContentParts []mdl.ContentPart `json:"content_parts,omitempty"`
	Content      json.RawMessage   `json:"content,omitempty"`
	IsError      bool              `json:"is_error,omitempty"`
}

func persistedSessionFromSession(sess *Session) persistedSession {
	if sess == nil {
		return persistedSession{}
	}
	snap := sess.Budget.Clone()
	out := persistedSession{
		ID:     sess.ID,
		Status: sess.Status,
		Config: sess.Config,
		State:  sess.State,
		Budget: persistedBudget{
			MaxTokens:  snap.MaxTokens,
			MaxSteps:   snap.MaxSteps,
			UsedTokens: snap.UsedTokens,
			UsedSteps:  snap.UsedSteps,
		},
		CreatedAt: sess.CreatedAt,
		EndedAt:   sess.EndedAt,
	}
	if len(sess.Messages) == 0 {
		return out
	}
	out.Messages = make([]persistedMessage, 0, len(sess.Messages))
	for _, msg := range sanitizePersistedMessages(sess.Messages) {
		pm := persistedMessage{
			Role:         msg.Role,
			ContentParts: msg.ContentParts,
			ToolCalls:    msg.ToolCalls,
		}
		if len(msg.ToolResults) > 0 {
			pm.ToolResults = make([]persistedToolResult, 0, len(msg.ToolResults))
			for _, tr := range msg.ToolResults {
				pm.ToolResults = append(pm.ToolResults, persistedToolResult{
					CallID:       tr.CallID,
					ContentParts: tr.ContentParts,
					IsError:      tr.IsError,
				})
			}
		}
		out.Messages = append(out.Messages, pm)
	}
	return out
}

func (ps *persistedSession) toSession(id string) (*Session, error) {
	sess := &Session{
		ID:     ps.ID,
		Status: ps.Status,
		Config: ps.Config,
		State:  ps.State,
		Budget: Budget{
			MaxTokens:  ps.Budget.MaxTokens,
			MaxSteps:   ps.Budget.MaxSteps,
			UsedTokens: ps.Budget.UsedTokens,
			UsedSteps:  ps.Budget.UsedSteps,
		},
		CreatedAt: ps.CreatedAt,
		EndedAt:   ps.EndedAt,
	}
	if len(ps.Messages) == 0 {
		return sess, nil
	}
	sess.Messages = make([]mdl.Message, 0, len(ps.Messages))
	for i, m := range ps.Messages {
		converted, migrated, err := migrateMessage(m)
		if err != nil {
			return nil, fmt.Errorf("unmarshal session %s: message %d: %w", id, i, err)
		}
		if migrated {
			logging.GetLogger().Warn("session store migrated legacy message content field",
				"session_id", ps.ID, "message_index", i)
		}
		sess.Messages = append(sess.Messages, converted)
	}
	return sess, nil
}

func migrateMessage(m persistedMessage) (mdl.Message, bool, error) {
	out := mdl.Message{
		Role:      m.Role,
		ToolCalls: m.ToolCalls,
	}
	var migrated bool
	var err error
	out.ContentParts, migrated, err = migrateContentParts(m.ContentParts, m.Content)
	if err != nil {
		return mdl.Message{}, false, err
	}
	if len(m.ToolResults) == 0 {
		return out, migrated, nil
	}
	out.ToolResults = make([]mdl.ToolResult, 0, len(m.ToolResults))
	for i, tr := range m.ToolResults {
		contentParts, trMigrated, err := migrateContentParts(tr.ContentParts, tr.Content)
		if err != nil {
			return mdl.Message{}, false, fmt.Errorf("tool_results[%d]: %w", i, err)
		}
		if trMigrated {
			migrated = true
		}
		out.ToolResults = append(out.ToolResults, mdl.ToolResult{
			CallID:       tr.CallID,
			ContentParts: contentParts,
			IsError:      tr.IsError,
		})
	}
	return out, migrated, nil
}

func sanitizePersistedMessages(messages []mdl.Message) []mdl.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]mdl.Message, 0, len(messages))
	for _, msg := range messages {
		msg.ContentParts = mdl.StripReasoningParts(msg.ContentParts)
		if len(msg.ToolResults) > 0 {
			results := make([]mdl.ToolResult, 0, len(msg.ToolResults))
			for _, tr := range msg.ToolResults {
				tr.ContentParts = mdl.StripReasoningParts(tr.ContentParts)
				results = append(results, tr)
			}
			msg.ToolResults = results
		}
		out = append(out, msg)
	}
	return out
}

func reasoningChanged(messages []mdl.Message) bool {
	for _, msg := range messages {
		if len(mdl.StripReasoningParts(msg.ContentParts)) != len(msg.ContentParts) {
			return true
		}
		for _, tr := range msg.ToolResults {
			if len(mdl.StripReasoningParts(tr.ContentParts)) != len(tr.ContentParts) {
				return true
			}
		}
	}
	return false
}

func migrateContentParts(parts []mdl.ContentPart, legacy json.RawMessage) ([]mdl.ContentPart, bool, error) {
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
	return []mdl.ContentPart{mdl.TextPart(text)}, true, nil
}
