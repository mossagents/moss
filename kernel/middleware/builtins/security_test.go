package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mossagi/moss/kernel/errors"
	"github.com/mossagi/moss/kernel/middleware"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/tool"
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
		{Role: "viewer", Tools: []string{"read_file", "list_files"}, Action: RBACAllow},
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
	var kErr *errors.Error
	if !errors.As(err, &kErr) || kErr.Code != errors.ErrRateLimit {
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

	d := rule(tool.ToolSpec{Name: "read_file", Risk: tool.RiskLow}, nil)
	if d != Allow {
		t.Fatalf("low risk should allow, got %s", d)
	}

	d = rule(tool.ToolSpec{Name: "run_command", Risk: tool.RiskHigh}, nil)
	if d != RequireApproval {
		t.Fatalf("high risk should require approval, got %s", d)
	}
}
