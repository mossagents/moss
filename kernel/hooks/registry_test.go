package hooks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mossagents/moss/kernel/hooks"
)

// TestRegistryToolPolicyGate 验证 toolPolicyGate 在 OnToolLifecycle pipeline 之前执行
// 且无法被 pipeline 拦截器绕过。
func TestRegistryToolPolicyGate(t *testing.T) {
	ctx := context.Background()
	gateErr := errors.New("gate blocked")

	t.Run("gate blocks before pipeline runs", func(t *testing.T) {
		r := hooks.NewRegistry()
		r.SetToolPolicyGate(func(_ context.Context, _ *hooks.ToolEvent) error {
			return gateErr
		})

		pipelineRan := false
		r.OnToolLifecycle.AddHook("observer", func(_ context.Context, _ *hooks.ToolEvent) error {
			pipelineRan = true
			return nil
		}, 0)

		ev := &hooks.ToolEvent{Stage: hooks.ToolLifecycleBefore}
		err := r.RunToolPolicyGate(ctx, ev)
		if !errors.Is(err, gateErr) {
			t.Fatalf("expected gate error, got %v", err)
		}
		if pipelineRan {
			t.Fatal("pipeline should not run when gate is checked separately")
		}
	})

	t.Run("nil gate is safe", func(t *testing.T) {
		r := hooks.NewRegistry()
		ev := &hooks.ToolEvent{Stage: hooks.ToolLifecycleBefore}
		if err := r.RunToolPolicyGate(ctx, ev); err != nil {
			t.Fatalf("nil gate should return nil, got %v", err)
		}
	})

	t.Run("gate only runs on Before stage", func(t *testing.T) {
		r := hooks.NewRegistry()
		called := false
		r.SetToolPolicyGate(func(_ context.Context, _ *hooks.ToolEvent) error {
			called = true
			return errors.New("should not trigger")
		})

		ev := &hooks.ToolEvent{Stage: hooks.ToolLifecycleAfter}
		err := r.RunToolPolicyGate(ctx, ev)
		if err != nil {
			t.Fatalf("gate should not run on After stage, got %v", err)
		}
		if called {
			t.Fatal("gate should not be called for After stage")
		}
	})

	t.Run("nil registry is safe", func(t *testing.T) {
		var r *hooks.Registry
		ev := &hooks.ToolEvent{Stage: hooks.ToolLifecycleBefore}
		if err := r.RunToolPolicyGate(ctx, ev); err != nil {
			t.Fatalf("nil registry should return nil, got %v", err)
		}
	})
}

// TestRegistryTrust 验证 IsTrusted/SetTrusted 正确控制信任状态。
func TestRegistryTrust(t *testing.T) {
	t.Run("default is trusted", func(t *testing.T) {
		r := hooks.NewRegistry()
		if !r.IsTrusted() {
			t.Fatal("new registry should be trusted by default")
		}
	})

	t.Run("SetTrusted false disables trust", func(t *testing.T) {
		r := hooks.NewRegistry()
		r.SetTrusted(false)
		if r.IsTrusted() {
			t.Fatal("expected IsTrusted() == false after SetTrusted(false)")
		}
	})

	t.Run("SetTrusted true restores trust", func(t *testing.T) {
		r := hooks.NewRegistry()
		r.SetTrusted(false)
		r.SetTrusted(true)
		if !r.IsTrusted() {
			t.Fatal("expected IsTrusted() == true after SetTrusted(true)")
		}
	})

	t.Run("nil registry IsTrusted returns false", func(t *testing.T) {
		var r *hooks.Registry
		if r.IsTrusted() {
			t.Fatal("nil registry should return false from IsTrusted()")
		}
	})

	t.Run("SetTrusted on nil registry is safe", func(t *testing.T) {
		var r *hooks.Registry
		// should not panic
		r.SetTrusted(false)
	})
}

// TestRegistryGateNotAffectedByTrust 验证 toolPolicyGate 在不信任状态下仍然执行。
func TestRegistryGateNotAffectedByTrust(t *testing.T) {
	ctx := context.Background()
	gateErr := errors.New("gate blocked")

	r := hooks.NewRegistry()
	r.SetTrusted(false)
	r.SetToolPolicyGate(func(_ context.Context, _ *hooks.ToolEvent) error {
		return gateErr
	})

	ev := &hooks.ToolEvent{Stage: hooks.ToolLifecycleBefore}
	err := r.RunToolPolicyGate(ctx, ev)
	if !errors.Is(err, gateErr) {
		t.Fatalf("gate should still run when untrusted, got %v", err)
	}
}
