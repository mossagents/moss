package builtins_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/tool"
)

// makeToolEvent 构建一个用于测试的 ToolEvent。
func makeToolEvent(spec tool.ToolSpec) *hooks.ToolEvent {
	return &hooks.ToolEvent{
		Stage:    hooks.ToolLifecycleBefore,
		Tool:     &spec,
		ToolName: spec.Name,
		CallID:   "test-call",
	}
}

// TestPolicyCheckDenyTool 验证 DenyTool 规则在命中时拒绝工具调用。
func TestPolicyCheckDenyTool(t *testing.T) {
	hook := builtins.PolicyCheck(builtins.DenyTool("rm", "drop_table"))
	ctx := context.Background()

	t.Run("denied tool returns error", func(t *testing.T) {
		err := hook(ctx, makeToolEvent(tool.ToolSpec{Name: "rm"}))
		if !errors.Is(err, builtins.ErrDenied) {
			t.Fatalf("expected ErrDenied, got %v", err)
		}
	})

	t.Run("allowed tool passes", func(t *testing.T) {
		err := hook(ctx, makeToolEvent(tool.ToolSpec{Name: "list_files"}))
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("after stage skipped", func(t *testing.T) {
		ev := makeToolEvent(tool.ToolSpec{Name: "rm"})
		ev.Stage = hooks.ToolLifecycleAfter
		err := hook(ctx, ev)
		if err != nil {
			t.Fatalf("after stage should not be checked, got %v", err)
		}
	})

	t.Run("nil event is safe", func(t *testing.T) {
		err := hook(ctx, nil)
		if err != nil {
			t.Fatalf("nil event should return nil, got %v", err)
		}
	})
}

// TestPolicyCheckDefaultAllow 验证 DefaultAllow 规则放行一切工具。
func TestPolicyCheckDefaultAllow(t *testing.T) {
	hook := builtins.PolicyCheck(builtins.DefaultAllow())
	ctx := context.Background()

	err := hook(ctx, makeToolEvent(tool.ToolSpec{Name: "anything"}))
	if err != nil {
		t.Fatalf("expected nil from DefaultAllow, got %v", err)
	}
}

// TestPolicyCheckDenyApprovalClass 验证 DenyApprovalClasses 正确拒绝。
func TestPolicyCheckDenyApprovalClass(t *testing.T) {
	hook := builtins.PolicyCheck(builtins.DenyApprovalClasses(tool.ApprovalClassSupervisorOnly))
	ctx := context.Background()

	t.Run("supervisor_only denied", func(t *testing.T) {
		spec := tool.ToolSpec{
			Name:          "privileged_op",
			ApprovalClass: tool.ApprovalClassSupervisorOnly,
		}
		err := hook(ctx, makeToolEvent(spec))
		if !errors.Is(err, builtins.ErrDenied) {
			t.Fatalf("expected ErrDenied for supervisor_only, got %v", err)
		}
	})

	t.Run("normal tool passes", func(t *testing.T) {
		spec := tool.ToolSpec{Name: "read_file"}
		err := hook(ctx, makeToolEvent(spec))
		if err != nil {
			t.Fatalf("normal tool should pass, got %v", err)
		}
	})
}

// TestPolicyCheckMultipleRules 验证多规则取最严格决策。
func TestPolicyCheckMultipleRules(t *testing.T) {
	hook := builtins.PolicyCheck(builtins.DefaultAllow(), builtins.DenyTool("bad_tool"))
	ctx := context.Background()

	err := hook(ctx, makeToolEvent(tool.ToolSpec{Name: "bad_tool"}))
	if !errors.Is(err, builtins.ErrDenied) {
		t.Fatalf("Deny should win over Allow, got %v", err)
	}
}

// TestAutoEnforceApprovalClass 验证 AutoEnforceApprovalClass 对显式声明的工具有效。
func TestAutoEnforceApprovalClass(t *testing.T) {
	rule := builtins.AutoEnforceApprovalClass()

	t.Run("explicit_user requires approval", func(t *testing.T) {
		pctx := io.PolicyContext{
			Tool: tool.ToolSpec{
				Name:          "delete_account",
				ApprovalClass: tool.ApprovalClassExplicitUser,
			},
		}
		result := rule(pctx)
		if result.Decision != io.PolicyRequireApproval {
			t.Fatalf("expected RequireApproval for ApprovalClassExplicitUser, got %v", result.Decision)
		}
	})

	t.Run("policy_guarded passes through", func(t *testing.T) {
		pctx := io.PolicyContext{
			Tool: tool.ToolSpec{
				Name:          "write_file",
				ApprovalClass: tool.ApprovalClassPolicyGuarded,
			},
		}
		result := rule(pctx)
		if result.Decision != io.PolicyAllow {
			t.Fatalf("expected Allow for ApprovalClassPolicyGuarded, got %v", result.Decision)
		}
	})

	t.Run("no approval class defaults to Allow", func(t *testing.T) {
		pctx := io.PolicyContext{
			Tool: tool.ToolSpec{Name: "list_files"},
		}
		result := rule(pctx)
		if result.Decision != io.PolicyAllow {
			t.Fatalf("expected Allow for tool with no explicit approval class, got %v", result.Decision)
		}
	})

	t.Run("supervisor_only passes through AutoEnforceApprovalClass", func(t *testing.T) {
		// AutoEnforceApprovalClass 只处理 ExplicitUser；SupervisorOnly 由其他规则处理
		pctx := io.PolicyContext{
			Tool: tool.ToolSpec{
				Name:          "admin_op",
				ApprovalClass: tool.ApprovalClassSupervisorOnly,
			},
		}
		result := rule(pctx)
		if result.Decision != io.PolicyAllow {
			t.Fatalf("expected Allow (SupervisorOnly handled elsewhere), got %v", result.Decision)
		}
	})
}

// TestAutoEnforceApprovalClassInPolicyCheck 验证 AutoEnforceApprovalClass 与 PolicyCheck 集成后
// 能在 ToolEvent 中正确触发审批。
func TestAutoEnforceApprovalClassInPolicyCheck(t *testing.T) {
	hook := builtins.PolicyCheck(builtins.AutoEnforceApprovalClass(), builtins.DefaultAllow())
	ctx := context.Background()

	t.Run("explicit_user without IO passes through safely", func(t *testing.T) {
		// RequireApproval + IO == nil → nil error（无审批处理器时安全放行，防止阻断 agent）
		spec := tool.ToolSpec{
			Name:          "delete_account",
			ApprovalClass: tool.ApprovalClassExplicitUser,
		}
		err := hook(ctx, makeToolEvent(spec))
		if err != nil {
			t.Fatalf("expected nil when no IO handler is present, got %v", err)
		}
	})

	t.Run("normal tool passes", func(t *testing.T) {
		spec := tool.ToolSpec{Name: "read_file"}
		err := hook(ctx, makeToolEvent(spec))
		if err != nil {
			t.Fatalf("expected nil for normal tool, got %v", err)
		}
	})
}
