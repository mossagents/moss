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
	p := plugin.Plugin{
		Name: "",
		BeforeLLM: func(ctx context.Context, e *hooks.LLMEvent) error { return nil },
	}
	if err := plugin.Validate(p); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidate_NoHooks(t *testing.T) {
	p := plugin.Plugin{Name: "myplugin"}
	if err := plugin.Validate(p); err == nil {
		t.Fatal("expected error for plugin with no hooks")
	}
}

func TestValidate_Valid(t *testing.T) {
	p := plugin.Plugin{
		Name:      "myplugin",
		BeforeLLM: func(ctx context.Context, e *hooks.LLMEvent) error { return nil },
	}
	if err := plugin.Validate(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ValidWithInterceptor(t *testing.T) {
	p := plugin.Plugin{
		Name: "myplugin",
		AfterLLMInterceptor: func(ctx context.Context, e *hooks.LLMEvent, next func(context.Context) error) error {
			return next(ctx)
		},
	}
	if err := plugin.Validate(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstall_BeforeLLMHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.Plugin{
		Name: "test-before-llm",
		BeforeLLM: func(ctx context.Context, e *hooks.LLMEvent) error {
			atomic.AddInt32(&called, 1)
			return nil
		},
	}
	plugin.Install(reg, p)
	reg.BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("BeforeLLM hook was not called")
	}
}

func TestInstall_AfterLLMHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.Plugin{
		Name: "test-after-llm",
		AfterLLM: func(ctx context.Context, e *hooks.LLMEvent) error {
			atomic.AddInt32(&called, 1)
			return nil
		},
	}
	plugin.Install(reg, p)
	reg.AfterLLM.Run(context.Background(), &hooks.LLMEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("AfterLLM hook was not called")
	}
}

func TestInstall_OnToolLifecycleHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.Plugin{
		Name: "test-tool",
		OnToolLifecycle: func(ctx context.Context, e *hooks.ToolEvent) error {
			atomic.AddInt32(&called, 1)
			return nil
		},
	}
	plugin.Install(reg, p)
	reg.OnToolLifecycle.Run(context.Background(), &hooks.ToolEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("OnToolLifecycle hook was not called")
	}
}

func TestInstall_OnSessionLifecycleHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.Plugin{
		Name: "test-session",
		OnSessionLifecycle: func(ctx context.Context, e *session.LifecycleEvent) error {
			atomic.AddInt32(&called, 1)
			return nil
		},
	}
	plugin.Install(reg, p)
	reg.OnSessionLifecycle.Run(context.Background(), &session.LifecycleEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("OnSessionLifecycle hook was not called")
	}
}

func TestInstall_OnErrorHookFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.Plugin{
		Name: "test-error",
		OnError: func(ctx context.Context, e *hooks.ErrorEvent) error {
			atomic.AddInt32(&called, 1)
			return nil
		},
	}
	plugin.Install(reg, p)
	reg.OnError.Run(context.Background(), &hooks.ErrorEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("OnError hook was not called")
	}
}

func TestInstall_InterceptorFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.Plugin{
		Name: "test-interceptor",
		BeforeLLMInterceptor: func(ctx context.Context, e *hooks.LLMEvent, next func(context.Context) error) error {
			atomic.AddInt32(&called, 1)
			return next(ctx)
		},
	}
	plugin.Install(reg, p)
	reg.BeforeLLM.Run(context.Background(), &hooks.LLMEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("BeforeLLMInterceptor was not called")
	}
}

func TestInstall_PanicOnInvalidPlugin(t *testing.T) {
	reg := hooks.NewRegistry()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for invalid plugin")
		}
	}()
	plugin.Install(reg, plugin.Plugin{Name: ""})
}

func TestInstall_AllSessionInterceptorFires(t *testing.T) {
	reg := hooks.NewRegistry()
	var called int32
	p := plugin.Plugin{
		Name: "test-session-interceptor",
		OnSessionLifecycleInterceptor: func(ctx context.Context, e *session.LifecycleEvent, next func(context.Context) error) error {
			atomic.AddInt32(&called, 1)
			return next(ctx)
		},
	}
	plugin.Install(reg, p)
	reg.OnSessionLifecycle.Run(context.Background(), &session.LifecycleEvent{})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("OnSessionLifecycleInterceptor was not called")
	}
}
