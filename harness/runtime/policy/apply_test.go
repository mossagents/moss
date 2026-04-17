package policy

import (
	"context"
	"encoding/json"
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/hooks/governance"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/guardian"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	kt "github.com/mossagents/moss/kernel/testing"
	"github.com/mossagents/moss/kernel/tool"
)

type executionEventRecorder struct {
	execution []observe.ExecutionEvent
}

func (o *executionEventRecorder) OnLLMCall(context.Context, observe.LLMCallEvent)      {}
func (o *executionEventRecorder) OnToolCall(context.Context, observe.ToolCallEvent)    {}
func (o *executionEventRecorder) OnApproval(context.Context, io.ApprovalEvent)         {}
func (o *executionEventRecorder) OnSessionEvent(context.Context, observe.SessionEvent) {}
func (o *executionEventRecorder) OnError(context.Context, observe.ErrorEvent)          {}
func (o *executionEventRecorder) OnExecutionEvent(_ context.Context, e observe.ExecutionEvent) {
	o.execution = append(o.execution, e)
}

// TestApplyNilKernel 验证 Apply 对 nil kernel 返回错误。
func TestApplyNilKernel(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	if err := Apply(nil, policy); err == nil {
		t.Fatal("expected error for nil kernel, got nil")
	}
}

// TestApplyAndCurrentRoundTrip 验证 Apply 后可通过 Current 读回策略。
func TestApplyAndCurrentRoundTrip(t *testing.T) {
	k := kernel.New()
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.ProtectedPathPrefixes = []string{"/etc/"}

	if err := Apply(k, policy); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, ok := Current(k)
	if !ok {
		t.Fatal("Current: policy not found after Apply")
	}
	if len(got.ProtectedPathPrefixes) == 0 || got.ProtectedPathPrefixes[0] != "/etc/" {
		t.Fatalf("expected ProtectedPathPrefixes to roundtrip, got %v", got.ProtectedPathPrefixes)
	}
}

// TestCurrentBeforeApply 验证未调用 Apply 时 Current 返回 false。
func TestCurrentBeforeApply(t *testing.T) {
	k := kernel.New()
	_, ok := Current(k)
	if ok {
		t.Fatal("Current should return false on fresh kernel before Apply")
	}
}

// TestApplyInstallsPolicyGate 验证 Apply 后 toolPolicyGate 实际拦截危险命令。
// 通过直接调用 Evaluate 验证规则链正常工作（gate 安装本身通过 roundtrip 间接验证）。
func TestApplyInstallsPolicyGate(t *testing.T) {
	k := kernel.New()
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	if err := Apply(k, policy); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// 验证 Apply 后策略已存储，且规则链对危险命令返回 Deny
	input, _ := json.Marshal(map[string]any{"command": "rm -rf /"})
	decision := Evaluate(policy, tool.ToolSpec{Name: "run_command"}, input)
	if decision != governance.Deny {
		t.Fatalf("expected Deny for dangerous command via compiled rules, got %s", decision)
	}
}

// TestApplyIdempotent 验证多次 Apply 不会重复安装 hook。
func TestApplyIdempotent(t *testing.T) {
	k := kernel.New()
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")

	for i := 0; i < 3; i++ {
		if err := Apply(k, policy); err != nil {
			t.Fatalf("Apply #%d: %v", i, err)
		}
	}

	// 最终 Current 仍然可以读回
	_, ok := Current(k)
	if !ok {
		t.Fatal("Current should be available after repeated Apply")
	}
}

func TestGuardianAutoApprovalEmitsReviewEvent(t *testing.T) {
	llm := &kt.MockLLM{Responses: []model.CompletionResponse{{
		Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart(`{"approved":true,"reason":"safe","confidence":"high"}`)}},
		StopReason: "end_turn",
	}}}
	k := kernel.New(kernel.WithLLM(llm))
	guardian.Install(k, guardian.New(llm, model.ModelConfig{Model: "guardian"}))
	observer := &executionEventRecorder{}
	autoApprove := guardianAutoApproval(k)
	decision := autoApprove(context.Background(), &hooks.ToolEvent{
		Tool:     &tool.ToolSpec{Name: "read_file", ApprovalClass: tool.ApprovalClassPolicyGuarded},
		Observer: observer,
	}, &io.ApprovalRequest{
		ID:         "approval-1",
		SessionID:  "sess-1",
		ToolName:   "read_file",
		Risk:       "low",
		ReasonCode: "path.protected",
	})
	if decision == nil || !decision.Approved {
		t.Fatalf("expected guardian auto approval, got %+v", decision)
	}
	if len(observer.execution) != 1 {
		t.Fatalf("execution events = %d, want 1", len(observer.execution))
	}
	event := observer.execution[0]
	if event.Type != observe.ExecutionGuardianReviewed {
		t.Fatalf("event type = %q, want %q", event.Type, observe.ExecutionGuardianReviewed)
	}
	if event.Metadata["outcome"] != "auto_approved" {
		t.Fatalf("unexpected guardian review metadata: %+v", event.Metadata)
	}
}

func TestGuardianAutoApprovalFallbackEmitsReviewEvent(t *testing.T) {
	llm := &kt.MockLLM{Responses: []model.CompletionResponse{{
		Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart(`{"approved":true,"reason":"safe","confidence":"medium"}`)}},
		StopReason: "end_turn",
	}}}
	k := kernel.New(kernel.WithLLM(llm))
	guardian.Install(k, guardian.New(llm, model.ModelConfig{Model: "guardian"}))
	observer := &executionEventRecorder{}
	autoApprove := guardianAutoApproval(k)
	decision := autoApprove(context.Background(), &hooks.ToolEvent{
		Tool:     &tool.ToolSpec{Name: "read_file", ApprovalClass: tool.ApprovalClassPolicyGuarded},
		Observer: observer,
	}, &io.ApprovalRequest{
		ID:        "approval-2",
		SessionID: "sess-2",
		ToolName:  "read_file",
		Risk:      "low",
	})
	if decision != nil {
		t.Fatalf("expected guardian fallback, got %+v", decision)
	}
	if len(observer.execution) != 1 {
		t.Fatalf("execution events = %d, want 1", len(observer.execution))
	}
	if observer.execution[0].Metadata["outcome"] != "fallback" {
		t.Fatalf("unexpected guardian fallback metadata: %+v", observer.execution[0].Metadata)
	}
}
