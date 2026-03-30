package appkit

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

func TestBuildDeepAgentKernel_DefaultPreset(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}

	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	tools := k.ToolRegistry().List()
	toolNames := map[string]bool{}
	for _, spec := range tools {
		toolNames[spec.Name] = true
	}
	for _, name := range []string{
		"read_file", "write_file", "edit_file", "glob", "ls", "grep", "run_command", "ask_user",
		"read_memory", "write_memory", "list_memories", "delete_memory",
		"offload_context", "compact_conversation", "write_todos", "update_task",
		"plan_task", "claim_task", "send_mail", "read_mailbox", "acquire_workspace", "release_workspace",
	} {
		if !toolNames[name] {
			t.Fatalf("expected built-in tool %q", name)
		}
	}
	if k.WorkspaceIsolation() == nil {
		t.Fatal("expected workspace isolation to be configured")
	}
	if k.TaskRuntime() == nil {
		t.Fatal("expected task runtime to be configured")
	}
	if k.RepoStateCapture() == nil {
		t.Fatal("expected repo state capture to be configured")
	}
	if k.PatchApply() == nil {
		t.Fatal("expected patch apply to be configured")
	}
	if k.PatchRevert() == nil {
		t.Fatal("expected patch revert to be configured")
	}
	if k.WorktreeSnapshots() == nil {
		t.Fatal("expected worktree snapshots to be configured")
	}

	reg := runtime.AgentRegistry(k)
	gp, ok := reg.Get("general-purpose")
	if !ok {
		t.Fatal("expected general-purpose agent preset")
	}
	if gp.TrustLevel != "restricted" {
		t.Fatalf("general-purpose trust=%q, want restricted", gp.TrustLevel)
	}
	if gp.MaxSteps <= 0 {
		t.Fatalf("general-purpose max_steps=%d, want >0", gp.MaxSteps)
	}
	if len(gp.Tools) == 0 {
		t.Fatal("expected general-purpose tools to be populated")
	}
}

func TestBuildDeepAgentKernel_DisableGeneralPurpose(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
	}

	disable := false
	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, &DeepAgentConfig{
		EnsureGeneralPurpose:     &disable,
		EnableSessionStore:       &disable,
		EnablePersistentMemories: &disable,
		EnableContextOffload:     &disable,
		EnableBootstrapContext:   &disable,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}

	if _, ok := runtime.AgentRegistry(k).Get("general-purpose"); ok {
		t.Fatal("general-purpose should not be auto-created when disabled")
	}
}

func TestBuildDeepAgentKernel_DisableWorkspaceIsolation(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
	}
	disable := false
	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, &DeepAgentConfig{
		EnableWorkspaceIsolation: &disable,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}
	if k.WorkspaceIsolation() != nil {
		t.Fatal("workspace isolation should be disabled")
	}
}

func TestBuildDeepAgentKernel_DisableTaskRuntime(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
	}
	disable := false
	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, &DeepAgentConfig{
		EnableTaskRuntime: &disable,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}
	if k.TaskRuntime() != nil {
		t.Fatal("task runtime should be disabled")
	}
}

func TestBuildDeepAgentKernel_PatchesOrphanToolCalls(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}
	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}
	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "x"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.AppendMessage(port.Message{
		Role: port.RoleAssistant,
		ToolCalls: []port.ToolCall{
			{ID: "orphan-1", Name: "run_command", Arguments: json.RawMessage(`{"command":"echo"}`)},
		},
	})
	mc := &middleware.Context{Session: sess}
	if err := k.Middleware().Run(context.Background(), middleware.BeforeLLM, mc); err != nil {
		t.Fatalf("middleware run: %v", err)
	}
	last := sess.Messages[len(sess.Messages)-1]
	if last.Role != port.RoleTool || len(last.ToolResults) != 1 {
		t.Fatalf("expected patched tool message, got %+v", last)
	}
	if last.ToolResults[0].CallID != "orphan-1" || !last.ToolResults[0].IsError {
		t.Fatalf("unexpected patched result %+v", last.ToolResults[0])
	}
	if !strings.Contains(last.ToolResults[0].Content, "missing tool result patched") {
		t.Fatalf("unexpected patch content %q", last.ToolResults[0].Content)
	}
}

func TestBuildDeepAgentKernel_DefaultLLMRetryInjected(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}
	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}
	kv := reflect.ValueOf(k).Elem()
	loopCfg := kv.FieldByName("loopCfg")
	llmRetry := loopCfg.FieldByName("LLMRetry")
	if llmRetry.FieldByName("MaxRetries").Int() <= 0 {
		t.Fatalf("expected default LLM retry enabled, got %d", llmRetry.FieldByName("MaxRetries").Int())
	}
	if time.Duration(llmRetry.FieldByName("InitialDelay").Int()) <= 0 {
		t.Fatalf("expected positive retry initial delay")
	}
}
