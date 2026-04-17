package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/x/stringutil"
)

// FileStore 是基于文件系统的 SessionStore 实现。
// 每个 Session 保存为独立的 append-only JSONL 文件：{dir}/{session_id}.jsonl。
// 读取时兼容旧版单 JSON 对象文件，便于平滑升级。
type FileStore struct {
	dir          string
	mu           sync.RWMutex
	summaryCache map[string]SessionSummary // lazy-loaded cache
	cacheLoaded  bool
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
	raw, err := loadPersistedSessionFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 兼容旧版 .json 单对象路径。
			legacyRaw, legacyErr := loadPersistedSessionFile(fs.legacyPath(id))
			if legacyErr != nil {
				if os.IsNotExist(legacyErr) {
					return nil, nil
				}
				return nil, fmt.Errorf("read session %s: %w", id, legacyErr)
			}
			raw = legacyRaw
		} else {
			return nil, fmt.Errorf("read session %s: %w", id, err)
		}
	}

	sess, err := raw.toSession(id)
	if err != nil {
		return nil, err
	}
	if reasoningChanged(sess.Messages) {
		sess.Messages = sanitizePersistedMessages(sess.Messages)
		if err := fs.rewriteLocked(sess); err != nil {
			return nil, err
		}
	}
	return sess, nil
}

func (fs *FileStore) List(_ context.Context) ([]SessionSummary, error) {
	fs.mu.Lock()
	if !fs.cacheLoaded {
		if err := fs.loadAllSummariesLocked(); err != nil {
			fs.mu.Unlock()
			return nil, err
		}
	}
	summaries := make([]SessionSummary, 0, len(fs.summaryCache))
	for _, s := range fs.summaryCache {
		summaries = append(summaries, s)
	}
	fs.mu.Unlock()

	sort.Slice(summaries, func(i, j int) bool {
		left := stringutil.FirstNonEmpty(summaries[i].UpdatedAt, summaries[i].CreatedAt)
		right := stringutil.FirstNonEmpty(summaries[j].UpdatedAt, summaries[j].CreatedAt)
		if left == right {
			return summaries[i].ID < summaries[j].ID
		}
		return left > right
	})
	return summaries, nil
}

// loadAllSummariesLocked scans the directory and populates the summary cache.
// Must be called with fs.mu held for writing.
func (fs *FileStore) loadAllSummariesLocked() error {
	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	fs.summaryCache = make(map[string]SessionSummary, len(entries))
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".jsonl")) {
			continue
		}
		raw, err := loadPersistedSessionFile(filepath.Join(fs.dir, e.Name()))
		if err != nil {
			continue
		}
		sess, err := raw.toSession(raw.ID)
		if err != nil || sess == nil {
			continue
		}
		if !VisibleInHistory(sess) {
			continue
		}
		summary := buildSessionSummary(sess)
		fs.summaryCache[sess.ID] = summary
	}
	fs.cacheLoaded = true
	return nil
}

// buildSessionSummary extracts a SessionSummary from a Session.
func buildSessionSummary(sess *Session) SessionSummary {
	source, parentID, taskID, preview, activityKind, archived, activityAt := ThreadMetadataValues(sess)
	endedAt := formatSessionTime(sess.EndedAt)
	updatedAt := formatSessionTime(activityAt)
	if updatedAt == "" {
		updatedAt = formatSessionTime(sess.CreatedAt)
	}
	profile, effectiveTrust, effectiveApproval, taskMode := ProfileMetadataValues(sess)
	return SessionSummary{
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
	}
}

func formatSessionTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format("2006-01-02 15:04:05")
}

func (fs *FileStore) Delete(_ context.Context, id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	path := fs.path(id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session %s: %w", id, err)
	}
	if fs.cacheLoaded {
		delete(fs.summaryCache, id)
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
	return filepath.Join(fs.dir, sanitizeID(id)+".jsonl")
}

func (fs *FileStore) legacyPath(id string) string {
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
	if err := appendPersistedSessionJSONL(fs.path(sess.ID), persistedSessionFromSession(sess)); err != nil {
		return err
	}
	// Update summary cache if initialized.
	if fs.cacheLoaded {
		if VisibleInHistory(sess) {
			fs.summaryCache[sess.ID] = buildSessionSummary(sess)
		} else {
			delete(fs.summaryCache, sess.ID)
		}
	}
	return nil
}

func (fs *FileStore) rewriteLocked(sess *Session) error {
	if err := rewritePersistedSessionJSONL(fs.path(sess.ID), persistedSessionFromSession(sess)); err != nil {
		return err
	}
	if fs.cacheLoaded {
		if VisibleInHistory(sess) {
			fs.summaryCache[sess.ID] = buildSessionSummary(sess)
		} else {
			delete(fs.summaryCache, sess.ID)
		}
	}
	return nil
}

type persistedSession struct {
	ID        string             `json:"id"`
	Status    SessionStatus      `json:"status"`
	Config    SessionConfig      `json:"config"`
	Messages  []persistedMessage `json:"messages"`
	State     json.RawMessage    `json:"state,omitempty"`
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
	Role         model.Role            `json:"role"`
	ContentParts []model.ContentPart   `json:"content_parts,omitempty"`
	Content      json.RawMessage       `json:"content,omitempty"`
	ToolCalls    []model.ToolCall      `json:"tool_calls,omitempty"`
	ToolResults  []persistedToolResult `json:"tool_results,omitempty"`
}

type persistedToolResult struct {
	CallID       string              `json:"call_id"`
	ContentParts []model.ContentPart `json:"content_parts,omitempty"`
	Content      json.RawMessage     `json:"content,omitempty"`
	IsError      bool                `json:"is_error,omitempty"`
}

func persistedSessionFromSession(sess *Session) persistedSession {
	if sess == nil {
		return persistedSession{}
	}
	snap := sess.Budget.Clone()
	// Take thread-safe snapshots of mutable fields.
	messages := sess.CopyMessages()
	allState := sess.CopyAllState()
	metadata := sess.CopyMetadata()

	stateJSON, err := json.Marshal(allState)
	if err != nil {
		stateJSON = nil
	}

	cfg := sess.Config
	cfg.Metadata = metadata

	out := persistedSession{
		ID:     sess.ID,
		Status: sess.Status,
		Config: cfg,
		State:  stateJSON,
		Budget: persistedBudget{
			MaxTokens:  snap.MaxTokens,
			MaxSteps:   snap.MaxSteps,
			UsedTokens: snap.UsedTokens,
			UsedSteps:  snap.UsedSteps,
		},
		CreatedAt: sess.CreatedAt,
		EndedAt:   sess.EndedAt,
	}
	if len(messages) == 0 {
		return out
	}
	out.Messages = make([]persistedMessage, 0, len(messages))
	for _, msg := range sanitizePersistedMessages(messages) {
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
	var scopedState ScopedState
	if len(ps.State) > 0 {
		if err := json.Unmarshal(ps.State, &scopedState); err != nil {
			return nil, fmt.Errorf("unmarshal session %s state: %w", id, err)
		}
	}
	sess := &Session{
		ID:     ps.ID,
		Status: ps.Status,
		Config: ps.Config,
		State:  scopedState,
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
	sess.Messages = make([]model.Message, 0, len(ps.Messages))
	for i, m := range ps.Messages {
		converted, err := decodeMessage(m)
		if err != nil {
			return nil, fmt.Errorf("unmarshal session %s: message %d: %w", id, i, err)
		}
		sess.Messages = append(sess.Messages, converted)
	}
	return sess, nil
}

func decodeMessage(m persistedMessage) (model.Message, error) {
	out := model.Message{
		Role:      m.Role,
		ToolCalls: m.ToolCalls,
	}
	var err error
	out.ContentParts, err = decodeContentParts(m.ContentParts, m.Content)
	if err != nil {
		return model.Message{}, err
	}
	if len(m.ToolResults) == 0 {
		return out, nil
	}
	out.ToolResults = make([]model.ToolResult, 0, len(m.ToolResults))
	for i, tr := range m.ToolResults {
		contentParts, err := decodeContentParts(tr.ContentParts, tr.Content)
		if err != nil {
			return model.Message{}, fmt.Errorf("tool_results[%d]: %w", i, err)
		}
		out.ToolResults = append(out.ToolResults, model.ToolResult{
			CallID:       tr.CallID,
			ContentParts: contentParts,
			IsError:      tr.IsError,
		})
	}
	return out, nil
}

func sanitizePersistedMessages(messages []model.Message) []model.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]model.Message, 0, len(messages))
	for _, msg := range messages {
		msg.ContentParts = model.StripReasoningParts(msg.ContentParts)
		if len(msg.ToolCalls) > 0 {
			msg.ToolCalls = model.NormalizeToolCalls(msg.ToolCalls)
		}
		if len(msg.ToolResults) > 0 {
			results := make([]model.ToolResult, 0, len(msg.ToolResults))
			for _, tr := range msg.ToolResults {
				tr.ContentParts = model.StripReasoningParts(tr.ContentParts)
				results = append(results, tr)
			}
			msg.ToolResults = results
		}
		out = append(out, msg)
	}
	return out
}

func reasoningChanged(messages []model.Message) bool {
	for _, msg := range messages {
		if len(model.StripReasoningParts(msg.ContentParts)) != len(msg.ContentParts) {
			return true
		}
		for _, tr := range msg.ToolResults {
			if len(model.StripReasoningParts(tr.ContentParts)) != len(tr.ContentParts) {
				return true
			}
		}
	}
	return false
}

func decodeContentParts(parts []model.ContentPart, legacy json.RawMessage) ([]model.ContentPart, error) {
	if len(parts) > 0 {
		if len(legacy) > 0 {
			return nil, fmt.Errorf("legacy content field is no longer supported")
		}
		return parts, nil
	}
	if len(legacy) == 0 {
		return nil, nil
	}
	return nil, fmt.Errorf("legacy content field is no longer supported")
}
