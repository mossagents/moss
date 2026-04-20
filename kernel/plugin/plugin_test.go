package plugin_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/plugin"
	"github.com/mossagents/moss/kernel/session"
)

func TestValidate_EmptyName(t *testing.T) {
	p := plugin.BeforeLLMHook("", 0, func(ctx context.Context, e *hooks.LLMEvent) error { return nil })
	if err := plugin.Validate(p); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidate_NoHooks(t *testing.T) {
	p := plugin.NewGroup("myplugin", 0)
	if err := plugin.Validate(p); err == nil {
		t.Fatal("expected error for plugin with no hooks")
	}
}

func TestValidate_Valid(t *testing.T) {
	p := plugin.BeforeLLMHook("myplugin", 0, func(ctx context.Context, e *hooks.LLMEvent) error { return nil })
	if err := plugin.Validate(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ValidWithInterceptor(t *testing.T) {
	p := plugin.AfterLLMInterceptor("myplugin", 0, func(ctx context.Context, e *hooks.LLMEvent, next func(context.Context) error) error {
		return next(ctx)
	})
	if err := plugin.Validate(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstall_BeforeLLMHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.BeforeLLMHook("test-before-llm", 0, func(ctx context.Context, e *hooks.LLMEvent) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	if err := plugin.Install(reg, p); err != nil {
		t.Fatalf("Install: %v", err)
	}
	reg.BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("BeforeLLM hook was not called")
	}
}

func TestInstall_BeforeLLMRequestHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.BeforeLLMRequestHook("test-before-llm-request", 0, func(ctx context.Context, e *hooks.LLMEvent) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	if err := plugin.Install(reg, p); err != nil {
		t.Fatalf("Install: %v", err)
	}
	reg.BeforeLLMRequest.Run(context.Background(), &hooks.LLMEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("BeforeLLMRequest hook was not called")
	}
}

func TestInstall_AfterLLMHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.AfterLLMHook("test-after-llm", 0, func(ctx context.Context, e *hooks.LLMEvent) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	if err := plugin.Install(reg, p); err != nil {
		t.Fatalf("Install: %v", err)
	}
	reg.AfterLLM.Run(context.Background(), &hooks.LLMEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("AfterLLM hook was not called")
	}
}

func TestInstall_OnToolLifecycleHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.ToolLifecycleHook("test-tool", 0, func(ctx context.Context, e *hooks.ToolEvent) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	if err := plugin.Install(reg, p); err != nil {
		t.Fatalf("Install: %v", err)
	}
	reg.OnToolLifecycle.Run(context.Background(), &hooks.ToolEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("OnToolLifecycle hook was not called")
	}
}

func TestInstall_OnSessionLifecycleHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.SessionLifecycleHook("test-session", 0, func(ctx context.Context, e *session.LifecycleEvent) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	if err := plugin.Install(reg, p); err != nil {
		t.Fatalf("Install: %v", err)
	}
	reg.OnSessionLifecycle.Run(context.Background(), &session.LifecycleEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("OnSessionLifecycle hook was not called")
	}
}

func TestInstall_OnErrorHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.ErrorHook("test-error", 0, func(ctx context.Context, e *hooks.ErrorEvent) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	if err := plugin.Install(reg, p); err != nil {
		t.Fatalf("Install: %v", err)
	}
	reg.OnError.Run(context.Background(), &hooks.ErrorEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("OnError hook was not called")
	}
}

func TestInstall_InterceptorFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.BeforeLLMInterceptor("test-interceptor", 0, func(ctx context.Context, e *hooks.LLMEvent, next func(context.Context) error) error {
		atomic.AddInt32(&called, 1)
		return next(ctx)
	})
	if err := plugin.Install(reg, p); err != nil {
		t.Fatalf("Install: %v", err)
	}
	reg.BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("BeforeLLMInterceptor was not called")
	}
}

func TestInstall_BeforeLLMRequestInterceptorFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.BeforeLLMRequestInterceptor("test-request-interceptor", 0, func(ctx context.Context, e *hooks.LLMEvent, next func(context.Context) error) error {
		atomic.AddInt32(&called, 1)
		return next(ctx)
	})
	if err := plugin.Install(reg, p); err != nil {
		t.Fatalf("Install: %v", err)
	}
	reg.BeforeLLMRequest.Run(context.Background(), &hooks.LLMEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("BeforeLLMRequestInterceptor was not called")
	}
}

func TestInstall_ErrorOnInvalidPlugin(t *testing.T) {
	reg := hooks.NewRegistry()
	if err := plugin.Install(reg, plugin.NewGroup("", 0)); err == nil {
		t.Fatal("expected error for invalid plugin")
	}
}

func TestInstall_AllSessionInterceptorFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.SessionLifecycleInterceptor("test-session-interceptor", 0, func(ctx context.Context, e *session.LifecycleEvent, next func(context.Context) error) error {
		atomic.AddInt32(&called, 1)
		return next(ctx)
	})
	if err := plugin.Install(reg, p); err != nil {
		t.Fatalf("Install: %v", err)
	}
	reg.OnSessionLifecycle.Run(context.Background(), &session.LifecycleEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("OnSessionLifecycleInterceptor was not called")
	}
}

func TestGroup_MultipleRegistrations(t *testing.T) {
	reg := hooks.NewRegistry()
	var order []string

	g := plugin.NewGroup("multi", 10)
	g.OnBeforeLLM(func(ctx context.Context, ev *hooks.LLMEvent) error {
		order = append(order, "before-llm")
		return nil
	})
	g.OnAfterLLM(func(ctx context.Context, ev *hooks.LLMEvent) error {
		order = append(order, "after-llm")
		return nil
	})
	g.OnError(func(ctx context.Context, ev *hooks.ErrorEvent) error {
		order = append(order, "error")
		return nil
	})

	if err := plugin.Install(reg, g); err != nil {
		t.Fatalf("Install: %v", err)
	}

	reg.BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})
	reg.AfterLLM.Run(context.Background(), &hooks.LLMEvent{})
	reg.OnError.Run(context.Background(), &hooks.ErrorEvent{})

	want := []string{"before-llm", "after-llm", "error"}
	if len(order) != len(want) {
		t.Fatalf("expected %d calls, got %d: %v", len(want), len(order), order)
	}
	for i, w := range want {
		if order[i] != w {
			t.Errorf("call[%d] = %q, want %q", i, order[i], w)
		}
	}
}

func TestGroup_Empty(t *testing.T) {
	g := plugin.NewGroup("empty", 0)
	if !g.Empty() {
		t.Fatal("new Group should be empty")
	}
	g.OnBeforeLLM(func(ctx context.Context, ev *hooks.LLMEvent) error { return nil })
	if g.Empty() {
		t.Fatal("Group with hook should not be empty")
	}
}
