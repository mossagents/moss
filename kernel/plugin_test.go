package kernel

import (
	"context"
	"testing"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/session"
)

func TestPlugin_InstallHooks(t *testing.T) {
	reg := hooks.NewRegistry()

	var called []string
	p := Plugin{
		Name:  "test-plugin",
		Order: 10,
		BeforeLLM: func(ctx context.Context, ev *hooks.LLMEvent) error {
			called = append(called, "before-llm")
			return nil
		},
		AfterLLM: func(ctx context.Context, ev *hooks.LLMEvent) error {
			called = append(called, "after-llm")
			return nil
		},
		BeforeToolCall: func(ctx context.Context, ev *hooks.ToolEvent) error {
			called = append(called, "before-tool")
			return nil
		},
		AfterToolCall: func(ctx context.Context, ev *hooks.ToolEvent) error {
			called = append(called, "after-tool")
			return nil
		},
		OnSessionStart: func(ctx context.Context, ev *hooks.SessionEvent) error {
			called = append(called, "session-start")
			return nil
		},
		OnSessionEnd: func(ctx context.Context, ev *hooks.SessionEvent) error {
			called = append(called, "session-end")
			return nil
		},
		OnSessionLifecycle: func(ctx context.Context, ev *session.LifecycleEvent) error {
			called = append(called, "session-lifecycle")
			return nil
		},
		OnToolLifecycle: func(ctx context.Context, ev *session.ToolLifecycleEvent) error {
			called = append(called, "tool-lifecycle")
			return nil
		},
		OnError: func(ctx context.Context, ev *hooks.ErrorEvent) error {
			called = append(called, "error")
			return nil
		},
	}
	installPlugin(reg, p)

	ctx := context.Background()
	reg.BeforeLLM.Run(ctx, &hooks.LLMEvent{})
	reg.AfterLLM.Run(ctx, &hooks.LLMEvent{})
	reg.BeforeToolCall.Run(ctx, &hooks.ToolEvent{})
	reg.AfterToolCall.Run(ctx, &hooks.ToolEvent{})
	reg.OnSessionStart.Run(ctx, &hooks.SessionEvent{})
	reg.OnSessionEnd.Run(ctx, &hooks.SessionEvent{})
	reg.OnSessionLifecycle.Run(ctx, &session.LifecycleEvent{})
	reg.OnToolLifecycle.Run(ctx, &session.ToolLifecycleEvent{})
	reg.OnError.Run(ctx, &hooks.ErrorEvent{})

	want := []string{
		"before-llm", "after-llm",
		"before-tool", "after-tool",
		"session-start", "session-end",
		"session-lifecycle", "tool-lifecycle",
		"error",
	}
	if len(called) != len(want) {
		t.Fatalf("expected %d calls, got %d: %v", len(want), len(called), called)
	}
	for i, w := range want {
		if called[i] != w {
			t.Errorf("call[%d] = %q, want %q", i, called[i], w)
		}
	}
}

func TestPlugin_NilFieldsIgnored(t *testing.T) {
	reg := hooks.NewRegistry()

	// Only set BeforeLLM — all others should remain empty.
	p := Plugin{
		Name: "partial",
		BeforeLLM: func(ctx context.Context, ev *hooks.LLMEvent) error {
			return nil
		},
	}
	installPlugin(reg, p)

	if reg.BeforeLLM.Empty() {
		t.Fatal("BeforeLLM should not be empty")
	}
	if !reg.AfterLLM.Empty() {
		t.Fatal("AfterLLM should be empty")
	}
	if !reg.BeforeToolCall.Empty() {
		t.Fatal("BeforeToolCall should be empty")
	}
	if !reg.OnError.Empty() {
		t.Fatal("OnError should be empty")
	}
	if !reg.OnSessionLifecycle.Empty() {
		t.Fatal("OnSessionLifecycle should be empty")
	}
	if !reg.OnToolLifecycle.Empty() {
		t.Fatal("OnToolLifecycle should be empty")
	}
}

func TestPlugin_OrderRespected(t *testing.T) {
	reg := hooks.NewRegistry()

	var order []string
	installPlugin(reg, Plugin{
		Name:  "late",
		Order: 100,
		BeforeLLM: func(ctx context.Context, ev *hooks.LLMEvent) error {
			order = append(order, "late")
			return nil
		},
	})
	installPlugin(reg, Plugin{
		Name:  "early",
		Order: 1,
		BeforeLLM: func(ctx context.Context, ev *hooks.LLMEvent) error {
			order = append(order, "early")
			return nil
		},
	})

	reg.BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})

	if len(order) != 2 || order[0] != "early" || order[1] != "late" {
		t.Fatalf("expected [early, late], got %v", order)
	}
}

func TestWithPlugin_KernelOption(t *testing.T) {
	var called bool
	k := New(
		WithPlugin(Plugin{
			Name: "test",
			BeforeLLM: func(ctx context.Context, ev *hooks.LLMEvent) error {
				called = true
				return nil
			},
		}),
	)

	k.Hooks().BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})
	if !called {
		t.Fatal("plugin hook not called")
	}
}

func TestWithPluginInstaller_KernelOption(t *testing.T) {
	var interceptorCalled bool
	k := New(
		WithPluginInstaller("test-interceptor", func(reg *hooks.Registry) {
			reg.BeforeToolCall.Intercept(func(ctx context.Context, ev *hooks.ToolEvent, next func(context.Context) error) error {
				interceptorCalled = true
				return next(ctx)
			})
		}),
	)

	k.Hooks().BeforeToolCall.Run(context.Background(), &hooks.ToolEvent{})
	if !interceptorCalled {
		t.Fatal("interceptor not called")
	}
}

func TestKernel_InstallPlugin(t *testing.T) {
	k := New()
	var called bool
	k.InstallPlugin(Plugin{
		Name: "dynamic",
		BeforeLLM: func(ctx context.Context, ev *hooks.LLMEvent) error {
			called = true
			return nil
		},
	})

	k.Hooks().BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})
	if !called {
		t.Fatal("dynamically installed plugin hook not called")
	}
}

func TestKernel_InstallHooks(t *testing.T) {
	k := New()
	var called bool
	k.InstallHooks(func(reg *hooks.Registry) {
		reg.AfterLLM.Intercept(func(ctx context.Context, ev *hooks.LLMEvent, next func(context.Context) error) error {
			called = true
			return next(ctx)
		})
	})

	k.Hooks().AfterLLM.Run(context.Background(), &hooks.LLMEvent{})
	if !called {
		t.Fatal("interceptor installed via InstallHooks not called")
	}
}

func TestKernel_WithPolicy_UsesPlugin(t *testing.T) {
	k := New()
	// WithPolicy should install a BeforeToolCall hook via Plugin.
	// Just verify the pipeline is non-empty.
	k.WithPolicy()
	if k.Hooks().BeforeToolCall.Empty() {
		t.Fatal("WithPolicy should install BeforeToolCall hook")
	}
}

func TestKernel_OnEvent_UsesInstallHooks(t *testing.T) {
	k := New()
	var received bool
	k.OnEvent("*", func(ev builtins.Event) {
		received = true
	})
	// Run a session start to trigger event emission.
	sess := &session.Session{ID: "test"}
	k.Hooks().OnSessionStart.Run(context.Background(), &hooks.SessionEvent{Session: sess})
	if !received {
		t.Fatal("OnEvent handler not triggered")
	}
}
