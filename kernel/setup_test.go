package kernel

import (
	"context"
	"strings"
	"testing"

	"github.com/mossagi/moss/kernel/port"
	kt "github.com/mossagi/moss/kernel/testing"
)

func TestSetupWithDefaults(t *testing.T) {
	mock := &kt.MockLLM{}
	io := &port.NoOpIO{}
	sb := kt.NewMemorySandbox()

	k := New(
		WithLLM(mock),
		WithUserIO(io),
		WithSandbox(sb),
	)

	ctx := context.Background()
	if err := k.SetupWithDefaults(ctx, "."); err != nil {
		t.Fatalf("SetupWithDefaults: %v", err)
	}

	// 验证核心 skill 被加载
	skills := k.SkillManager().List()
	found := false
	for _, s := range skills {
		if s.Name == "core" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected core skill to be registered")
	}

	// 验证内置工具被注册
	tools := k.ToolRegistry().List()
	if len(tools) == 0 {
		t.Error("expected tools to be registered")
	}
	toolNames := make(map[string]bool)
	for _, ts := range tools {
		toolNames[ts.Name] = true
	}
	for _, name := range []string{"read_file", "write_file", "list_files", "search_text", "run_command", "ask_user"} {
		if !toolNames[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

func TestSetupWithDefaults_WithoutBuiltin(t *testing.T) {
	mock := &kt.MockLLM{}
	io := &port.NoOpIO{}

	k := New(
		WithLLM(mock),
		WithUserIO(io),
	)

	ctx := context.Background()
	if err := k.SetupWithDefaults(ctx, ".", WithoutBuiltin()); err != nil {
		t.Fatalf("SetupWithDefaults: %v", err)
	}

	// 核心 skill 不应存在
	skills := k.SkillManager().List()
	for _, s := range skills {
		if s.Name == "core" {
			t.Error("core skill should not be registered when WithoutBuiltin is used")
		}
	}
}

func TestSetupWithDefaults_NoSandbox(t *testing.T) {
	mock := &kt.MockLLM{}
	io := &port.NoOpIO{}

	k := New(
		WithLLM(mock),
		WithUserIO(io),
		// 不设置 Sandbox — 纯对话模式
	)

	ctx := context.Background()
	if err := k.SetupWithDefaults(ctx, "."); err != nil {
		t.Fatalf("SetupWithDefaults: %v", err)
	}

	// 仅 ask_user 应被注册
	tools := k.ToolRegistry().List()
	toolNames := make(map[string]bool)
	for _, ts := range tools {
		toolNames[ts.Name] = true
	}
	if !toolNames["ask_user"] {
		t.Error("expected ask_user to be registered without sandbox")
	}
	for _, name := range []string{"read_file", "write_file", "list_files", "search_text", "run_command"} {
		if toolNames[name] {
			t.Errorf("tool %q should not be registered without sandbox", name)
		}
	}
}

func TestBoot_RequiresLLMAndUserIO(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		wantErr string
	}{
		{
			name:    "no LLM no UserIO",
			opts:    nil,
			wantErr: "LLM port is required",
		},
		{
			name:    "LLM only",
			opts:    []Option{WithLLM(&kt.MockLLM{})},
			wantErr: "UserIO port is not set",
		},
		{
			name: "both set",
			opts: []Option{WithLLM(&kt.MockLLM{}), WithUserIO(&port.NoOpIO{})},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := New(tt.opts...)
			err := k.Boot(context.Background())
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}
