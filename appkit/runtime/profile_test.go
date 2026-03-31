package runtime

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/session"
)

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
	if err := os.WriteFile(filepath.Join(workspace, "moss.yaml"), []byte("profiles:\n  custom-project:\n    label: Custom Project\n"), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(workspace, "moss.yaml"), []byte("profiles:\n  custom-project:\n    label: Custom Project\n"), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}

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
		ExecutionPolicy: ResolveExecutionPolicyForWorkspace("", appconfig.TrustTrusted, "confirm"),
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
}
