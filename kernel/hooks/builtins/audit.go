package builtins

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
)

// AuditLogger 通过 Observer 接口记录审计日志。
// 输出格式为 JSON Lines，每行一条审计事件。
// 不侵入核心逻辑，零耦合。
type AuditLogger struct {
	writer io.Writer
	mu     sync.Mutex
}

// NewAuditLogger 创建审计日志记录器。
func NewAuditLogger(writer io.Writer) *AuditLogger {
	return &AuditLogger{writer: writer}
}

// auditEntry 是一条审计日志记录。
type auditEntry struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	Data      any    `json:"data,omitempty"`
}

func (a *AuditLogger) write(entry auditEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')
	a.mu.Lock()
	defer a.mu.Unlock()
	_, _ = a.writer.Write(data)
}

func (a *AuditLogger) OnLLMCall(_ context.Context, e observe.LLMCallEvent) {
	errMsg := ""
	if e.Error != nil {
		errMsg = e.Error.Error()
	}
	data := map[string]any{
		"model":             e.Model,
		"duration_ms":       e.Duration.Milliseconds(),
		"prompt_tokens":     e.Usage.PromptTokens,
		"completion_tokens": e.Usage.CompletionTokens,
		"tokens":            e.Usage.TotalTokens,
		"stop_reason":       e.StopReason,
		"error":             errMsg,
	}
	if e.EstimatedCostUSD > 0 {
		data["cost_usd"] = e.EstimatedCostUSD
	}
	ts := e.StartedAt.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	a.write(auditEntry{
		Timestamp: ts.Format(time.RFC3339),
		Type:      "llm_call",
		SessionID: e.SessionID,
		Data:      data,
	})
}

func (a *AuditLogger) OnToolCall(_ context.Context, e observe.ToolCallEvent) {
	errMsg := ""
	if e.Error != nil {
		errMsg = e.Error.Error()
	}
	ts := e.StartedAt.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	a.write(auditEntry{
		Timestamp: ts.Format(time.RFC3339),
		Type:      "tool_call",
		SessionID: e.SessionID,
		Data: map[string]any{
			"tool":        e.ToolName,
			"risk":        e.Risk,
			"duration_ms": e.Duration.Milliseconds(),
			"error":       errMsg,
		},
	})
}

func (a *AuditLogger) OnExecutionEvent(_ context.Context, e observe.ExecutionEvent) {
	data := map[string]any{
		"type":          e.Type,
		"event_id":      e.EventID,
		"event_version": e.EventVersion,
		"run_id":        e.RunID,
		"turn_id":       e.TurnID,
		"parent_id":     e.ParentID,
		"phase":         e.Phase,
		"actor":         e.Actor,
		"payload_kind":  e.PayloadKind,
	}
	if e.ToolName != "" {
		data["tool"] = e.ToolName
	}
	if e.CallID != "" {
		data["call_id"] = e.CallID
	}
	if e.Risk != "" {
		data["risk"] = e.Risk
	}
	if e.ReasonCode != "" {
		data["reason_code"] = e.ReasonCode
	}
	if e.Enforcement != "" {
		data["enforcement"] = e.Enforcement
	}
	if e.Model != "" {
		data["model"] = e.Model
	}
	if e.Duration > 0 {
		data["duration_ms"] = e.Duration.Milliseconds()
	}
	if e.Error != "" {
		data["error"] = e.Error
	}
	for k, v := range e.Metadata {
		data[k] = v
	}
	ts := e.Timestamp.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	a.write(auditEntry{
		Timestamp: ts.Format(time.RFC3339),
		Type:      "execution_event",
		SessionID: e.SessionID,
		Data:      data,
	})
}

func (a *AuditLogger) OnApproval(_ context.Context, e kernio.ApprovalEvent) {
	data := map[string]any{
		"id":           e.Request.ID,
		"kind":         e.Request.Kind,
		"tool":         e.Request.ToolName,
		"risk":         e.Request.Risk,
		"reason":       e.Request.Reason,
		"reason_code":  e.Request.ReasonCode,
		"enforcement":  e.Request.Enforcement,
		"requested_at": e.Request.RequestedAt.UTC().Format(time.RFC3339),
	}
	ts := e.Request.RequestedAt.UTC()
	if e.Decision != nil {
		data["approved"] = e.Decision.Approved
		data["decision_source"] = e.Decision.Source
		data["decided_at"] = e.Decision.DecidedAt.UTC().Format(time.RFC3339)
		if e.Decision.Reason != "" {
			data["decision_reason"] = e.Decision.Reason
		}
		if !e.Decision.DecidedAt.IsZero() {
			ts = e.Decision.DecidedAt.UTC()
		}
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	a.write(auditEntry{
		Timestamp: ts.Format(time.RFC3339),
		Type:      "approval_" + e.Type,
		SessionID: e.SessionID,
		Data:      data,
	})
}

func (a *AuditLogger) OnSessionEvent(_ context.Context, e observe.SessionEvent) {
	a.write(auditEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Type:      "session_" + e.Type,
		SessionID: e.SessionID,
	})
}

func (a *AuditLogger) OnError(_ context.Context, e observe.ErrorEvent) {
	a.write(auditEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Type:      "error",
		SessionID: e.SessionID,
		Data: map[string]any{
			"phase":   e.Phase,
			"message": e.Message,
		},
	})
}
