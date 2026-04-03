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
	"github.com/mossagents/moss/kernel/retry"
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
		"offload_context", "compact_conversation", "update_task",
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
	if k.Checkpoints() == nil {
		t.Fatal("expected checkpoints to be configured")
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

func TestBuildDeepAgentKernel_PlanningProfileEnablesWriteTodos(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
		Profile:   "planning",
	}
	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if _, _, ok := k.ToolRegistry().Get("write_todos"); !ok {
		t.Fatal("expected write_todos tool in planning profile")
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

func TestBuildDeepAgentKernel_DisableCheckpointStore(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
	}
	disable := false
	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, &DeepAgentConfig{
		EnableCheckpointStore: &disable,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}
	if k.Checkpoints() != nil {
		t.Fatal("checkpoint store should be disabled")
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
	patchText := port.ContentPartsToPlainText(last.ToolResults[0].ContentParts)
	if !strings.Contains(patchText, "missing tool result patched") {
		t.Fatalf("unexpected patch content %q", patchText)
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

func TestBuildDeepAgentKernel_CustomLLMGovernanceApplied(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}
	enableRetry := true
	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, &DeepAgentConfig{
		EnableDefaultLLMRetry: &enableRetry,
		LLMRetryConfig: &retry.Config{
			MaxRetries:   4,
			InitialDelay: 5 * time.Millisecond,
			MaxDelay:     50 * time.Millisecond,
			Multiplier:   1.5,
		},
		LLMBreakerConfig: &retry.BreakerConfig{
			MaxFailures: 2,
			ResetAfter:  200 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}
	kv := reflect.ValueOf(k).Elem()
	loopCfg := kv.FieldByName("loopCfg")
	llmRetry := loopCfg.FieldByName("LLMRetry")
	if llmRetry.FieldByName("MaxRetries").Int() != 4 {
		t.Fatalf("MaxRetries=%d, want 4", llmRetry.FieldByName("MaxRetries").Int())
	}
	if time.Duration(llmRetry.FieldByName("MaxDelay").Int()) != 50*time.Millisecond {
		t.Fatalf("MaxDelay=%v, want %v", time.Duration(llmRetry.FieldByName("MaxDelay").Int()), 50*time.Millisecond)
	}
	if loopCfg.FieldByName("LLMBreaker").IsNil() {
		t.Fatal("expected LLM breaker to be configured")
	}
}

func TestBuildDeepAgentKernel_DisableDefaultLLMRetry(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}
	disableRetry := false
	k, err := BuildDeepAgentKernel(context.Background(), flags, &port.NoOpIO{}, &DeepAgentConfig{
		EnableDefaultLLMRetry: &disableRetry,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgentKernel: %v", err)
	}
	kv := reflect.ValueOf(k).Elem()
	loopCfg := kv.FieldByName("loopCfg")
	llmRetry := loopCfg.FieldByName("LLMRetry")
	if llmRetry.FieldByName("MaxRetries").Int() != 0 {
		t.Fatalf("expected default retry disabled, got %d", llmRetry.FieldByName("MaxRetries").Int())
	}
}

// ---------------------------------------------------------------------------
// DeepAgentConfig.ApplyOver
// ---------------------------------------------------------------------------

func TestApplyOver_zeroValueDoesNotOverrideBase(t *testing.T) {
	base := DefaultDeepAgentConfig()
	overlay := DeepAgentConfig{} // all zero

	result := overlay.ApplyOver(base)

	if result.AppName != base.AppName {
		t.Errorf("AppName: want %q got %q", base.AppName, result.AppName)
	}
	if result.GeneralPurposeMaxSteps != base.GeneralPurposeMaxSteps {
		t.Errorf("GeneralPurposeMaxSteps: want %d got %d", base.GeneralPurposeMaxSteps, result.GeneralPurposeMaxSteps)
	}
	if result.EnableSessionStore == nil || *result.EnableSessionStore != *base.EnableSessionStore {
		t.Errorf("EnableSessionStore: want %v got %v", base.EnableSessionStore, result.EnableSessionStore)
	}
}

func TestApplyOver_nonZeroStringOverridesBase(t *testing.T) {
	base := DefaultDeepAgentConfig()
	overlay := DeepAgentConfig{AppName: "my-app"}

	result := overlay.ApplyOver(base)

	if result.AppName != "my-app" {
		t.Errorf("AppName: want %q got %q", "my-app", result.AppName)
	}
	// unrelated fields must stay from base
	if result.GeneralPurposeMaxSteps != base.GeneralPurposeMaxSteps {
		t.Errorf("GeneralPurposeMaxSteps modified unexpectedly: %d", result.GeneralPurposeMaxSteps)
	}
}

func TestApplyOver_nilPtrDoesNotOverrideBase(t *testing.T) {
	base := DefaultDeepAgentConfig()
	overlay := DeepAgentConfig{EnableSessionStore: nil} // nil ptr

	result := overlay.ApplyOver(base)

	if result.EnableSessionStore == nil {
		t.Fatal("EnableSessionStore should not be nil after ApplyOver with nil overlay")
	}
	if *result.EnableSessionStore != *base.EnableSessionStore {
		t.Errorf("EnableSessionStore: want %v got %v", *base.EnableSessionStore, *result.EnableSessionStore)
	}
}

func TestApplyOver_nonNilPtrOverridesBase(t *testing.T) {
	base := DefaultDeepAgentConfig() // EnableSessionStore = true
	f := false
	overlay := DeepAgentConfig{EnableSessionStore: &f}

	result := overlay.ApplyOver(base)

	if result.EnableSessionStore == nil || *result.EnableSessionStore != false {
		t.Errorf("EnableSessionStore: want false got %v", result.EnableSessionStore)
	}
}

func TestApplyOver_maxStepsZeroPreservesBase(t *testing.T) {
	base := DefaultDeepAgentConfig() // GeneralPurposeMaxSteps = 50
	overlay := DeepAgentConfig{GeneralPurposeMaxSteps: 0}

	result := overlay.ApplyOver(base)

	if result.GeneralPurposeMaxSteps != 50 {
		t.Errorf("GeneralPurposeMaxSteps: want 50 got %d", result.GeneralPurposeMaxSteps)
	}
}

func TestApplyOver_maxStepsPositiveOverridesBase(t *testing.T) {
	base := DefaultDeepAgentConfig() // GeneralPurposeMaxSteps = 50
	overlay := DeepAgentConfig{GeneralPurposeMaxSteps: 100}

	result := overlay.ApplyOver(base)

	if result.GeneralPurposeMaxSteps != 100 {
		t.Errorf("GeneralPurposeMaxSteps: want 100 got %d", result.GeneralPurposeMaxSteps)
	}
}

func TestApplyOver_emptySliceDoesNotOverrideBase(t *testing.T) {
	opt := runtime.WithBuiltinTools(false)
	base := DeepAgentConfig{DefaultSetupOptions: []runtime.Option{opt}}
	overlay := DeepAgentConfig{DefaultSetupOptions: nil} // empty/nil

	result := overlay.ApplyOver(base)

	if len(result.DefaultSetupOptions) != 1 {
		t.Errorf("DefaultSetupOptions: want 1 element got %d", len(result.DefaultSetupOptions))
	}
}

func TestApplyOver_nonEmptySliceOverridesBase(t *testing.T) {
	opt1 := runtime.WithBuiltinTools(false)
	opt2 := runtime.WithAgents(false)
	base := DeepAgentConfig{DefaultSetupOptions: []runtime.Option{opt1}}
	overlay := DeepAgentConfig{DefaultSetupOptions: []runtime.Option{opt1, opt2}}

	result := overlay.ApplyOver(base)

	if len(result.DefaultSetupOptions) != 2 {
		t.Errorf("DefaultSetupOptions: want 2 elements got %d", len(result.DefaultSetupOptions))
	}
}

func TestApplyOver_pureFunction_doesNotMutateBase(t *testing.T) {
	base := DefaultDeepAgentConfig()
	origName := base.AppName
	overlay := DeepAgentConfig{AppName: "changed"}

	_ = overlay.ApplyOver(base)

	// base must be unchanged (ApplyOver is a value receiver returning a new config)
	if base.AppName != origName {
		t.Errorf("base.AppName was mutated: want %q got %q", origName, base.AppName)
	}
}
