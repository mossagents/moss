package session

import (
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/model"
)

// SessionStatus 表示 Session 的生命周期状态。
type SessionStatus string

const (
	StatusCreated   SessionStatus = "created"
	StatusRunning   SessionStatus = "running"
	StatusPaused    SessionStatus = "paused"
	StatusCompleted SessionStatus = "completed"
	StatusFailed    SessionStatus = "failed"
	StatusCancelled SessionStatus = "cancelled"
)

// SessionConfig 配置 Session 的运行参数。
type SessionConfig struct {
	Goal         string            `json:"goal"`
	Mode         string            `json:"mode,omitempty"`
	Profile      string            `json:"profile,omitempty"`
	TrustLevel   string            `json:"trust_level,omitempty"`
	MaxSteps     int               `json:"max_steps,omitempty"`
	MaxTokens    int               `json:"max_tokens,omitempty"`
	BudgetPolicy string            `json:"budget_policy,omitempty"`
	Timeout      time.Duration     `json:"timeout,omitempty"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	ModelConfig  model.ModelConfig `json:"model_config,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
}

// Budget 追踪 Session 的资源预算使用情况。
type Budget struct {
	MaxTokens  int `json:"max_tokens"`
	MaxSteps   int `json:"max_steps"`
	UsedTokens int `json:"used_tokens"`
	UsedSteps  int `json:"used_steps"`
	mu         sync.Mutex
}

// Exhausted 返回预算是否已耗尽。
func (b *Budget) Exhausted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return (b.MaxTokens > 0 && b.UsedTokens >= b.MaxTokens) ||
		(b.MaxSteps > 0 && b.UsedSteps >= b.MaxSteps)
}

// Record 记录一次资源消耗。
func (b *Budget) Record(tokens, steps int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.UsedTokens += tokens
	b.UsedSteps += steps
}

// TryConsume 原子地检查并记录预算消耗。
// 若记录后会超过预算上限，则返回 false 且不修改现有计数。
func (b *Budget) TryConsume(tokens, steps int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	nextTokens := b.UsedTokens + tokens
	nextSteps := b.UsedSteps + steps
	if (b.MaxTokens > 0 && nextTokens > b.MaxTokens) ||
		(b.MaxSteps > 0 && nextSteps > b.MaxSteps) {
		return false
	}
	b.UsedTokens = nextTokens
	b.UsedSteps = nextSteps
	return true
}

func (b *Budget) UsedStepsValue() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.UsedSteps
}

func (b *Budget) UsedTokensValue() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.UsedTokens
}

func (b *Budget) ResetUsage() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.UsedSteps = 0
	b.UsedTokens = 0
}

// Clone returns a copy of budget counters with a fresh mutex.
func (b *Budget) Clone() Budget {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Budget{
		MaxTokens:  b.MaxTokens,
		MaxSteps:   b.MaxSteps,
		UsedTokens: b.UsedTokens,
		UsedSteps:  b.UsedSteps,
	}
}

// Session 是 Agent 的执行上下文，包含对话历史、状态和预算。
type Session struct {
	ID        string          `json:"id"`
	Status    SessionStatus   `json:"status"`
	Config    SessionConfig   `json:"config"`
	Title     string          `json:"title,omitempty"` // user-facing display title
	Messages  []model.Message `json:"messages"`
	State     ScopedState     `json:"state,omitempty"`
	Budget    Budget          `json:"budget"`
	CreatedAt time.Time       `json:"created_at"`
	EndedAt   time.Time       `json:"ended_at,omitempty"`
	mu        sync.RWMutex    `json:"-"` // protects Messages, Title, Config.Metadata, State, and materializationDomain

	materializationDomain string `json:"-"`
}

// AppendMessage 追加一条消息到对话历史。
func (s *Session) AppendMessage(msg model.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = append(s.Messages, msg)
}

// ReplaceMessages 原子地替换完整的消息历史。
func (s *Session) ReplaceMessages(msgs []model.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = msgs
}

// CopyMessages 在读锁保护下返回消息历史的浅拷贝。
// 供需要并发安全读取的调用方使用（如 PromptMessages）。
func (s *Session) CopyMessages() []model.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Messages) == 0 {
		return nil
	}
	out := make([]model.Message, len(s.Messages))
	copy(out, s.Messages)
	return out
}

// UpdateSystemPrompt 原子地更新或插入系统提示消息。
// 若消息历史的第一条是 system 消息则原地更新，否则前插。
func (s *Session) UpdateSystemPrompt(prompt string) {
	newMsg := model.Message{
		Role:         model.RoleSystem,
		ContentParts: []model.ContentPart{model.TextPart(prompt)},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Messages) > 0 && s.Messages[0].Role == model.RoleSystem {
		s.Messages[0].ContentParts = newMsg.ContentParts
	} else {
		s.Messages = append([]model.Message{newMsg}, s.Messages...)
	}
}

// SetTitle 线程安全地设置会话显示标题。
func (s *Session) SetTitle(title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Title = title
}

// GetTitle 线程安全地读取会话显示标题。
func (s *Session) GetTitle() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Title
}

// TruncateMessages 按 token 预算截断对话历史，保留最近的消息。
// counter 函数返回单条消息的 token 数。
// 系统提示消息（role=system）不会被截断。
func (s *Session) TruncateMessages(maxTokens int, counter func(model.Message) int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if maxTokens <= 0 || len(s.Messages) == 0 {
		return
	}

	// 定位 system 消息的边界，确保截断不会移除系统提示。
	systemBoundary := 0
	for i, msg := range s.Messages {
		if msg.Role == model.RoleSystem {
			systemBoundary = i + 1
		}
	}

	total := 0
	cutoff := len(s.Messages)
	for i := len(s.Messages) - 1; i >= 0; i-- {
		cost := counter(s.Messages[i])
		if total+cost > maxTokens {
			cutoff = i + 1
			break
		}
		total += cost
	}

	// 截断点不得越过系统消息边界。
	if cutoff < systemBoundary {
		cutoff = systemBoundary
	}

	if cutoff > 0 && cutoff < len(s.Messages) {
		s.Messages = s.Messages[cutoff:]
	}
}

// SetState sets a state key-value pair with automatic scope routing.
// Keys prefixed with "app:", "user:", or "temp:" are routed to the
// corresponding scope; all other keys go to the Session scope.
func (s *Session) SetState(key string, value any) {
	scope, realKey := ParseScopeKey(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State.Set(scope, realKey, value)
}

// GetState retrieves a state value with automatic scope routing.
func (s *Session) GetState(key string) (any, bool) {
	scope, realKey := ParseScopeKey(key)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State.Get(scope, realKey)
}

// DeleteState removes a state key with automatic scope routing.
func (s *Session) DeleteState(key string) {
	scope, realKey := ParseScopeKey(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State.Delete(scope, realKey)
}

// SetScopedState explicitly sets a value in the given scope.
func (s *Session) SetScopedState(scope StateScope, key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State.Set(scope, key, value)
}

// GetScopedState explicitly reads a value from the given scope.
func (s *Session) GetScopedState(scope StateScope, key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State.Get(scope, key)
}

// DeleteScopedState explicitly deletes a key from the given scope.
func (s *Session) DeleteScopedState(scope StateScope, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State.Delete(scope, key)
}

// CopyState returns a shallow copy of the Session scope map (backward-compatible).
func (s *Session) CopyState() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State.CopySessionScope()
}

// CopyAllState returns a deep copy of all four scopes.
func (s *Session) CopyAllState() ScopedState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State.Clone()
}

// ClearTempState resets the Temp scope.
func (s *Session) ClearTempState() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State.ClearTemp()
}

// SetMetadata 线程安全地设置 Config.Metadata 键值。
func (s *Session) SetMetadata(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Config.Metadata == nil {
		s.Config.Metadata = make(map[string]any)
	}
	s.Config.Metadata[key] = value
}

// GetMetadata 线程安全地读取 Config.Metadata 键值。
func (s *Session) GetMetadata(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Config.Metadata == nil {
		return nil, false
	}
	v, ok := s.Config.Metadata[key]
	return v, ok
}

// DeleteMetadata 线程安全地删除 Config.Metadata 键值。
func (s *Session) DeleteMetadata(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Config.Metadata, key)
}

// EnsureMetadata 线程安全地确保 Config.Metadata 已初始化并返回副本。
func (s *Session) EnsureMetadata() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Config.Metadata == nil {
		s.Config.Metadata = make(map[string]any)
	}
	out := make(map[string]any, len(s.Config.Metadata))
	for k, v := range s.Config.Metadata {
		out[k] = v
	}
	return out
}

// CopyMetadata 在读锁保护下返回 Config.Metadata 的浅拷贝。
func (s *Session) CopyMetadata() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Config.Metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(s.Config.Metadata))
	for k, v := range s.Config.Metadata {
		out[k] = v
	}
	return out
}

// SetMetadataBatch 线程安全地批量设置多个 Config.Metadata 键值。
func (s *Session) SetMetadataBatch(pairs map[string]any) {
	if len(pairs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Config.Metadata == nil {
		s.Config.Metadata = make(map[string]any)
	}
	for k, v := range pairs {
		s.Config.Metadata[k] = v
	}
}
