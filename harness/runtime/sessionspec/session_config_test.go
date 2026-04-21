package sessionspec

import (
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	kernelsession "github.com/mossagents/moss/kernel/session"
)

func TestApplyToSessionConfigAndRestore(t *testing.T) {
	resolved, err := Resolve(SessionSpec{
		Goal:   "ship typed session persistence",
		Origin: OriginRequest{Preset: "code"},
	}, ResolveInput{
		WorkspaceTrust: appconfig.TrustTrusted,
		DefaultRunMode: "interactive",
		Registries:     testRegistries(),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	resolved.Prompt.TrustedAugmentationIDs = []string{"workspace:trusted"}
	resolved.Prompt.TrustedAugmentationDigests = []string{"sha256:abc"}
	resolved.Prompt.RenderedPromptVersion = "v1"
	resolved.Prompt.SnapshotRef = "snapshot-1"

	cfg, err := ApplyToSessionConfig(kernelsession.SessionConfig{}, SessionSpec{
		Goal:   "ship typed session persistence",
		Origin: OriginRequest{Preset: "code"},
	}, resolved, &PromptSnapshot{
		Ref:            "snapshot-1",
		Layers:         []ResolvedPromptLayer{{ID: "system/base", Source: "builtin", Content: "You are a coding agent."}},
		RenderedPrompt: "You are a coding agent.",
		Version:        "v1",
	})
	if err != nil {
		t.Fatalf("ApplyToSessionConfig() error = %v", err)
	}
	if cfg.Mode != "interactive" {
		t.Fatalf("mode = %q, want interactive", cfg.Mode)
	}
	if cfg.Profile != "code" {
		t.Fatalf("profile = %q, want code", cfg.Profile)
	}
	if cfg.Metadata[kernelsession.MetadataEffectiveApproval] != "confirm" {
		t.Fatalf("effective approval metadata = %v, want confirm", cfg.Metadata[kernelsession.MetadataEffectiveApproval])
	}

	restoredSpec, ok := SessionSpecFromConfig(cfg)
	if !ok {
		t.Fatal("expected requested session spec to be present")
	}
	if restoredSpec.Origin.Preset != "code" {
		t.Fatalf("restored preset = %q, want code", restoredSpec.Origin.Preset)
	}
	restoredResolved, ok, err := ResolvedSessionSpecFromConfig(cfg)
	if err != nil {
		t.Fatalf("ResolvedSessionSpecFromConfig() error = %v", err)
	}
	if !ok {
		t.Fatal("expected resolved session spec to be present")
	}
	if restoredResolved.Runtime.SessionPolicyName != "deep-work" {
		t.Fatalf("restored session policy = %q, want deep-work", restoredResolved.Runtime.SessionPolicyName)
	}
	if restoredResolved.Intent.PromptPack.ID != "coding" {
		t.Fatalf("restored prompt pack = %q, want coding", restoredResolved.Intent.PromptPack.ID)
	}
	restoredSnapshot, ok := PromptSnapshotFromConfig(cfg)
	if !ok || restoredSnapshot.Ref != "snapshot-1" {
		t.Fatalf("restored snapshot = %+v, want snapshot-1", restoredSnapshot)
	}
}
