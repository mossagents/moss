package session

import (
	"time"

	"github.com/mossagi/moss/kernel/port"
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
	Goal         string         `json:"goal"`
	Mode         string         `json:"mode,omitempty"`
	TrustLevel   string         `json:"trust_level,omitempty"`
	MaxSteps     int            `json:"max_steps,omitempty"`
	MaxTokens    int            `json:"max_tokens,omitempty"`
	SystemPrompt string         `json:"system_prompt,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Budget 追踪 Session 的资源预算使用情况。
type Budget struct {
	MaxTokens  int `json:"max_tokens"`
	MaxSteps   int `json:"max_steps"`
	UsedTokens int `json:"used_tokens"`
	UsedSteps  int `json:"used_steps"`
}

// Exhausted 返回预算是否已耗尽。
func (b *Budget) Exhausted() bool {
	return (b.MaxTokens > 0 && b.UsedTokens >= b.MaxTokens) ||
		(b.MaxSteps > 0 && b.UsedSteps >= b.MaxSteps)
}

// Record 记录一次资源消耗。
func (b *Budget) Record(tokens, steps int) {
	b.UsedTokens += tokens
	b.UsedSteps += steps
}

// Session 是 Agent 的执行上下文，包含对话历史、状态和预算。
type Session struct {
	ID        string         `json:"id"`
	Status    SessionStatus  `json:"status"`
	Config    SessionConfig  `json:"config"`
	Messages  []port.Message `json:"messages"`
	State     map[string]any `json:"state,omitempty"`
	Budget    Budget         `json:"budget"`
	CreatedAt time.Time      `json:"created_at"`
	EndedAt   time.Time      `json:"ended_at,omitempty"`
}

// AppendMessage 追加一条消息到对话历史。
func (s *Session) AppendMessage(msg port.Message) {
	s.Messages = append(s.Messages, msg)
}

// TruncateMessages 按 token 预算截断对话历史，保留最近的消息。
// counter 函数返回单条消息的 token 数。
func (s *Session) TruncateMessages(maxTokens int, counter func(port.Message) int) {
	if maxTokens <= 0 || len(s.Messages) == 0 {
		return
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

	if cutoff > 0 && cutoff < len(s.Messages) {
		s.Messages = s.Messages[cutoff:]
	}
}

// SetState 设置键值状态。
func (s *Session) SetState(key string, value any) {
	if s.State == nil {
		s.State = make(map[string]any)
	}
	s.State[key] = value
}

// GetState 获取键值状态。
func (s *Session) GetState(key string) (any, bool) {
	if s.State == nil {
		return nil, false
	}
	v, ok := s.State[key]
	return v, ok
}
