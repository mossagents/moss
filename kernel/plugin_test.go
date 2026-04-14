package kernel

import (
	"context"
	"testing"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

func TestPlugin_InstallLifecycleHandlers(t *testing.T) {
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
		OnSessionLifecycle: func(ctx context.Context, ev *session.LifecycleEvent) error {
			called = append(called, "session-lifecycle")
			return nil
		},
		OnToolLifecycle: func(ctx context.Context, ev *hooks.ToolEvent) error {
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
	reg.OnSessionLifecycle.Run(ctx, &session.LifecycleEvent{Stage: session.LifecycleStarted})
	reg.OnToolLifecycle.Run(ctx, &hooks.ToolEvent{Stage: hooks.ToolLifecycleBefore})
	reg.OnError.Run(ctx, &hooks.ErrorEvent{})

	want := []string{
		"before-llm", "after-llm",
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

func TestPlugin_InterceptorWrapsHook(t *testing.T) {
	reg := hooks.NewRegistry()
	var order []string

	installPlugin(reg, Plugin{
		Name:  "logger-like",
		Order: 10,
		BeforeLLMInterceptor: func(ctx context.Context, ev *hooks.LLMEvent, next func(context.Context) error) error {
			order = append(order, "interceptor-before")
			if err := next(ctx); err != nil {
				return err
			}
			order = append(order, "interceptor-after")
			return nil
		},
		BeforeLLM: func(ctx context.Context, ev *hooks.LLMEvent) error {
			order = append(order, "hook")
			return nil
		},
	})

	reg.BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})

	want := []string{"interceptor-before", "hook", "interceptor-after"}
	for i, item := range want {
		if len(order) <= i || order[i] != item {
			t.Fatalf("execution order = %v, want %v", order, want)
		}
	}
}

func TestPlugin_RequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for unnamed plugin")
		}
	}()

	installPlugin(hooks.NewRegistry(), Plugin{
		BeforeLLM: func(ctx context.Context, ev *hooks.LLMEvent) error { return nil },
	})
}

func TestWithPlugin_InvalidPluginPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid plugin")
		}
	}()

	New(WithPlugin(Plugin{Name: "invalid"}))
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

	k.chain.BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})
	if !called {
		t.Fatal("plugin hook not called")
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

	k.chain.BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})
	if !called {
		t.Fatal("dynamically installed plugin hook not called")
	}
}

func TestKernel_InstallPluginPanicsAfterRunStarted(t *testing.T) {
	k := New()
	_, runID, err := k.runs.begin(context.Background(), "sess")
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	defer k.runs.end(runID)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when installing plugin after run start")
		}
	}()

	k.InstallPlugin(Plugin{
		Name:      "late",
		BeforeLLM: func(ctx context.Context, ev *hooks.LLMEvent) error { return nil },
	})
}

func TestNewLLMAgent_InstallsPlugins(t *testing.T) {
	agent := NewLLMAgent(LLMAgentConfig{
		Name: "plugin-agent",
		Plugins: []Plugin{{
			Name: "before",
			BeforeLLM: func(ctx context.Context, ev *hooks.LLMEvent) error {
				ev.Session.ReplaceMessages([]model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("installed")}}})
				return nil
			},
		}},
	})

	sess := &session.Session{ID: "test"}
	if err := agent.hooks.BeforeLLM.Run(context.Background(), &hooks.LLMEvent{Session: sess}); err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if got := model.ContentPartsToPlainText(sess.CopyMessages()[0].ContentParts); got != "installed" {
		t.Fatalf("message content = %q, want installed", got)
	}
}

func TestKernel_WithPolicy_UsesPlugin(t *testing.T) {
	k := New()
	// WithPolicy should install an OnToolLifecycle hook via Plugin.
	// Just verify the pipeline is non-empty.
	k.WithPolicy()
	if k.chain.OnToolLifecycle.Empty() {
		t.Fatal("WithPolicy should install OnToolLifecycle hook")
	}
}

func TestKernel_OnEvent_UsesPlugin(t *testing.T) {
	k := New()
	var received bool
	k.OnEvent("*", func(ev builtins.Event) {
		received = true
	})
	// Run a session lifecycle event to trigger event emission.
	sess := &session.Session{ID: "test"}
	k.chain.OnSessionLifecycle.Run(context.Background(), &session.LifecycleEvent{Stage: session.LifecycleStarted, Session: sess})
	if !received {
		t.Fatal("OnEvent handler not triggered")
	}
}
