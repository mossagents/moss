package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

// ── Identity ─────────────────────────────────────────

func TestIdentity_HasRole(t *testing.T) {
	id := &port.Identity{
		UserID: "u1",
		Roles:  []string{"viewer", "editor"},
	}
	if !id.HasRole("viewer") {
		t.Error("should have viewer role")
	}
	if id.HasRole("admin") {
		t.Error("should not have admin role")
	}
}

// ── RBAC ─────────────────────────────────────────────

func TestRBAC_AllowByRole(t *testing.T) {
	rules := []RBACRule{
		{Role: "viewer", Tools: []string{"read_file", "ls"}, Action: RBACAllow},
		{Role: "viewer", Tools: []string{"*"}, Action: RBACDeny},
	}
	mw := RBAC(rules)

	sess := &session.Session{
		ID:    "s1",
		State: make(map[string]any),
	}
	SetIdentity(sess.State, &port.Identity{UserID: "u1", Roles: []string{"viewer"}})

	// read_file 应允许
	mc := &middleware.Context{
		Phase:   middleware.BeforeToolCall,
		Session: sess,
		Tool:    &tool.ToolSpec{Name: "read_file"},
	}
	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if err != nil {
		t.Fatalf("read_file should be allowed: %v", err)
	}

	// write_file 应拒绝
	mc.Tool = &tool.ToolSpec{Name: "write_file"}
	err = mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if err != ErrDenied {
		t.Fatalf("write_file should be denied, got: %v", err)
	}
}

func TestRBAC_NoIdentity(t *testing.T) {
	mw := RBAC([]RBACRule{
		{Role: "viewer", Tools: []string{"*"}, Action: RBACDeny},
	})

	mc := &middleware.Context{
		Phase:   middleware.BeforeToolCall,
		Session: &session.Session{ID: "s1"},
		Tool:    &tool.ToolSpec{Name: "read_file"},
	}
	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if err != nil {
		t.Fatalf("should pass when no identity: %v", err)
	}
}

// ── AuthMiddleware ───────────────────────────────────

type mockAuthenticator struct {
	identity *port.Identity
	err      error
}

type executionEventRecorder struct {
	port.NoOpObserver
	events []port.ExecutionEvent
}

func (r *executionEventRecorder) OnExecutionEvent(_ context.Context, e port.ExecutionEvent) {
	r.events = append(r.events, e)
}

func (m *mockAuthenticator) Authenticate(_ context.Context, _ string) (*port.Identity, error) {
	return m.identity, m.err
}

func TestAuthMiddleware(t *testing.T) {
	auth := &mockAuthenticator{
		identity: &port.Identity{UserID: "u1", Roles: []string{"admin"}},
	}
	mw := AuthMiddleware(auth)

	sess := &session.Session{
		ID:     "s1",
		Config: session.SessionConfig{Metadata: map[string]any{"auth_token": "tok123"}},
	}
	mc := &middleware.Context{
		Phase:   middleware.OnSessionStart,
		Session: sess,
	}
	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if err != nil {
		t.Fatalf("auth should succeed: %v", err)
	}

	id := GetIdentity(sess.State)
	if id == nil || id.UserID != "u1" {
		t.Fatal("identity should be set in session state")
	}
}

// ── AuditLogger ──────────────────────────────────────

func TestAuditLogger_OnToolCall(t *testing.T) {
	var buf bytes.Buffer
	logger := NewAuditLogger(&buf)

	logger.OnToolCall(context.Background(), port.ToolCallEvent{
		SessionID: "s1",
		ToolName:  "read_file",
		Duration:  50 * time.Millisecond,
	})

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("parse audit entry: %v", err)
	}
	if entry.Type != "tool_call" {
		t.Fatalf("expected type tool_call, got %s", entry.Type)
	}
	if entry.SessionID != "s1" {
		t.Fatalf("expected session s1, got %s", entry.SessionID)
	}
}

func TestAuditLogger_OnLLMCallIncludesUsageAndCost(t *testing.T) {
	var buf bytes.Buffer
	logger := NewAuditLogger(&buf)

	logger.OnLLMCall(context.Background(), port.LLMCallEvent{
		SessionID: "s1",
		Model:     "gpt-5",
		Duration:  10 * time.Millisecond,
		Usage: port.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
		EstimatedCostUSD: 0.25,
	})

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("parse audit entry: %v", err)
	}
	data, ok := entry.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected llm_call data map, got %T", entry.Data)
	}
	if data["prompt_tokens"] != float64(10) || data["completion_tokens"] != float64(5) {
		t.Fatalf("unexpected token data %+v", data)
	}
	if data["cost_usd"] != 0.25 {
		t.Fatalf("unexpected cost data %+v", data)
	}
}

func TestAuditLogger_OnSessionEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewAuditLogger(&buf)

	logger.OnSessionEvent(context.Background(), port.SessionEvent{
		SessionID: "s2",
		Type:      "created",
	})

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("parse audit entry: %v", err)
	}
	if entry.Type != "session_created" {
		t.Fatalf("expected session_created, got %s", entry.Type)
	}
}

func TestAuditLogger_OnApproval(t *testing.T) {
	var buf bytes.Buffer
	logger := NewAuditLogger(&buf)

	logger.OnApproval(context.Background(), port.ApprovalEvent{
		SessionID: "s3",
		Type:      "resolved",
		Request: port.ApprovalRequest{
			ID:          "approval-1",
			Kind:        port.ApprovalKindTool,
			ToolName:    "write_file",
			Risk:        "high",
			Reason:      "policy requires approval",
			ReasonCode:  "tool.requires_approval",
			Enforcement: port.EnforcementRequireApproval,
			RequestedAt: time.Now(),
		},
		Decision: &port.ApprovalDecision{
			RequestID: "approval-1",
			Approved:  true,
			Source:    "cli",
			DecidedAt: time.Now(),
		},
	})

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("parse audit entry: %v", err)
	}
	if entry.Type != "approval_resolved" {
		t.Fatalf("expected approval_resolved, got %s", entry.Type)
	}
	if entry.SessionID != "s3" {
		t.Fatalf("expected session s3, got %s", entry.SessionID)
	}
	data, ok := entry.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected approval data map, got %T", entry.Data)
	}
	if data["reason_code"] != "tool.requires_approval" {
		t.Fatalf("expected reason_code, got %+v", data)
	}
}

func TestAuditLogger_OnExecutionEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewAuditLogger(&buf)

	logger.OnExecutionEvent(context.Background(), port.ExecutionEvent{
		Type:      port.ExecutionToolCompleted,
		SessionID: "s4",
		Timestamp: time.Now(),
		ToolName:  "write_file",
		CallID:    "call-1",
		Risk:      "high",
		Duration:  25 * time.Millisecond,
		Data: map[string]any{
			"is_error": false,
		},
	})

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("parse audit entry: %v", err)
	}
	if entry.Type != "execution_event" {
		t.Fatalf("expected execution_event, got %s", entry.Type)
	}
	if entry.SessionID != "s4" {
		t.Fatalf("expected session s4, got %s", entry.SessionID)
	}
	data, ok := entry.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", entry.Data)
	}
	if got := data["type"]; got != string(port.ExecutionToolCompleted) {
		t.Fatalf("expected execution type %s, got %v", port.ExecutionToolCompleted, got)
	}
}

// ── RateLimiter ───────────────────────────────────────

func TestRateLimiter_AllowsBurst(t *testing.T) {
	mw := RateLimiter(10, 3)

	sess := &session.Session{ID: "s1"}
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: sess,
	}

	// 前 3 次应该通过（burst = 3）
	for i := 0; i < 3; i++ {
		err := mw(context.Background(), mc, func(_ context.Context) error { return nil })
		if err != nil {
			t.Fatalf("request %d should pass: %v", i, err)
		}
	}

	// 第 4 次应该被限流
	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if err == nil {
		t.Fatal("should be rate limited")
	}
	var kErr *kerrors.Error
	if !stderrors.As(err, &kErr) || kErr.Code != kerrors.ErrRateLimit {
		t.Fatalf("expected ErrRateLimit, got: %v", err)
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	mw := RateLimiter(100, 1) // 100 rps, burst 1

	sess := &session.Session{ID: "s2"}
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: sess,
	}

	// 消耗 burst
	_ = mw(context.Background(), mc, func(_ context.Context) error { return nil })

	// 等待令牌恢复
	time.Sleep(20 * time.Millisecond) // 100 rps → 2 tokens in 20ms

	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if err != nil {
		t.Fatalf("should pass after refill: %v", err)
	}
}

// ── RiskBasedPolicy ──────────────────────────────────

func TestRiskBasedPolicy(t *testing.T) {
	rule := RiskBasedPolicy(tool.RiskHigh, RequireApproval)

	d := rule(PolicyContext{Tool: tool.ToolSpec{Name: "read_file", Risk: tool.RiskLow}})
	if d.Decision != Allow {
		t.Fatalf("low risk should allow, got %s", d.Decision)
	}

	d = rule(PolicyContext{Tool: tool.ToolSpec{Name: "run_command", Risk: tool.RiskHigh}})
	if d.Decision != RequireApproval {
		t.Fatalf("high risk should require approval, got %s", d.Decision)
	}
	if d.Reason.Code != "risk.threshold" {
		t.Fatalf("expected reason code risk.threshold, got %q", d.Reason.Code)
	}
}

func TestPolicyCheck_DenySendsHumanReadableMessage(t *testing.T) {
	io := port.NewBufferIO()
	sess := &session.Session{ID: "s1"}
	mw := PolicyCheck(DenyTool("write_file"))
	mc := &middleware.Context{
		Phase:   middleware.BeforeToolCall,
		Session: sess,
		Tool:    &tool.ToolSpec{Name: "write_file"},
		IO:      io,
	}
	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if err != ErrDenied {
		t.Fatalf("expected ErrDenied, got %v", err)
	}
	if len(io.Sent) == 0 {
		t.Fatal("expected human-readable denial message")
	}
	if got := io.Sent[0].Content; !strings.Contains(got, "reason_code=tool.denied") {
		t.Fatalf("expected reason code in denial message, got %q", got)
	}
}

func TestPolicyCheck_ApprovalPromptIncludesReasonCode(t *testing.T) {
	io := port.NewBufferIO()
	sess := &session.Session{ID: "s1"}
	mw := PolicyCheck(RequireApprovalFor("write_file"))
	mc := &middleware.Context{
		Phase:   middleware.BeforeToolCall,
		Session: sess,
		Tool:    &tool.ToolSpec{Name: "write_file", Risk: tool.RiskHigh},
		IO:      io,
	}
	if err := mw(context.Background(), mc, func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("expected approval flow to continue, got %v", err)
	}
	if len(io.Asked) == 0 {
		t.Fatal("expected approval request")
	}
	if got := io.Asked[0].Prompt; !strings.Contains(got, "reason_code=tool.requires_approval") {
		t.Fatalf("expected reason code in prompt, got %q", got)
	}
}

func TestPolicyCheck_EmitsPolicyRuleMatchedEvent(t *testing.T) {
	io := port.NewBufferIO()
	sess := &session.Session{ID: "s5"}
	observer := &executionEventRecorder{}
	mw := PolicyCheck(CommandRules(CommandPatternRule{
		Name:   "git-push",
		Match:  "git push*",
		Access: Deny,
	}))
	input, _ := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"push", "origin", "main"},
	})
	mc := &middleware.Context{
		Phase:    middleware.BeforeToolCall,
		Session:  sess,
		Tool:     &tool.ToolSpec{Name: "run_command", Risk: tool.RiskHigh},
		IO:       io,
		Observer: observer,
		Input:    input,
	}
	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if err != ErrDenied {
		t.Fatalf("expected ErrDenied, got %v", err)
	}
	if len(observer.events) == 0 {
		t.Fatal("expected policy match event")
	}
	if observer.events[0].Type != port.ExecutionPolicyRuleMatched {
		t.Fatalf("event type = %s", observer.events[0].Type)
	}
	if observer.events[0].Data["rule_name"] != "git-push" {
		t.Fatalf("unexpected event data: %+v", observer.events[0].Data)
	}
}
