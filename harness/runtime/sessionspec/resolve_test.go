package sessionspec

import (
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/permissions"
	"github.com/mossagents/moss/harness/runtime/presets"
	"github.com/mossagents/moss/harness/runtime/promptpacks"
	"github.com/mossagents/moss/kernel/model"
)

func TestResolveAppliesExplicitPresetBeforeWorkspaceDefaults(t *testing.T) {
	resolved, err := Resolve(SessionSpec{
		Origin: OriginRequest{Preset: "code"},
	}, ResolveInput{
		WorkspaceTrust: appconfig.TrustTrusted,
		DefaultRunMode: "interactive",
		WorkspaceDefaults: Defaults{
			PermissionProfile: "read-only",
		},
		Registries: testRegistries(),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Runtime.PermissionProfile != "workspace-write" {
		t.Fatalf("permission profile = %q, want workspace-write", resolved.Runtime.PermissionProfile)
	}
	if resolved.Origin.Preset != "code" {
		t.Fatalf("preset = %q, want code", resolved.Origin.Preset)
	}
}

func TestResolveUsesWorkspaceDefaultsOnlyWhenTrusted(t *testing.T) {
	_, err := Resolve(SessionSpec{}, ResolveInput{
		WorkspaceTrust: appconfig.TrustRestricted,
		DefaultRunMode: "interactive",
		WorkspaceDefaults: Defaults{
			Preset: "code",
		},
		Registries: testRegistries(),
	})
	if err == nil {
		t.Fatal("expected restricted workspace without global defaults to fail")
	}
	resolved, err := Resolve(SessionSpec{}, ResolveInput{
		WorkspaceTrust: appconfig.TrustTrusted,
		DefaultRunMode: "interactive",
		WorkspaceDefaults: Defaults{
			Preset: "code",
		},
		Registries: testRegistries(),
	})
	if err != nil {
		t.Fatalf("Resolve() trusted error = %v", err)
	}
	if resolved.Origin.Preset != "code" {
		t.Fatalf("preset = %q, want code", resolved.Origin.Preset)
	}
}

func TestResolveFailsWhenRunModeMissing(t *testing.T) {
	_, err := Resolve(SessionSpec{}, ResolveInput{
		WorkspaceTrust: appconfig.TrustTrusted,
		Registries:     testRegistries(),
	})
	if err == nil {
		t.Fatal("expected missing run mode to fail")
	}
}

func TestResolveFailsForUnknownCollaborationMode(t *testing.T) {
	_, err := Resolve(SessionSpec{
		Intent:  IntentRequest{CollaborationMode: "surprise"},
		Runtime: RuntimeRequest{RunMode: "interactive"},
	}, ResolveInput{
		WorkspaceTrust: appconfig.TrustTrusted,
		GlobalDefaults: Defaults{
			PromptPack:        "coding",
			PermissionProfile: "workspace-write",
			SessionPolicy:     "deep-work",
			ModelProfile:      "code-default",
		},
		Registries: testRegistries(),
	})
	if err == nil {
		t.Fatal("expected unknown collaboration mode to fail")
	}
}

func testRegistries() Registries {
	return Registries{
		PromptPacks: map[string]promptpacks.Pack{
			"coding": {ID: "coding", Source: "builtin:coding"},
		},
		Presets: map[string]presets.Preset{
			"code": {
				ID:                "code",
				PromptPack:        "coding",
				CollaborationMode: "execute",
				PermissionProfile: "workspace-write",
				SessionPolicy:     "deep-work",
				ModelProfile:      "code-default",
			},
		},
		PermissionProfiles: map[string]permissions.Profile{
			"workspace-write": {Name: "workspace-write", ApprovalPolicy: "confirm"},
			"read-only":       {Name: "read-only", ApprovalPolicy: "read-only"},
		},
		SessionPolicies: map[string]SessionPolicyConfig{
			"deep-work": {MaxSteps: 200, MaxTokens: 120000},
		},
		ModelProfiles: map[string]ModelProfile{
			"code-default": {
				Provider: "openai",
				ModelConfig: model.ModelConfig{
					Model: "gpt-5.4",
				},
			},
		},
	}
}
