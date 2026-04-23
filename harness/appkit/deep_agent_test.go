package appkit

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/harness/capability"
	rprobe "github.com/mossagents/moss/harness/runtime/probe"
	kt "github.com/mossagents/moss/harness/testing"
	"github.com/mossagents/moss/kernel"
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

	reg := harness.SubagentCatalogOf(k)
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

	snapshot, err := capability.LoadCapabilitySnapshot(capability.CapabilityStatusPath())
	if err != nil {
		t.Fatalf("LoadCapabilitySnapshot: %v", err)
	}
	want := map[string]bool{
		rprobe.CapabilityExecutionWorkspace:      false,
		rprobe.CapabilityExecutionIsolation:      false,
		rprobe.CapabilityExecutionRepoState:      false,
		rprobe.CapabilityExecutionPatchApply:     false,
		rprobe.CapabilityExecutionPatchRevert:    false,
		rprobe.CapabilityExecutionWorktreeStates: false,
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

func TestBuildDeepAgent_PlanningProfileEnablesUpdatePlan(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: ".",
		Trust:     "trusted",
	}
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, nil)
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if _, ok := k.ToolRegistry().Get("update_plan"); !ok {
		t.Fatal("expected update_plan tool in planning pack")
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

	if _, ok := harness.SubagentCatalogOf(k).Get("general-purpose"); ok {
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
	k.SetLLM(&kt.MockLLM{
		Responses: []model.CompletionResponse{{
			Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
			StopReason: "end_turn",
			Usage:      model.TokenUsage{TotalTokens: 1},
		}},
	})
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
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
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("continue")}}
	sess.AppendMessage(userMsg)
	if _, err := kernel.CollectRunAgentResult(context.Background(), k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("root"),
		UserContent: &userMsg,
	}); err != nil {
		t.Fatalf("CollectRunAgentResult: %v", err)
	}
	var patched *model.ToolResult
	for i := range sess.Messages {
		for j := range sess.Messages[i].ToolResults {
			tr := &sess.Messages[i].ToolResults[j]
			if tr.CallID == "orphan-1" {
				patched = tr
				break
			}
		}
	}
	if patched == nil {
		t.Fatal("expected patched tool result for orphan-1")
	}
	if !patched.IsError {
		t.Fatalf("unexpected patched result %+v", *patched)
	}
	patchText := model.ContentPartsToPlainText(patched.ContentParts)
	if !strings.Contains(patchText, "missing tool result patched") {
		t.Fatalf("unexpected patch content %q", patchText)
	}
}

func TestBuildDeepAgent_AdditionalFeaturesHonorGovernedPhases(t *testing.T) {
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: t.TempDir(),
		Trust:     "restricted",
	}
	runtimeSeen := false
	k, err := BuildDeepAgent(context.Background(), flags, &io.NoOpIO{}, &DeepAgentConfig{
		AdditionalFeatures: []harness.Feature{
			harness.FeatureFunc{
				FeatureName: "late-check",
				MetadataValue: harness.FeatureMetadata{
					Phase: harness.FeaturePhasePostRuntime,
				},
				InstallFunc: func(_ context.Context, h *harness.Harness) error {
					_, runtimeSeen = h.Kernel().ToolRegistry().Get("read_file")
					if !runtimeSeen {
						return fmt.Errorf("expected runtime tools before post-runtime deep-agent feature")
					}
					return nil
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildDeepAgent: %v", err)
	}
	if k == nil {
		t.Fatal("expected kernel")
	}
	if !runtimeSeen {
		t.Fatal("expected governed post-runtime additional feature to observe runtime setup")
	}
}

func TestBuildDeepAgentFeatures_DefaultPackSequence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	flags := &AppFlags{
		Provider:  "openai",
		Workspace: t.TempDir(),
		Trust:     "restricted",
	}
	features, err := buildDeepAgentFeatures(flags, DeepAgentDefaults())
	if err != nil {
		t.Fatalf("buildDeepAgentFeatures: %v", err)
	}

	want := []string{
		"state-catalog",
		"session-store",
		"context-offload",
		"context-management",
		"checkpointing",
		"task-delegation",
		"persistent-memories",
		"execution-services",
		"planning",
		"bootstrap-context",
		"llm-resilience",
		"runtime-setup",
		"patch-tool-calls",
		"tool-policy",
		"execution-capability-report",
		"general-purpose-agent",
	}
	if got := deepAgentFeatureNames(features); !reflect.DeepEqual(got, want) {
		t.Fatalf("feature sequence mismatch:\n got=%v\nwant=%v", got, want)
	}

	capabilityMeta := deepAgentFeatureMetadata(t, features[len(features)-2])
	if capabilityMeta.Phase != harness.FeaturePhasePostRuntime {
		t.Fatalf("execution-capability-report phase=%q, want %q", capabilityMeta.Phase, harness.FeaturePhasePostRuntime)
	}
	if !reflect.DeepEqual(capabilityMeta.Requires, []string{"execution-services"}) {
		t.Fatalf("execution-capability-report requires=%v, want [execution-services]", capabilityMeta.Requires)
	}

	generalPurposeMeta := deepAgentFeatureMetadata(t, features[len(features)-1])
	if generalPurposeMeta.Phase != harness.FeaturePhasePostRuntime {
		t.Fatalf("general-purpose-agent phase=%q, want %q", generalPurposeMeta.Phase, harness.FeaturePhasePostRuntime)
	}
	if !reflect.DeepEqual(generalPurposeMeta.Requires, []string{"runtime-setup"}) {
		t.Fatalf("general-purpose-agent requires=%v, want [runtime-setup]", generalPurposeMeta.Requires)
	}
}

func TestBuildDeepAgentFeatures_DisableOptionalPacks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	disable := false
	flags := &AppFlags{
		Provider:  "openai",
		Workspace: t.TempDir(),
		Trust:     "trusted",
	}
	features, err := buildDeepAgentFeatures(flags, DeepAgentConfig{
		AppName:                       "moss",
		EnableSessionStore:            &disable,
		EnableCheckpointStore:         &disable,
		EnableTaskRuntime:             &disable,
		EnablePersistentMemories:      &disable,
		EnableContextOffload:          &disable,
		EnableBootstrapContext:        &disable,
		EnsureGeneralPurpose:          &disable,
		EnableDefaultRestrictedPolicy: &disable,
		EnableDefaultLLMRetry:         &disable,
	})
	if err != nil {
		t.Fatalf("buildDeepAgentFeatures: %v", err)
	}

	want := []string{
		"state-catalog",
		"execution-services",
		"planning",
		"runtime-setup",
		"patch-tool-calls",
		"execution-capability-report",
	}
	if got := deepAgentFeatureNames(features); !reflect.DeepEqual(got, want) {
		t.Fatalf("feature sequence mismatch:\n got=%v\nwant=%v", got, want)
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

func TestNewDeepAgentConfig_OptionsComposePresetContract(t *testing.T) {
	retryEnabled := false
	cfg := NewDeepAgentConfig(
		WithDeepAgentAppName("mossresearch"),
		WithDeepAgentGeneralPurposeAgent("research-generalist", "delegate carefully", "Research helper", 120),
		WithDeepAgentDefaultRestrictedPolicy(false),
		WithDeepAgentLLMGovernance(&retryEnabled, &retry.Config{MaxRetries: 4}, &retry.BreakerConfig{MaxFailures: 2}),
		WithDeepAgentRuntimeSetupOptions(
			harness.WithBuiltinTools(false),
			harness.WithAgents(false),
		),
		WithDeepAgentAdditionalFeatures(
			harness.FeatureFunc{FeatureName: "product-feature"},
		),
	)

	if cfg == nil {
		t.Fatal("expected config")
	}
	if cfg.AppName != "mossresearch" {
		t.Fatalf("AppName = %q, want mossresearch", cfg.AppName)
	}
	if cfg.GeneralPurposeName != "research-generalist" {
		t.Fatalf("GeneralPurposeName = %q, want research-generalist", cfg.GeneralPurposeName)
	}
	if cfg.GeneralPurposePrompt != "delegate carefully" {
		t.Fatalf("GeneralPurposePrompt = %q, want delegate carefully", cfg.GeneralPurposePrompt)
	}
	if cfg.GeneralPurposeDesc != "Research helper" {
		t.Fatalf("GeneralPurposeDesc = %q, want Research helper", cfg.GeneralPurposeDesc)
	}
	if cfg.GeneralPurposeMaxSteps != 120 {
		t.Fatalf("GeneralPurposeMaxSteps = %d, want 120", cfg.GeneralPurposeMaxSteps)
	}
	if cfg.EnableDefaultRestrictedPolicy == nil || *cfg.EnableDefaultRestrictedPolicy {
		t.Fatalf("EnableDefaultRestrictedPolicy = %v, want false", cfg.EnableDefaultRestrictedPolicy)
	}
	if cfg.EnableDefaultLLMRetry == nil || *cfg.EnableDefaultLLMRetry {
		t.Fatalf("EnableDefaultLLMRetry = %v, want false", cfg.EnableDefaultLLMRetry)
	}
	if cfg.LLMRetryConfig == nil || cfg.LLMRetryConfig.MaxRetries != 4 {
		t.Fatalf("LLMRetryConfig = %+v, want MaxRetries=4", cfg.LLMRetryConfig)
	}
	if cfg.LLMBreakerConfig == nil || cfg.LLMBreakerConfig.MaxFailures != 2 {
		t.Fatalf("LLMBreakerConfig = %+v, want MaxFailures=2", cfg.LLMBreakerConfig)
	}
	if len(cfg.DefaultSetupOptions) != 2 {
		t.Fatalf("DefaultSetupOptions len = %d, want 2", len(cfg.DefaultSetupOptions))
	}
	if len(cfg.AdditionalFeatures) != 1 || cfg.AdditionalFeatures[0].Name() != "product-feature" {
		t.Fatalf("AdditionalFeatures = %v, want [product-feature]", deepAgentFeatureNames(cfg.AdditionalFeatures))
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
	opt := harness.WithBuiltinTools(false)
	base := DeepAgentConfig{DefaultSetupOptions: []harness.RuntimeSetupOption{opt}}
	overlay := DeepAgentConfig{DefaultSetupOptions: nil}

	result := overlay.ApplyOver(base)

	if len(result.DefaultSetupOptions) != 1 {
		t.Errorf("DefaultSetupOptions: want 1 element got %d", len(result.DefaultSetupOptions))
	}
}

func TestDeepAgentApplyOver_nonEmptySliceOverridesBase(t *testing.T) {
	opt1 := harness.WithBuiltinTools(false)
	opt2 := harness.WithAgents(false)
	base := DeepAgentConfig{DefaultSetupOptions: []harness.RuntimeSetupOption{opt1}}
	overlay := DeepAgentConfig{DefaultSetupOptions: []harness.RuntimeSetupOption{opt1, opt2}}

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

func deepAgentFeatureNames(features []harness.Feature) []string {
	names := make([]string, 0, len(features))
	for _, feature := range features {
		names = append(names, feature.Name())
	}
	return names
}

func deepAgentFeatureMetadata(t *testing.T, feature harness.Feature) harness.FeatureMetadata {
	t.Helper()
	withMetadata, ok := feature.(harness.FeatureWithMetadata)
	if !ok {
		t.Fatalf("feature %q does not expose metadata", feature.Name())
	}
	return withMetadata.Metadata()
}
