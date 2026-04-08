package prompting

import (
	"context"
	"encoding/json"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompose_PriorityConfigOverSessionOverModel(t *testing.T) {
	k := kernel.New(kernel.WithToolRegistry(tool.NewRegistry()))
	out, err := Compose(ComposeInput{
		Workspace:           t.TempDir(),
		Trust:               "trusted",
		ConfigInstructions:  "config-base",
		SessionInstructions: "session-base",
		ModelInstructions:   "model-base",
		Kernel:              k,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if !strings.HasPrefix(out.Prompt, "config-base") {
		t.Fatalf("expected config base first, got: %s", out.Prompt)
	}
	if out.DebugMeta.BaseSource != "config" {
		t.Fatalf("base source = %q, want config", out.DebugMeta.BaseSource)
	}
	if out.Graph.BaseSource != "config" {
		t.Fatalf("graph base source = %q, want config", out.Graph.BaseSource)
	}
	if layer := findLayer(t, out.Graph, "base_session"); layer.Enabled || layer.SuppressionReason != "lower_priority_source" {
		t.Fatalf("expected base_session suppressed by precedence, got %+v", layer)
	}
}

func TestCompose_DynamicSectionOrder(t *testing.T) {
	k := kernel.New(kernel.WithToolRegistry(tool.NewRegistry()))
	if err := k.ToolRegistry().Register(tool.ToolSpec{
		Name:        "z_tool",
		Description: "z tool",
		Risk:        tool.RiskLow,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) { return nil, nil }); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	out, err := Compose(ComposeInput{
		Workspace:         t.TempDir(),
		Trust:             "trusted",
		ModelInstructions: "model-base",
		Kernel:            k,
		ProfileName:       "coding",
		TaskMode:          "coding",
		SkillPrompts:      []string{"skill-1"},
		RuntimeNotices:    []string{"notice-1"},
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	idxEnv := strings.Index(out.Prompt, "## Environment")
	idxCaps := strings.Index(out.Prompt, "## Runtime Capabilities")
	idxMode := strings.Index(out.Prompt, "## Operating Mode")
	idxSkills := strings.Index(out.Prompt, "## Skills")
	idxNotices := strings.Index(out.Prompt, "## Runtime Notices")
	if !(idxEnv >= 0 && idxCaps > idxEnv && idxMode > idxCaps && idxSkills > idxMode && idxNotices > idxSkills) {
		t.Fatalf("unexpected section order:\n%s", out.Prompt)
	}
}

func TestCompose_BootstrapSectionHeading(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("Be precise."), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	k := kernel.New(kernel.WithToolRegistry(tool.NewRegistry()))
	out, err := Compose(ComposeInput{
		Workspace:         ws,
		Trust:             "trusted",
		ModelInstructions: "model-base",
		Kernel:            k,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if !strings.Contains(out.Prompt, "## Bootstrap Context") {
		t.Fatalf("expected bootstrap heading, got: %s", out.Prompt)
	}
}

func TestCompose_NoDuplicateEnvironmentHeading(t *testing.T) {
	k := kernel.New(kernel.WithToolRegistry(tool.NewRegistry()))
	out, err := Compose(ComposeInput{
		Workspace:          t.TempDir(),
		Trust:              "trusted",
		ConfigInstructions: "## Environment\n- custom: yes",
		Kernel:             k,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if strings.Count(strings.ToLower(out.Prompt), "## environment") != 1 {
		t.Fatalf("expected single environment heading, got: %s", out.Prompt)
	}
	layer := findLayer(t, out.Graph, "environment")
	if layer.Enabled || layer.SuppressionReason != "duplicate_heading" {
		t.Fatalf("expected environment layer suppressed by duplicate heading, got %+v", layer)
	}
}

func TestSessionInstructionsFromMetadata_TypeValidation(t *testing.T) {
	_, err := SessionInstructionsFromMetadata(map[string]any{
		MetadataSessionInstructionsKey: 123,
	})
	if err == nil {
		t.Fatal("expected type validation error")
	}
}

func TestAttachComposeDebugMeta(t *testing.T) {
	meta := AttachComposeDebugMeta(nil, ComposeDebugMeta{
		BaseSource:         "config",
		DynamicSectionID:   []string{"environment", "skills"},
		EnabledLayers:      []string{"base_config", "environment", "skills"},
		SuppressedLayers:   []string{"runtime_notices"},
		SourceChain:        []string{"base:config", "dynamic:environment", "dynamic:skills"},
		InstructionProfile: "planning",
		SuppressionReasons: map[string]string{
			"runtime_notices": "empty_content",
		},
		LayerTokenEstimates: map[string]int{
			"base_config":     10,
			"environment":     6,
			"skills":          4,
			"runtime_notices": 0,
		},
	})
	if got, _ := meta[MetadataBaseSourceKey].(string); got != "config" {
		t.Fatalf("base source = %q", got)
	}
	if got, _ := meta[MetadataDynamicSectionsKey].(string); got != "environment,skills" {
		t.Fatalf("dynamic sections = %q", got)
	}
	if got, _ := meta[MetadataSourceChainKey].(string); got == "" {
		t.Fatal("expected source chain")
	}
	if got, ok := meta[MetadataEnabledLayersKey].([]string); !ok || len(got) != 3 {
		t.Fatalf("enabled layers = %#v", meta[MetadataEnabledLayersKey])
	}
	if got, ok := meta[MetadataSuppressionReasonsKey].(map[string]string); !ok || got["runtime_notices"] != "empty_content" {
		t.Fatalf("suppression reasons = %#v", meta[MetadataSuppressionReasonsKey])
	}
	if got, _ := meta[MetadataInstructionProfileKey].(string); got != "planning" {
		t.Fatalf("instruction profile = %q", got)
	}
}

func TestAttachComposeDebugMeta_ClearsStaleKeysAndRebuildsSourceChain(t *testing.T) {
	meta := map[string]any{
		MetadataBaseSourceKey:         "session",
		MetadataDynamicSectionsKey:    "skills",
		MetadataEnabledLayersKey:      []string{"base_session", "skills"},
		MetadataSuppressedLayersKey:   []string{"environment"},
		MetadataSuppressionReasonsKey: map[string]string{"environment": "duplicate_heading"},
		MetadataLayerTokensKey:        map[string]int{"skills": 4},
		MetadataSourceChainKey:        "base:session -> dynamic:skills",
		MetadataInstructionProfileKey: "research",
	}
	meta = AttachComposeDebugMeta(meta, ComposeDebugMeta{
		BaseSource:       "config",
		DynamicSectionID: []string{"environment"},
		EnabledLayers:    []string{"base_config", "environment"},
	})
	if got, _ := meta[MetadataBaseSourceKey].(string); got != "config" {
		t.Fatalf("base source = %q", got)
	}
	if got, _ := meta[MetadataDynamicSectionsKey].(string); got != "environment" {
		t.Fatalf("dynamic sections = %q", got)
	}
	if _, ok := meta[MetadataSuppressedLayersKey]; ok {
		t.Fatalf("expected stale suppressed layers removed, got %#v", meta[MetadataSuppressedLayersKey])
	}
	if got, _ := meta[MetadataSourceChainKey].(string); got != "base:config -> dynamic:environment" {
		t.Fatalf("source chain = %q", got)
	}
}

func TestComposeDebugMetaFromMetadata(t *testing.T) {
	debug, err := ComposeDebugMetaFromMetadata(map[string]any{
		MetadataBaseSourceKey:         "config",
		MetadataDynamicSectionsKey:    "environment,skills",
		MetadataEnabledLayersKey:      []string{"base_config", "environment"},
		MetadataSuppressedLayersKey:   []any{"skills"},
		MetadataSuppressionReasonsKey: map[string]any{"skills": "empty_content"},
		MetadataLayerTokensKey:        map[string]any{"base_config": 12.0, "environment": 6},
		MetadataSourceChainKey:        "base:config -> dynamic:environment",
		MetadataInstructionProfileKey: "planning",
	})
	if err != nil {
		t.Fatalf("ComposeDebugMetaFromMetadata: %v", err)
	}
	if got, want := debug.BaseSource, "config"; got != want {
		t.Fatalf("base source = %q, want %q", got, want)
	}
	if got := strings.Join(debug.DynamicSectionID, ","); got != "environment,skills" {
		t.Fatalf("dynamic sections = %q", got)
	}
	if got := debug.SuppressionReasons["skills"]; got != "empty_content" {
		t.Fatalf("suppression reason = %q", got)
	}
	if got := debug.LayerTokenEstimates["base_config"]; got != 12 {
		t.Fatalf("base token estimate = %d", got)
	}
}

func TestCompose_ProfileTaskModeSection(t *testing.T) {
	k := kernel.New(kernel.WithToolRegistry(tool.NewRegistry()))
	out, err := Compose(ComposeInput{
		Workspace:         t.TempDir(),
		Trust:             "trusted",
		ModelInstructions: "base",
		ProfileName:       "research",
		TaskMode:          "research",
		Kernel:            k,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if !strings.Contains(out.Prompt, "## Operating Mode") ||
		!strings.Contains(out.Prompt, "Active profile: research") ||
		!strings.Contains(out.Prompt, "Task mode: research") {
		t.Fatalf("missing operating mode section:\n%s", out.Prompt)
	}
	if out.Graph.Profile.ID != "research" {
		t.Fatalf("profile id = %q, want research", out.Graph.Profile.ID)
	}
}

func TestCompose_DebugMetaTracksEnabledAndSuppressedLayers(t *testing.T) {
	k := kernel.New(kernel.WithToolRegistry(tool.NewRegistry()))
	out, err := Compose(ComposeInput{
		Workspace:          t.TempDir(),
		Trust:              "trusted",
		ConfigInstructions: "## Environment\n- custom: yes",
		ProfileName:        "planner",
		TaskMode:           "planning",
		Kernel:             k,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if out.DebugMeta.InstructionProfile != "planning" {
		t.Fatalf("instruction profile = %q", out.DebugMeta.InstructionProfile)
	}
	if got := out.DebugMeta.SuppressionReasons["environment"]; got != "duplicate_heading" {
		t.Fatalf("environment suppression reason = %q", got)
	}
	if _, ok := out.DebugMeta.LayerTokenEstimates["base_config"]; !ok {
		t.Fatalf("expected base_config token estimate, got %+v", out.DebugMeta.LayerTokenEstimates)
	}
	if !strings.Contains(strings.Join(out.DebugMeta.SourceChain, " -> "), "base:config") {
		t.Fatalf("source chain = %v", out.DebugMeta.SourceChain)
	}
}

func findLayer(t *testing.T, graph InstructionGraph, id string) InstructionLayer {
	t.Helper()
	for _, layer := range graph.Layers {
		if layer.ID == id {
			return layer
		}
	}
	t.Fatalf("instruction layer %q not found", id)
	return InstructionLayer{}
}
