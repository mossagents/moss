package task

import (
	"encoding/json"
	"fmt"
	"time"
)

// A2AMessageKind 表示 Agent-to-Agent 消息的语义类型。
type A2AMessageKind string

const (
	// A2AKindTaskDelegate 委托子任务给另一个 Agent。
	A2AKindTaskDelegate A2AMessageKind = "task_delegate"
	// A2AKindTaskResult 子任务执行结果回报。
	A2AKindTaskResult A2AMessageKind = "task_result"
	// A2AKindStatusUpdate 进度或状态更新。
	A2AKindStatusUpdate A2AMessageKind = "status_update"
	// A2AKindCancel 取消正在进行的任务。
	A2AKindCancel A2AMessageKind = "cancel"
	// A2AKindError 错误通知。
	A2AKindError A2AMessageKind = "error"
)

// A2AEnvelope 是嵌入到 MailMessage.Metadata["a2a"] 的结构化信封。
// 为向后兼容，信封以 JSON 字符串形式存储在 Metadata 中，
// 原有 MailMessage.Content 字段保留可读摘要文本。
type A2AEnvelope struct {
	Kind     A2AMessageKind  `json:"kind"`
	Protocol string          `json:"protocol"`  // 固定为 "a2a/v1"
	CorrelID string          `json:"correl_id"` // 关联 ID，用于 request/reply 配对
	TraceID  string          `json:"trace_id"`  // 分布式追踪 ID
	SentAt   time.Time       `json:"sent_at"`
	Payload  json.RawMessage `json:"payload"`
}

// A2AError 是 A2AKindError 消息的负载。
type A2AError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TaskDelegatePayload 是委派任务的负载（A2AKindTaskDelegate）。
type TaskDelegatePayload struct {
	TaskID      string         `json:"task_id"`
	Goal        string         `json:"goal"`
	Context     string         `json:"context,omitempty"`
	Constraints map[string]any `json:"constraints,omitempty"`
	TimeoutSec  int            `json:"timeout_sec,omitempty"`
}

// TaskResultPayload 是任务结果的负载（A2AKindTaskResult）。
type TaskResultPayload struct {
	TaskID  string `json:"task_id"`
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// StatusUpdatePayload 是状态更新的负载（A2AKindStatusUpdate）。
type StatusUpdatePayload struct {
	TaskID   string  `json:"task_id"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress,omitempty"` // 0-1
	Note     string  `json:"note,omitempty"`
}

// NewA2AMessage 构造携带 A2AEnvelope 的 MailMessage。
// content 是人可读的摘要，实际结构化数据在 envelope 中。
func NewA2AMessage(base MailMessage, envelope A2AEnvelope) (MailMessage, error) {
	if envelope.Protocol == "" {
		envelope.Protocol = "a2a/v1"
	}
	if envelope.SentAt.IsZero() {
		envelope.SentAt = time.Now()
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return MailMessage{}, fmt.Errorf("a2a: marshal envelope: %w", err)
	}
	if base.Metadata == nil {
		base.Metadata = make(map[string]any)
	}
	base.Metadata["a2a"] = string(raw)
	return base, nil
}

// ExtractA2AEnvelope 从 MailMessage.Metadata["a2a"] 提取 A2AEnvelope。
// 如果消息不含 a2a 信封，返回 nil, nil。
func ExtractA2AEnvelope(msg MailMessage) (*A2AEnvelope, error) {
	raw, ok := msg.Metadata["a2a"]
	if !ok {
		return nil, nil
	}
	var s string
	switch v := raw.(type) {
	case string:
		s = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("a2a: re-marshal metadata: %w", err)
		}
		s = string(b)
	}
	var env A2AEnvelope
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		return nil, fmt.Errorf("a2a: unmarshal envelope: %w", err)
	}
	return &env, nil
}

// IsA2AMessage 判断 MailMessage 是否携带 A2A 信封。
func IsA2AMessage(msg MailMessage) bool {
	_, ok := msg.Metadata["a2a"]
	return ok
}

// UnmarshalPayload 将 envelope.Payload 反序列化到目标结构。
func (e *A2AEnvelope) UnmarshalPayload(v any) error {
	if len(e.Payload) == 0 {
		return fmt.Errorf("a2a: envelope payload is empty")
	}
	return json.Unmarshal(e.Payload, v)
}

// MarshalPayload 将结构体序列化为 envelope.Payload。
func (e *A2AEnvelope) MarshalPayload(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("a2a: marshal payload: %w", err)
	}
	e.Payload = b
	return nil
}
