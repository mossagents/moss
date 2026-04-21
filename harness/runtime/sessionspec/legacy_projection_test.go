package sessionspec

import (
	"testing"

	rpolicy "github.com/mossagents/moss/harness/runtime/policy"
	rprofile "github.com/mossagents/moss/harness/runtime/profile"
	kernelsession "github.com/mossagents/moss/kernel/session"
)

func TestApplyLegacyProfileProjectionPersistsTypedSpec(t *testing.T) {
	cfg, err := ApplyLegacyProfileProjection(kernelsession.SessionConfig{
		Goal:       "ship typed entry projection",
		Mode:       "oneshot",
		TrustLevel: "trusted",
		MaxSteps:   80,
	}, rprofile.ResolvedProfile{
		Name:         "planning",
		TaskMode:     "planning",
		Trust:        "trusted",
		ApprovalMode: "confirm",
		ToolPolicy:   rpolicy.ResolveToolPolicyForWorkspace("", "trusted", "confirm"),
	}, LegacyProjectionInput{
		PromptPack: "coding",
		Provider:   "openai",
		ModelName:  "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("ApplyLegacyProfileProjection() error = %v", err)
	}
	if cfg.SessionSpec == nil || cfg.ResolvedSessionSpec == nil {
		t.Fatalf("expected typed session specs, got %+v", cfg)
	}
	if cfg.SessionSpec.Intent.CollaborationMode != "plan" {
		t.Fatalf("requested collaboration_mode = %q, want plan", cfg.SessionSpec.Intent.CollaborationMode)
	}
	if cfg.ResolvedSessionSpec.Runtime.PermissionProfile != "legacy:planning" {
		t.Fatalf("permission_profile = %q, want legacy:planning", cfg.ResolvedSessionSpec.Runtime.PermissionProfile)
	}
	if cfg.ResolvedSessionSpec.Intent.PromptPack.ID != "coding" {
		t.Fatalf("prompt_pack = %q, want coding", cfg.ResolvedSessionSpec.Intent.PromptPack.ID)
	}
	if cfg.ResolvedSessionSpec.Runtime.ModelProfile == "" {
		t.Fatal("expected synthetic model profile name")
	}
	if cfg.Metadata[kernelsession.MetadataTaskMode] != "plan" {
		t.Fatalf("task_mode metadata = %v, want plan", cfg.Metadata[kernelsession.MetadataTaskMode])
	}
}
