package prompting

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
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
		Workspace:          t.TempDir(),
		Trust:              "trusted",
		ModelInstructions:  "model-base",
		Kernel:             k,
		SkillPrompts:       []string{"skill-1"},
		RuntimeNotices:     []string{"notice-1"},
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	idxEnv := strings.Index(out.Prompt, "## Environment")
	idxCaps := strings.Index(out.Prompt, "## Runtime Capabilities")
	idxSkills := strings.Index(out.Prompt, "## Skills")
	idxNotices := strings.Index(out.Prompt, "## Runtime Notices")
	if !(idxEnv >= 0 && idxCaps > idxEnv && idxSkills > idxCaps && idxNotices > idxSkills) {
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
		BaseSource:       "config",
		DynamicSectionID: []string{"environment", "skills"},
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
}
