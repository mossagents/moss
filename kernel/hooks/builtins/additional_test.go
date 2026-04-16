package builtins_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/plugin"
	"github.com/mossagents/moss/kernel/session"
)

// ─── AuditLogger ───────────────────────────────────────────────────────────

func TestAuditLogger_OnLLMCall(t *testing.T) {
	var buf bytes.Buffer
	al := builtins.NewAuditLogger(&buf)
	al.OnLLMCall(context.Background(), observe.LLMCallEvent{
		SessionID:  "sess-1",
		Model:      "gpt-4",
		StartedAt:  time.Now(),
		Usage:      model.TokenUsage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15},
		StopReason: "end_turn",
	})
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if entry["type"] != "llm_call" {
		t.Fatalf("expected type=llm_call, got %v", entry["type"])
	}
	if entry["session_id"] != "sess-1" {
		t.Fatalf("expected session_id=sess-1, got %v", entry["session_id"])
	}
}

func TestAuditLogger_OnLLMCall_WithError(t *testing.T) {
	var buf bytes.Buffer
	al := builtins.NewAuditLogger(&buf)
	al.OnLLMCall(context.Background(), observe.LLMCallEvent{
		Error: errors.New("model unavailable"),
	})
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data, _ := entry["data"].(map[string]any)
	if data == nil || data["error"] == "" {
		t.Fatalf("expected error in data, got %v", data)
	}
}

func TestAuditLogger_OnLLMCall_WithCost(t *testing.T) {
	var buf bytes.Buffer
	al := builtins.NewAuditLogger(&buf)
	al.OnLLMCall(context.Background(), observe.LLMCallEvent{
		EstimatedCostUSD: 0.01,
	})
	line := buf.String()
	if !strings.Contains(line, "cost_usd") {
		t.Fatalf("expected cost_usd in output, got: %s", line)
	}
}

func TestAuditLogger_OnToolCall(t *testing.T) {
	var buf bytes.Buffer
	al := builtins.NewAuditLogger(&buf)
	al.OnToolCall(context.Background(), observe.ToolCallEvent{
		SessionID: "sess-2",
		ToolName:  "bash",
		Risk:      "medium",
	})
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry["type"] != "tool_call" {
		t.Fatalf("expected type=tool_call, got %v", entry["type"])
	}
}

func TestAuditLogger_OnApproval(t *testing.T) {
	var buf bytes.Buffer
	al := builtins.NewAuditLogger(&buf)
	al.OnApproval(context.Background(), kernio.ApprovalEvent{
		Type:      "requested",
		SessionID: "sess-3",
		Request: kernio.ApprovalRequest{
			ID:          "req-1",
			ToolName:    "delete_file",
			Risk:        "high",
			RequestedAt: time.Now(),
		},
	})
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry["type"] != "approval_requested" {
		t.Fatalf("expected type=approval_requested, got %v", entry["type"])
	}
}

func TestAuditLogger_OnApproval_WithDecision(t *testing.T) {
	var buf bytes.Buffer
	al := builtins.NewAuditLogger(&buf)
	decided := time.Now()
	al.OnApproval(context.Background(), kernio.ApprovalEvent{
		Type:      "decided",
		SessionID: "sess-4",
		Request:   kernio.ApprovalRequest{RequestedAt: time.Now()},
		Decision: &kernio.ApprovalDecision{
			Approved:  true,
			Source:    "user",
			DecidedAt: decided,
			Reason:    "looks safe",
		},
	})
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data, _ := entry["data"].(map[string]any)
	if data["approved"] != true {
		t.Fatalf("expected approved=true, got %v", data["approved"])
	}
}

func TestAuditLogger_OnSessionEvent(t *testing.T) {
	var buf bytes.Buffer
	al := builtins.NewAuditLogger(&buf)
	al.OnSessionEvent(context.Background(), observe.SessionEvent{
		Type: "started", SessionID: "sess-5",
	})
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry["type"] != "session_started" {
		t.Fatalf("expected type=session_started, got %v", entry["type"])
	}
}

func TestAuditLogger_OnError(t *testing.T) {
	var buf bytes.Buffer
	al := builtins.NewAuditLogger(&buf)
	al.OnError(context.Background(), observe.ErrorEvent{
		Phase: "tool_call", Message: "boom", SessionID: "sess-6",
	})
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry["type"] != "error" {
		t.Fatalf("expected type=error, got %v", entry["type"])
	}
}

func TestAuditLogger_OnExecutionEvent(t *testing.T) {
	var buf bytes.Buffer
	al := builtins.NewAuditLogger(&buf)
	al.OnExecutionEvent(context.Background(), observe.ExecutionEvent{
		Type:      "tool_approved",
		SessionID: "sess-7",
		ToolName:  "bash",
		Risk:      "high",
		CallID:    "cid-1",
		Timestamp: time.Now(),
	})
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry["type"] != "execution_event" {
		t.Fatalf("expected type=execution_event, got %v", entry["type"])
	}
}

// ─── LoggerPlugin ──────────────────────────────────────────────────────────

func TestLoggerPlugin_Construction(t *testing.T) {
	p := builtins.LoggerPlugin()
	if p.Name() != "logger" {
		t.Fatalf("expected name=logger, got %q", p.Name())
	}
	// Install into registry and verify pipelines are non-empty.
	reg := hooks.NewRegistry()
	plugin.Install(reg, p)
	if reg.BeforeLLM.Empty() {
		t.Fatal("BeforeLLM should not be empty after install")
	}
	if reg.OnToolLifecycle.Empty() {
		t.Fatal("OnToolLifecycle should not be empty after install")
	}
	if reg.OnSessionLifecycle.Empty() {
		t.Fatal("OnSessionLifecycle should not be empty after install")
	}
}

func TestLoggerPlugin_BeforeLLMInterceptor_Invokes(t *testing.T) {
	reg := hooks.NewRegistry()
	plugin.Install(reg, builtins.LoggerPlugin())
	sess := &session.Session{ID: "log-test"}
	ev := &hooks.LLMEvent{Session: sess}
	if err := reg.BeforeLLM.Run(context.Background(), ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoggerPlugin_BeforeLLMInterceptor_PropagatesError(t *testing.T) {
	reg := hooks.NewRegistry()
	plugin.Install(reg, builtins.LoggerPlugin())
	// Add a hook that returns an error to test propagation through the interceptor.
	sentinel := errors.New("downstream error")
	reg.BeforeLLM.AddHook("fail", func(_ context.Context, _ *hooks.LLMEvent) error {
		return sentinel
	}, 2000)
	ev := &hooks.LLMEvent{Session: &session.Session{}}
	err := reg.BeforeLLM.Run(context.Background(), ev)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got: %v", err)
	}
}

// ─── LoggerPlugin interceptors ────────────────────────────────────────────

func TestLoggerPlugin_AfterLLM(t *testing.T) {
	reg := hooks.NewRegistry()
	plugin.Install(reg, builtins.LoggerPlugin())
	ev := &hooks.LLMEvent{Session: &session.Session{ID: "after-test"}}
	if err := reg.AfterLLM.Run(context.Background(), ev); err != nil {
		t.Fatalf("AfterLLM run failed: %v", err)
	}
}

func TestLoggerPlugin_OnError(t *testing.T) {
	reg := hooks.NewRegistry()
	plugin.Install(reg, builtins.LoggerPlugin())
	ev := &hooks.ErrorEvent{Error: errors.New("test error")}
	if err := reg.OnError.Run(context.Background(), ev); err != nil {
		t.Fatalf("OnError should not propagate error, got %v", err)
	}
}
