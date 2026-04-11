package appkit

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
)

func TestBuildDeepAgent_DefaultPreset(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}

	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	tools := k.ToolRegistry().List()
	toolNames := map[string]bool{}
	for _, spec := range tools {
		toolNames[spec.Name()] = true
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

func TestBuildDeepAgent_PersistsExecutionCapabilities(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: t.TempDir(),
		Trust:     "trusted",
	}

	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}
	if k.WorkspaceIsolation() == nil {
		t.Fatal("expected workspace isolation to be configured")
	}

	snapshot, err := runtime.LoadCapabilitySnapshot(runtime.CapabilityStatusPath())
	if err != nil {
		t.Fatalf("LoadCapabilitySnapshot: %v", err)
	}
	want := map[string]bool{
		runtime.CapabilityExecutionWorkspace:      false,
		runtime.CapabilityExecutionExecutor:       false,
		runtime.CapabilityExecutionIsolation:      false,
		runtime.CapabilityExecutionRepoState:      false,
		runtime.CapabilityExecutionPatchApply:     false,
		runtime.CapabilityExecutionPatchRevert:    false,
		runtime.CapabilityExecutionWorktreeStates: false,
	}
	for _, item := range snapshot.Items {
		if _, ok := want[item.Capability]; ok && item.State == "ready" {
			want[item.Capability] = true
		}
	}
	for capability, found := range want {
		if !found {
			t.Fatalf("missing ready execution capability %s in %+v", capability, snapshot.Items)
		}
	}
}

func TestBuildDeepAgent_PlanningProfileEnablesWriteTodos(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
		Profile:   "planning",
	}
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if _, ok := k.ToolRegistry().Get("write_todos"); !ok {
		t.Fatal("expected write_todos tool in planning profile")
	}
}

func TestBuildDeepAgent_DisableGeneralPurpose(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
	}

	disable := false
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, &DeepAgentConfig{
		EnsureGeneralPurpose:     &disable,
		EnableSessionStore:       &disable,
		EnablePersistentMemories: &disable,
		EnableContextOffload:     &disable,
		EnableBootstrapContext:   &disable,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}

	if _, ok := runtime.AgentRegistry(k).Get("general-purpose"); ok {
		t.Fatal("general-purpose should not be auto-created when disabled")
	}
}

func TestBuildDeepAgent_DisableWorkspaceIsolation(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
	}
	disable := false
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, &DeepAgentConfig{
		EnableWorkspaceIsolation: &disable,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}
	if k.WorkspaceIsolation() != nil {
		t.Fatal("workspace isolation should be disabled")
	}
}

func TestBuildDeepAgent_DisableTaskRuntime(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
	}
	disable := false
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, &DeepAgentConfig{
		EnableTaskRuntime: &disable,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}
	if k.TaskRuntime() != nil {
		t.Fatal("task runtime should be disabled")
	}
}

func TestBuildDeepAgent_DisableCheckpointStore(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
	}
	disable := false
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, &DeepAgentConfig{
		EnableCheckpointStore: &disable,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}
	if k.Checkpoints() != nil {
		t.Fatal("checkpoint store should be disabled")
	}
}

func TestBuildDeepAgent_PatchesOrphanToolCalls(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}
	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "x"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.AppendMessage(model.Message{
		Role: model.RoleAssistant,
		ToolCalls: []model.ToolCall{
			{ID: "orphan-1", Name: "run_command", Arguments: json.RawMessage(`{"command":"echo"}`)},
		},
	})
	ev := &hooks.LLMEvent{Session: sess}
	if err := k.Hooks().BeforeLLM.Run(context.Background(), ev); err != nil {
		t.Fatalf("hooks run: %v", err)
	}
	last := sess.Messages[len(sess.Messages)-1]
	if last.Role != model.RoleTool || len(last.ToolResults) != 1 {
		t.Fatalf("expected patched tool message, got %+v", last)
	}
	if last.ToolResults[0].CallID != "orphan-1" || !last.ToolResults[0].IsError {
		t.Fatalf("unexpected patched result %+v", last.ToolResults[0])
	}
	patchText := model.ContentPartsToPlainText(last.ToolResults[0].ContentParts)
	if !strings.Contains(patchText, "missing tool result patched") {
		t.Fatalf("unexpected patch content %q", patchText)
	}
}

func TestBuildDeepAgent_DefaultLLMRetryInjected(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
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

func TestBuildDeepAgent_CustomLLMGovernanceApplied(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}
	enableRetry := true
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, &DeepAgentConfig{
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
		t.Fatalf("BuildDeepAgent: %v", err)
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

func TestBuildDeepAgent_DisableDefaultLLMRetry(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "restricted",
	}
	disableRetry := false
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, &DeepAgentConfig{
		EnableDefaultLLMRetry: &disableRetry,
	})
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}
	kv := reflect.ValueOf(k).Elem()
	loopCfg := kv.FieldByName("loopCfg")
	llmRetry := loopCfg.FieldByName("LLMRetry")
	if llmRetry.FieldByName("MaxRetries").Int() != 0 {
		t.Fatalf("expected default retry disabled, got %d", llmRetry.FieldByName("MaxRetries").Int())
	}
}

func TestDeepAgentApplyOver_zeroValueDoesNotOverrideBase(t *testing.T) {
	base := DeepAgentDefaults()
	overlay := DeepAgentConfig{}

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

func TestDeepAgentApplyOver_nonZeroStringOverridesBase(t *testing.T) {
	base := DeepAgentDefaults()
	overlay := DeepAgentConfig{AppName: "my-app"}

	result := overlay.ApplyOver(base)

	if result.AppName != "my-app" {
		t.Errorf("AppName: want %q got %q", "my-app", result.AppName)
	}
	if result.GeneralPurposeMaxSteps != base.GeneralPurposeMaxSteps {
		t.Errorf("GeneralPurposeMaxSteps modified unexpectedly: %d", result.GeneralPurposeMaxSteps)
	}
}

func TestDeepAgentApplyOver_nilPtrDoesNotOverrideBase(t *testing.T) {
	base := DeepAgentDefaults()
	overlay := DeepAgentConfig{EnableSessionStore: nil}

	result := overlay.ApplyOver(base)

	if result.EnableSessionStore == nil {
		t.Fatal("EnableSessionStore should not be nil after ApplyOver with nil overlay")
	}
	if *result.EnableSessionStore != *base.EnableSessionStore {
		t.Errorf("EnableSessionStore: want %v got %v", *base.EnableSessionStore, *result.EnableSessionStore)
	}
}

func TestDeepAgentApplyOver_nonNilPtrOverridesBase(t *testing.T) {
	base := DeepAgentDefaults()
	f := false
	overlay := DeepAgentConfig{EnableSessionStore: &f}

	result := overlay.ApplyOver(base)

	if result.EnableSessionStore == nil || *result.EnableSessionStore != false {
		t.Errorf("EnableSessionStore: want false got %v", result.EnableSessionStore)
	}
}

func TestDeepAgentApplyOver_maxStepsZeroPreservesBase(t *testing.T) {
	base := DeepAgentDefaults()
	overlay := DeepAgentConfig{GeneralPurposeMaxSteps: 0}

	result := overlay.ApplyOver(base)

	if result.GeneralPurposeMaxSteps != 50 {
		t.Errorf("GeneralPurposeMaxSteps: want 50 got %d", result.GeneralPurposeMaxSteps)
	}
}

func TestDeepAgentApplyOver_maxStepsPositiveOverridesBase(t *testing.T) {
	base := DeepAgentDefaults()
	overlay := DeepAgentConfig{GeneralPurposeMaxSteps: 100}

	result := overlay.ApplyOver(base)

	if result.GeneralPurposeMaxSteps != 100 {
		t.Errorf("GeneralPurposeMaxSteps: want 100 got %d", result.GeneralPurposeMaxSteps)
	}
}

func TestDeepAgentApplyOver_emptySliceDoesNotOverrideBase(t *testing.T) {
	opt := runtime.WithBuiltinTools(false)
	base := DeepAgentConfig{DefaultSetupOptions: []runtime.Option{opt}}
	overlay := DeepAgentConfig{DefaultSetupOptions: nil}

	result := overlay.ApplyOver(base)

	if len(result.DefaultSetupOptions) != 1 {
		t.Errorf("DefaultSetupOptions: want 1 element got %d", len(result.DefaultSetupOptions))
	}
}

func TestDeepAgentApplyOver_nonEmptySliceOverridesBase(t *testing.T) {
	opt1 := runtime.WithBuiltinTools(false)
	opt2 := runtime.WithAgents(false)
	base := DeepAgentConfig{DefaultSetupOptions: []runtime.Option{opt1}}
	overlay := DeepAgentConfig{DefaultSetupOptions: []runtime.Option{opt1, opt2}}

	result := overlay.ApplyOver(base)

	if len(result.DefaultSetupOptions) != 2 {
		t.Errorf("DefaultSetupOptions: want 2 elements got %d", len(result.DefaultSetupOptions))
	}
}

func TestDeepAgentApplyOver_pureFunction_doesNotMutateBase(t *testing.T) {
	base := DeepAgentDefaults()
	origName := base.AppName
	overlay := DeepAgentConfig{AppName: "changed"}

	_ = overlay.ApplyOver(base)

	if base.AppName != origName {
		t.Errorf("base.AppName was mutated: want %q got %q", origName, base.AppName)
	}
}
