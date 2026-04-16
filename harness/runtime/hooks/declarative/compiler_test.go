package declarative

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

func makeToolEvent(name string) *hooks.ToolEvent {
	return &hooks.ToolEvent{
		Stage:    hooks.ToolLifecycleBefore,
		Tool:     &tool.ToolSpec{Name: name, Risk: tool.RiskLow},
		ToolName: name,
		Session:  &session.Session{ID: "test-session"},
		Input:    json.RawMessage(`{"key": "value"}`),
	}
}

func TestCompileHooks_Empty(t *testing.T) {
	plugins, err := CompileHooks(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plugins) != 0 {
		t.Fatalf("expected 0 plugins, got %d", len(plugins))
	}
}

func TestCompileHooks_MissingName(t *testing.T) {
	_, err := CompileHooks([]HookConfig{{Type: HookTypeCommand, Command: "/bin/true"}})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestCompileHooks_UnknownType(t *testing.T) {
	_, err := CompileHooks([]HookConfig{{Name: "test", Type: "unknown"}})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestCompileHooks_CommandMissingCommand(t *testing.T) {
	_, err := CompileHooks([]HookConfig{{Name: "test", Type: HookTypeCommand}})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestCompileHooks_HTTPMissingURL(t *testing.T) {
	_, err := CompileHooks([]HookConfig{{Name: "test", Type: HookTypeHTTP}})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestCompileHooks_PromptMissingPrompt(t *testing.T) {
	_, err := CompileHooks([]HookConfig{{Name: "test", Type: HookTypePrompt}})
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}
}

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern string
		tool    string
		want    bool
	}{
		{"*", "anything", true},
		{"write_*", "write_file", true},
		{"write_*", "read_file", false},
		{"bash", "bash", true},
		{"bash", "python", false},
		{"", "anything", true}, // empty pattern matches all
	}

	for _, tt := range tests {
		cfg := HookConfig{
			Name:    "test",
			Type:    HookTypeCommand,
			Event:   EventPreToolUse,
			Match:   tt.pattern,
			Command: "echo ok",
		}
		called := false
		fn := wrapWithMatch(func(_ context.Context, _ *hooks.ToolEvent) error {
			called = true
			return nil
		}, cfg.Match, cfg.Event)

		ev := makeToolEvent(tt.tool)
		fn(context.Background(), ev)
		if called != tt.want {
			t.Errorf("pattern=%q tool=%q: called=%v, want=%v", tt.pattern, tt.tool, called, tt.want)
		}
	}
}

func TestEventFiltering(t *testing.T) {
	called := false
	fn := wrapWithMatch(func(_ context.Context, _ *hooks.ToolEvent) error {
		called = true
		return nil
	}, "", EventPostToolUse)

	// Pre-tool-use event should NOT trigger post_tool_use hook.
	ev := makeToolEvent("test_tool")
	ev.Stage = hooks.ToolLifecycleBefore
	fn(context.Background(), ev)
	if called {
		t.Fatal("post_tool_use hook should not fire on pre_tool_use event")
	}

	// Post-tool-use event SHOULD trigger.
	ev.Stage = hooks.ToolLifecycleAfter
	fn(context.Background(), ev)
	if !called {
		t.Fatal("post_tool_use hook should fire on post_tool_use event")
	}
}

func TestHTTPHook_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fn := httpHook(HookConfig{
		Name:           "test-http",
		URL:            server.URL,
		Timeout:        5 * time.Second,
		BlockOnFailure: true,
	})

	if err := fn(context.Background(), makeToolEvent("test")); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestHTTPHook_BlockOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	fn := httpHook(HookConfig{
		Name:           "test-http",
		URL:            server.URL,
		Timeout:        5 * time.Second,
		BlockOnFailure: true,
	})

	if err := fn(context.Background(), makeToolEvent("test")); err == nil {
		t.Fatal("expected error for 403 response with block_on_failure")
	}
}

func TestHTTPHook_NoBlockOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	fn := httpHook(HookConfig{
		Name:           "test-http",
		URL:            server.URL,
		Timeout:        5 * time.Second,
		BlockOnFailure: false,
	})

	if err := fn(context.Background(), makeToolEvent("test")); err != nil {
		t.Fatalf("expected nil error with block_on_failure=false, got: %v", err)
	}
}

func TestPromptHook_BlockOnFailure(t *testing.T) {
	fn := promptHook(HookConfig{
		Name:           "safety-check",
		Prompt:         "Is this operation safe?",
		BlockOnFailure: true,
	})

	err := fn(context.Background(), makeToolEvent("write_file"))
	if err == nil {
		t.Fatal("expected error for prompt hook with block_on_failure")
	}
}

func TestPromptHook_NoBlock(t *testing.T) {
	fn := promptHook(HookConfig{
		Name:           "safety-check",
		Prompt:         "Is this operation safe?",
		BlockOnFailure: false,
	})

	if err := fn(context.Background(), makeToolEvent("write_file")); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestCompileHooks_FullIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configs := []HookConfig{
		{
			Name:           "webhook",
			Type:           HookTypeHTTP,
			Event:          EventPreToolUse,
			Match:          "write_*",
			URL:            server.URL,
			Timeout:        5 * time.Second,
			BlockOnFailure: true,
		},
		{
			Name:   "audit-log",
			Type:   HookTypePrompt,
			Event:  EventPostToolUse,
			Prompt: "Was this safe?",
		},
	}

	plugins, err := CompileHooks(configs)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(plugins))
	}
	for _, p := range plugins {
		if p.Name() == "" {
			t.Fatal("plugin should have a name")
		}
		fmt.Printf("compiled plugin: %s\n", p.Name())
	}
}
