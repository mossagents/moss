package product

import (
	appconfig "github.com/mossagents/moss/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverCustomCommandsPrefersProjectWhenTrusted(t *testing.T) {
	appconfig.SetAppName("mosscode")
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	ws := t.TempDir()
	userDir := filepath.Join(appconfig.AppDir(), "commands")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "review.md"), []byte("User review flow"), 0o600); err != nil {
		t.Fatalf("write user command: %v", err)
	}
	projectDir := filepath.Join(ws, ".mosscode", "commands")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "review.md"), []byte("Project review flow"), 0o600); err != nil {
		t.Fatalf("write project command: %v", err)
	}

	commands, err := DiscoverCustomCommands(ws, "mosscode", appconfig.TrustTrusted)
	if err != nil {
		t.Fatalf("DiscoverCustomCommands: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one merged command, got %d", len(commands))
	}
	if commands[0].Name != "review" || !strings.Contains(commands[0].Prompt, "Project review") || commands[0].Scope != "project" {
		t.Fatalf("unexpected command: %+v", commands[0])
	}
}

func TestDiscoverCustomCommandsSkipsProjectWhenRestricted(t *testing.T) {
	appconfig.SetAppName("mosscode")
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	ws := t.TempDir()
	userDir := filepath.Join(appconfig.AppDir(), "commands")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "review.md"), []byte("User review flow"), 0o600); err != nil {
		t.Fatalf("write user command: %v", err)
	}
	projectDir := filepath.Join(ws, ".mosscode", "commands")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "review.md"), []byte("Project review flow"), 0o600); err != nil {
		t.Fatalf("write project command: %v", err)
	}

	commands, err := DiscoverCustomCommands(ws, "mosscode", appconfig.TrustRestricted)
	if err != nil {
		t.Fatalf("DiscoverCustomCommands: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one user command, got %d", len(commands))
	}
	if !strings.Contains(commands[0].Prompt, "User review") || commands[0].Scope != "user" {
		t.Fatalf("unexpected restricted command: %+v", commands[0])
	}
}

func TestRenderCustomCommandPromptInjectsArgs(t *testing.T) {
	cmd := CustomCommand{Name: "review", Prompt: "Review this repo: {{args}}\nWorkspace: {{workspace}}"}
	got := RenderCustomCommandPrompt(cmd, "focus on tests", "D:\\work")
	if !strings.Contains(got, "focus on tests") || !strings.Contains(got, "D:\\work") {
		t.Fatalf("unexpected rendered prompt: %q", got)
	}
}

func TestInitWorkspaceBootstrapCreatesAgentsAndCommandsDir(t *testing.T) {
	out, err := InitWorkspaceBootstrap(t.TempDir(), "mosscode")
	if err != nil {
		t.Fatalf("InitWorkspaceBootstrap: %v", err)
	}
	if !strings.Contains(out, "Created") || !strings.Contains(out, "commands") {
		t.Fatalf("unexpected init output: %q", out)
	}
}
