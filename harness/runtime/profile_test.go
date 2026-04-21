package runtime

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	rpolicy "github.com/mossagents/moss/harness/runtime/policy"
	rprofile "github.com/mossagents/moss/harness/runtime/profile"
	"github.com/mossagents/moss/kernel/session"
)

func writeProjectConfig(t *testing.T, workspace string, data []byte) {
	t.Helper()
	path := appconfig.DefaultProjectConfigPath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir project config dir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}
}

func TestProfileNamesForWorkspaceIncludesBuiltinsAndConfig(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	appconfig.SetAppName("mosscode")
	t.Cleanup(func() { appconfig.SetAppName("moss") })

	appDir := filepath.Join(home, ".mosscode")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.yaml"), []byte("profiles:\n  custom-global:\n    label: Custom Global\n"), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	writeProjectConfig(t, workspace, []byte("profiles:\n  custom-project:\n    label: Custom Project\n"))

	names, err := rprofile.ProfileNamesForWorkspace(workspace, appconfig.TrustTrusted)
	if err != nil {
		t.Fatalf("ProfileNamesForWorkspace: %v", err)
	}
	for _, want := range []string{"default", "coding", "research", "planning", "readonly", "custom-global", "custom-project"} {
		if !slices.Contains(names, want) {
			t.Fatalf("expected profile %q in %v", want, names)
		}
	}
}

func TestProfileNamesForWorkspaceRestrictedSkipsProjectConfig(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	appconfig.SetAppName("mosscode")
	t.Cleanup(func() { appconfig.SetAppName("moss") })

	appDir := filepath.Join(home, ".mosscode")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.yaml"), []byte("profiles:\n  custom-global:\n    label: Custom Global\n"), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	writeProjectConfig(t, workspace, []byte("profiles:\n  custom-project:\n    label: Custom Project\n"))

	names, err := rprofile.ProfileNamesForWorkspace(workspace, appconfig.TrustRestricted)
	if err != nil {
		t.Fatalf("ProfileNamesForWorkspace: %v", err)
	}
	if slices.Contains(names, "custom-project") {
		t.Fatalf("restricted profile list should not load project config: %v", names)
	}
	if !slices.Contains(names, "custom-global") {
		t.Fatalf("expected global profile in %v", names)
	}
}

func TestApplyResolvedProfileToSessionConfigPersistsMetadata(t *testing.T) {
	resolved := rprofile.ResolvedProfile{
		Name:         "research",
		TaskMode:     "research",
		Trust:        appconfig.TrustTrusted,
		ApprovalMode: "confirm",
		SessionDefaults: appconfig.SessionProfileConfig{
			MaxSteps:  42,
			MaxTokens: 99,
		},
		ToolPolicy: rpolicy.ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "confirm"),
	}

	cfg := rprofile.ApplyResolvedProfileToSessionConfig(session.SessionConfig{}, resolved)
	if cfg.Profile != "research" {
		t.Fatalf("profile = %q, want research", cfg.Profile)
	}
	if cfg.MaxSteps != 42 || cfg.MaxTokens != 99 {
		t.Fatalf("session defaults not applied: steps=%d tokens=%d", cfg.MaxSteps, cfg.MaxTokens)
	}
	if got := cfg.Metadata[session.MetadataTaskMode]; got != "research" {
		t.Fatalf("task mode metadata = %v", got)
	}
	if got := cfg.Metadata[session.MetadataEffectiveApproval]; got != "confirm" {
		t.Fatalf("approval metadata = %v", got)
	}
	if cfg.SessionSpec == nil || cfg.ResolvedSessionSpec == nil {
		t.Fatalf("expected typed session spec projection, got %+v", cfg)
	}
	if cfg.ResolvedSessionSpec.Intent.CollaborationMode != "investigate" {
		t.Fatalf("collaboration_mode = %q, want investigate", cfg.ResolvedSessionSpec.Intent.CollaborationMode)
	}
	if cfg.ResolvedSessionSpec.Runtime.PermissionProfile != "legacy:research" {
		t.Fatalf("permission_profile = %q, want legacy:research", cfg.ResolvedSessionSpec.Runtime.PermissionProfile)
	}
	if _, ok := cfg.Metadata[session.MetadataToolPolicy]; !ok {
		t.Fatal("expected tool policy metadata")
	}
	if _, ok := cfg.Metadata[session.MetadataToolPolicySummary]; !ok {
		t.Fatal("expected tool policy summary metadata")
	}
}

func TestResolveProfileForWorkspaceAppliesCommandRules(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	appconfig.SetAppName("mosscode")
	t.Cleanup(func() { appconfig.SetAppName("moss") })

	appDir := filepath.Join(home, ".mosscode")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	configData := []byte("profiles:\n  guarded:\n    execution:\n      command_rules:\n        - name: git-push\n          match: \"git push*\"\n          access: require-approval\n")
	writeProjectConfig(t, workspace, configData)

	resolved, err := rprofile.ResolveProfileForWorkspace(rprofile.ProfileResolveOptions{
		Workspace:        workspace,
		RequestedProfile: "guarded",
	})
	if err != nil {
		t.Fatalf("ResolveProfileForWorkspace: %v", err)
	}
	if len(resolved.ToolPolicy.Command.Rules) != 1 {
		t.Fatalf("expected 1 command rule, got %d", len(resolved.ToolPolicy.Command.Rules))
	}
	rule := resolved.ToolPolicy.Command.Rules[0]
	if rule.Name != "git-push" || rule.Match != "git push*" || rule.Access != rpolicy.ToolAccessRequireApproval {
		t.Fatalf("unexpected command rule: %+v", rule)
	}
}

func TestResolveProfileForWorkspaceAppliesHTTPRules(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	appconfig.SetAppName("mosscode")
	t.Cleanup(func() { appconfig.SetAppName("moss") })

	appDir := filepath.Join(home, ".mosscode")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	configData := []byte("profiles:\n  guarded:\n    execution:\n      http_rules:\n        - name: api-host\n          match: \"api.example.com\"\n          methods: [GET]\n          access: require-approval\n")
	writeProjectConfig(t, workspace, configData)

	resolved, err := rprofile.ResolveProfileForWorkspace(rprofile.ProfileResolveOptions{
		Workspace:        workspace,
		RequestedProfile: "guarded",
	})
	if err != nil {
		t.Fatalf("ResolveProfileForWorkspace: %v", err)
	}
	if len(resolved.ToolPolicy.HTTP.Rules) != 1 {
		t.Fatalf("expected 1 http rule, got %d", len(resolved.ToolPolicy.HTTP.Rules))
	}
	rule := resolved.ToolPolicy.HTTP.Rules[0]
	if rule.Name != "api-host" || rule.Match != "api.example.com" || rule.Access != rpolicy.ToolAccessRequireApproval || len(rule.Methods) != 1 || rule.Methods[0] != "GET" {
		t.Fatalf("unexpected http rule: %+v", rule)
	}
}

func TestResolveSessionPostureForWorkspaceUsesResolvedProfile(t *testing.T) {
	posture, resolved, err := rprofile.ResolveSessionPostureForWorkspace(rprofile.ProfileResolveOptions{
		RequestedProfile: "coding",
		Trust:            appconfig.TrustRestricted,
		ApprovalMode:     "full",
	})
	if err != nil {
		t.Fatalf("ResolveSessionPostureForWorkspace: %v", err)
	}
	if posture.Profile != resolved.Name {
		t.Fatalf("posture.Profile = %q, want %q", posture.Profile, resolved.Name)
	}
	if posture.EffectiveTrust != resolved.Trust {
		t.Fatalf("posture.EffectiveTrust = %q, want %q", posture.EffectiveTrust, resolved.Trust)
	}
	if posture.EffectiveApproval != resolved.ApprovalMode {
		t.Fatalf("posture.EffectiveApproval = %q, want %q", posture.EffectiveApproval, resolved.ApprovalMode)
	}
	if !posture.HasToolPolicy {
		t.Fatal("expected resolved posture to include tool policy")
	}
	if posture.ToolPolicy.ApprovalMode != "full-auto" {
		t.Fatalf("posture.ToolPolicy.ApprovalMode = %q, want full-auto", posture.ToolPolicy.ApprovalMode)
	}
}

func TestSessionPostureFromSessionUsesResolvedSessionSpec(t *testing.T) {
	posture := rprofile.SessionPostureFromSession(&session.Session{
		Config: session.SessionConfig{
			ResolvedSessionSpec: &session.ResolvedSessionSpec{
				Workspace: session.ResolvedWorkspace{Trust: appconfig.TrustTrusted},
				Intent:    session.ResolvedIntent{CollaborationMode: "plan"},
				Runtime: session.ResolvedRuntime{
					PermissionProfile: "workspace-write",
					PermissionPolicy:  []byte(`{"Name":"workspace-write","Trust":"trusted","Policy":{"trust":"trusted","approval_mode":"confirm","command":{"access":"allow"},"http":{"access":"allow"},"workspace_write_access":"allow","memory_write_access":"allow","graph_mutation_access":"allow"}}`),
				},
				Origin: session.ResolvedOrigin{Preset: "plan-safe"},
			},
		},
	})
	if posture.Profile != "plan-safe" {
		t.Fatalf("posture.Profile = %q, want plan-safe", posture.Profile)
	}
	if posture.TaskMode != "plan" {
		t.Fatalf("posture.TaskMode = %q, want plan", posture.TaskMode)
	}
	if posture.EffectiveTrust != appconfig.TrustTrusted {
		t.Fatalf("posture.EffectiveTrust = %q, want %q", posture.EffectiveTrust, appconfig.TrustTrusted)
	}
	if posture.EffectiveApproval != "confirm" {
		t.Fatalf("posture.EffectiveApproval = %q, want confirm", posture.EffectiveApproval)
	}
	if !posture.HasToolPolicy {
		t.Fatal("expected posture to include resolved tool policy")
	}
}
