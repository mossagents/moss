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
	"github.com/mossagents/moss/kernel/tool"
)

// ─── helpers ───────────────────────────────────────────────────────────────

func newSession(id string) *session.Session {
	return &session.Session{ID: id, State: map[string]any{}}
}

func toolEvent(name string, sess *session.Session) *hooks.ToolEvent {
	return &hooks.ToolEvent{
		Stage:   hooks.ToolLifecycleBefore,
		Tool:    &tool.ToolSpec{Name: name, Risk: tool.RiskLow},
		Session: sess,
	}
}

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

// ─── SetIdentity / GetIdentity ─────────────────────────────────────────────

func TestSetGetIdentity(t *testing.T) {
	state := map[string]any{}
	id := &kernio.Identity{UserID: "user-1", Roles: []string{"admin"}}
	builtins.SetIdentity(state, id)
	got := builtins.GetIdentity(state)
	if got == nil || got.UserID != "user-1" {
		t.Fatalf("expected identity with UserID=user-1, got %+v", got)
	}
}

func TestGetIdentity_NilState(t *testing.T) {
	if builtins.GetIdentity(nil) != nil {
		t.Fatal("nil state should return nil identity")
	}
}

func TestGetIdentity_Missing(t *testing.T) {
	if builtins.GetIdentity(map[string]any{}) != nil {
		t.Fatal("missing key should return nil identity")
	}
}

func TestSetIdentity_NilState(t *testing.T) {
	// Should not panic with nil state.
	builtins.SetIdentity(nil, &kernio.Identity{UserID: "x"})
}

// ─── RBAC ──────────────────────────────────────────────────────────────────

func TestRBAC_NoRules_Allows(t *testing.T) {
	hook := builtins.RBAC(nil)
	sess := newSession("s1")
	if err := hook(context.Background(), toolEvent("bash", sess)); err != nil {
		t.Fatalf("no rules should allow: %v", err)
	}
}

func TestRBAC_NoIdentity_Denied(t *testing.T) {
	hook := builtins.RBAC([]builtins.RBACRule{{Role: "admin", Tools: []string{"*"}, Action: builtins.RBACAllow}})
	sess := newSession("s2") // no identity set
	err := hook(context.Background(), toolEvent("bash", sess))
	if !errors.Is(err, builtins.ErrDenied) {
		t.Fatalf("expected ErrDenied without identity, got: %v", err)
	}
}

func TestRBAC_AllowRule_Passes(t *testing.T) {
	hook := builtins.RBAC([]builtins.RBACRule{{Role: "operator", Tools: []string{"*"}, Action: builtins.RBACAllow}})
	sess := newSession("s3")
	builtins.SetIdentity(sess.State, &kernio.Identity{UserID: "u1", Roles: []string{"operator"}})
	if err := hook(context.Background(), toolEvent("bash", sess)); err != nil {
		t.Fatalf("allow rule should pass: %v", err)
	}
}

func TestRBAC_DenyRule_Blocked(t *testing.T) {
	hook := builtins.RBAC([]builtins.RBACRule{{Role: "guest", Tools: []string{"delete_file"}, Action: builtins.RBACDeny}})
	sess := newSession("s4")
	builtins.SetIdentity(sess.State, &kernio.Identity{UserID: "u2", Roles: []string{"guest"}})
	err := hook(context.Background(), toolEvent("delete_file", sess))
	if !errors.Is(err, builtins.ErrDenied) {
		t.Fatalf("expected ErrDenied for deny rule, got: %v", err)
	}
}

func TestRBAC_NoMatchingRule_Denied(t *testing.T) {
	hook := builtins.RBAC([]builtins.RBACRule{{Role: "admin", Tools: []string{"*"}, Action: builtins.RBACAllow}})
	sess := newSession("s5")
	builtins.SetIdentity(sess.State, &kernio.Identity{UserID: "u3", Roles: []string{"viewer"}})
	err := hook(context.Background(), toolEvent("bash", sess))
	if !errors.Is(err, builtins.ErrDenied) {
		t.Fatalf("expected ErrDenied when no rule matches, got: %v", err)
	}
}

func TestRBAC_NonBeforeStage_Skipped(t *testing.T) {
	hook := builtins.RBAC([]builtins.RBACRule{{Role: "admin", Tools: []string{"*"}, Action: builtins.RBACDeny}})
	ev := &hooks.ToolEvent{Stage: hooks.ToolLifecycleAfter}
	if err := hook(context.Background(), ev); err != nil {
		t.Fatalf("non-before stage should be skipped: %v", err)
	}
}

// ─── RateLimiter ──────────────────────────────────────────────────────────

func TestRateLimiter_AllowsWithinBurst(t *testing.T) {
	hook := builtins.RateLimiter(100, 5)
	sess := &session.Session{ID: "rate-1"}
	for i := 0; i < 5; i++ {
		ev := &hooks.LLMEvent{Session: sess}
		if err := hook(context.Background(), ev); err != nil {
			t.Fatalf("call %d should be allowed: %v", i, err)
		}
	}
}

func TestRateLimiter_BlocksWhenExhausted(t *testing.T) {
	hook := builtins.RateLimiter(1, 1)
	sess := &session.Session{ID: "rate-2"}
	ev := &hooks.LLMEvent{Session: sess}
	// First call consumes the single token.
	_ = hook(context.Background(), ev)
	// Second call should fail immediately.
	if err := hook(context.Background(), ev); err == nil {
		t.Fatal("expected rate limit error on second call with burst=1")
	}
}

// ─── RiskBasedPolicy ──────────────────────────────────────────────────────

func makeRiskCtx(risk tool.RiskLevel) builtins.PolicyContext {
	return builtins.PolicyContext{
		Tool: tool.ToolSpec{Name: "test", Risk: risk},
	}
}

func TestRiskBasedPolicy_BelowThreshold_Allows(t *testing.T) {
	rule := builtins.RiskBasedPolicy(tool.RiskHigh, builtins.Deny)
	result := rule(makeRiskCtx(tool.RiskLow))
	if result.Decision != builtins.Allow {
		t.Fatalf("low risk below high threshold should allow, got %v", result.Decision)
	}
}

func TestRiskBasedPolicy_AtThreshold_Denies(t *testing.T) {
	rule := builtins.RiskBasedPolicy(tool.RiskMedium, builtins.Deny)
	result := rule(makeRiskCtx(tool.RiskHigh))
	if result.Decision != builtins.Deny {
		t.Fatalf("high risk should be denied, got %v", result.Decision)
	}
}

func TestRiskBasedPolicy_AtThreshold_RequiresApproval(t *testing.T) {
	rule := builtins.RiskBasedPolicy(tool.RiskMedium, builtins.RequireApproval)
	result := rule(makeRiskCtx(tool.RiskMedium))
	if result.Decision != builtins.RequireApproval {
		t.Fatalf("medium risk at medium threshold should require approval, got %v", result.Decision)
	}
}

// ─── RequireApprovalFor ───────────────────────────────────────────────────

func TestRequireApprovalFor_MatchingTool(t *testing.T) {
	rule := builtins.RequireApprovalFor("bash", "run_code")
	ctx := builtins.PolicyContext{Tool: tool.ToolSpec{Name: "bash"}}
	result := rule(ctx)
	if result.Decision != builtins.RequireApproval {
		t.Fatalf("expected RequireApproval, got %v", result.Decision)
	}
}

func TestRequireApprovalFor_NonMatchingTool(t *testing.T) {
	rule := builtins.RequireApprovalFor("bash")
	ctx := builtins.PolicyContext{Tool: tool.ToolSpec{Name: "read_file"}}
	result := rule(ctx)
	if result.Decision != builtins.Allow {
		t.Fatalf("expected Allow for non-matching tool, got %v", result.Decision)
	}
}

// ─── DenyEffects ─────────────────────────────────────────────────────────

func TestDenyEffects_MatchingEffect(t *testing.T) {
	rule := builtins.DenyEffects(tool.EffectExternalSideEffect)
	ctx := builtins.PolicyContext{
		Tool: tool.ToolSpec{Name: "http", Effects: []tool.Effect{tool.EffectExternalSideEffect}},
	}
	result := rule(ctx)
	if result.Decision != builtins.Deny {
		t.Fatalf("expected Deny for matching effect, got %v", result.Decision)
	}
}

func TestDenyEffects_NoMatch(t *testing.T) {
	rule := builtins.DenyEffects(tool.EffectExternalSideEffect)
	ctx := builtins.PolicyContext{
		Tool: tool.ToolSpec{Name: "read", Effects: []tool.Effect{tool.EffectReadOnly}},
	}
	result := rule(ctx)
	if result.Decision != builtins.Allow {
		t.Fatalf("expected Allow when effect not in set, got %v", result.Decision)
	}
}

// ─── RequireApprovalForEffects ────────────────────────────────────────────

func TestRequireApprovalForEffects_MatchingEffect(t *testing.T) {
	rule := builtins.RequireApprovalForEffects(tool.EffectWritesWorkspace)
	ctx := builtins.PolicyContext{
		Tool: tool.ToolSpec{Name: "write_file", Effects: []tool.Effect{tool.EffectWritesWorkspace}},
	}
	result := rule(ctx)
	if result.Decision != builtins.RequireApproval {
		t.Fatalf("expected RequireApproval for workspace effect, got %v", result.Decision)
	}
}

func TestRequireApprovalForEffects_NoMatch(t *testing.T) {
	rule := builtins.RequireApprovalForEffects(tool.EffectWritesWorkspace)
	ctx := builtins.PolicyContext{
		Tool: tool.ToolSpec{Name: "read", Effects: []tool.Effect{tool.EffectReadOnly}},
	}
	result := rule(ctx)
	if result.Decision != builtins.Allow {
		t.Fatalf("expected Allow when effect not in set, got %v", result.Decision)
	}
}

// ─── RequireApprovalForApprovalClasses ───────────────────────────────────

func TestRequireApprovalForApprovalClasses_Match(t *testing.T) {
	rule := builtins.RequireApprovalForApprovalClasses(tool.ApprovalClassExplicitUser)
	ctx := builtins.PolicyContext{
		Tool: tool.ToolSpec{Name: "dangerous", ApprovalClass: tool.ApprovalClassExplicitUser},
	}
	result := rule(ctx)
	if result.Decision != builtins.RequireApproval {
		t.Fatalf("expected RequireApproval for explicit_user class, got %v", result.Decision)
	}
}

func TestRequireApprovalForApprovalClasses_NoMatch(t *testing.T) {
	rule := builtins.RequireApprovalForApprovalClasses(tool.ApprovalClassExplicitUser)
	ctx := builtins.PolicyContext{
		Tool: tool.ToolSpec{Name: "read", ApprovalClass: tool.ApprovalClassNone},
	}
	result := rule(ctx)
	if result.Decision != builtins.Allow {
		t.Fatalf("expected Allow for non-matching class, got %v", result.Decision)
	}
}

// ─── RequireApprovalForURLHost ────────────────────────────────────────────

func makeHTTPCtx(urlJSON string) builtins.PolicyContext {
	return builtins.PolicyContext{
		Tool:  tool.ToolSpec{Name: "http_request"},
		Input: json.RawMessage(urlJSON),
	}
}

func TestRequireApprovalForURLHost_AllowedHost(t *testing.T) {
	rule := builtins.RequireApprovalForURLHost("api.example.com")
	ctx := makeHTTPCtx(`{"url":"https://api.example.com/v1/resource"}`)
	result := rule(ctx)
	if result.Decision != builtins.Allow {
		t.Fatalf("expected Allow for allowed host, got %v", result.Decision)
	}
}

func TestRequireApprovalForURLHost_UnknownHost(t *testing.T) {
	rule := builtins.RequireApprovalForURLHost("api.example.com")
	ctx := makeHTTPCtx(`{"url":"https://evil.example.org/data"}`)
	result := rule(ctx)
	if result.Decision != builtins.RequireApproval {
		t.Fatalf("expected RequireApproval for unknown host, got %v", result.Decision)
	}
}

func TestRequireApprovalForURLHost_EmptyHosts(t *testing.T) {
	rule := builtins.RequireApprovalForURLHost()
	ctx := makeHTTPCtx(`{"url":"https://anything.com"}`)
	result := rule(ctx)
	if result.Decision != builtins.Allow {
		t.Fatalf("expected Allow when no hosts configured, got %v", result.Decision)
	}
}

func TestRequireApprovalForURLHost_NonHTTPTool(t *testing.T) {
	rule := builtins.RequireApprovalForURLHost("api.example.com")
	ctx := builtins.PolicyContext{
		Tool:  tool.ToolSpec{Name: "bash"},
		Input: json.RawMessage(`{"url":"https://evil.org"}`),
	}
	result := rule(ctx)
	if result.Decision != builtins.Allow {
		t.Fatalf("expected Allow for non-http tool, got %v", result.Decision)
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
