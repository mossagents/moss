package runtime

import (
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/session"
	"os"
	"path/filepath"
	"slices"
	"testing"
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

	names, err := ProfileNamesForWorkspace(workspace, appconfig.TrustTrusted)
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

	names, err := ProfileNamesForWorkspace(workspace, appconfig.TrustRestricted)
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
	resolved := ResolvedProfile{
		Name:         "research",
		TaskMode:     "research",
		Trust:        appconfig.TrustTrusted,
		ApprovalMode: "confirm",
		SessionDefaults: appconfig.SessionProfileConfig{
			MaxSteps:  42,
			MaxTokens: 99,
		},
		ToolPolicy: ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "confirm"),
	}

	cfg := ApplyResolvedProfileToSessionConfig(session.SessionConfig{}, resolved)
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

	resolved, err := ResolveProfileForWorkspace(ProfileResolveOptions{
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
	if rule.Name != "git-push" || rule.Match != "git push*" || rule.Access != ToolAccessRequireApproval {
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

	resolved, err := ResolveProfileForWorkspace(ProfileResolveOptions{
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
	if rule.Name != "api-host" || rule.Match != "api.example.com" || rule.Access != ToolAccessRequireApproval || len(rule.Methods) != 1 || rule.Methods[0] != "GET" {
		t.Fatalf("unexpected http rule: %+v", rule)
	}
}

