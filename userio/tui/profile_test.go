package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	appconfig "github.com/mossagents/moss/config"
)

func TestSlashCommandProfileList(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(workspace, "moss.yaml"), []byte("profiles:\n  sandboxed:\n    label: Sandboxed\n"), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	m := newChatModel("openai", "gpt-4o", workspace)
	m.trust = appconfig.TrustTrusted
	updated, _ := m.handleSlashCommand("/profile list")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem {
		t.Fatalf("expected system message, got %v", last.kind)
	}
	for _, want := range []string{"Available profiles:", "- default", "- sandboxed"} {
		if !strings.Contains(last.content, want) {
			t.Fatalf("profile list missing %q:\n%s", want, last.content)
		}
	}
}

func TestSlashCommandProfileSetReturnsSwitchMsg(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, cmd := m.handleSlashCommand("/profile set research")
	if !updated.streaming {
		t.Fatal("expected profile switch to mark chat as streaming")
	}
	if cmd == nil {
		t.Fatal("expected switch command")
	}
	msg := cmd()
	switchMsg, ok := msg.(switchProfileMsg)
	if !ok {
		t.Fatalf("expected switchProfileMsg, got %T", msg)
	}
	if switchMsg.profile != "research" {
		t.Fatalf("profile = %q, want research", switchMsg.profile)
	}
}

func TestShiftTabCyclesProfile(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	appconfig.SetAppName("mosscode")
	t.Cleanup(func() { appconfig.SetAppName("moss") })

	if err := os.WriteFile(filepath.Join(workspace, "moss.yaml"), []byte("profiles:\n  zzz:\n    label: ZZZ\n"), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	m := newChatModel("openai", "gpt-4o", workspace)
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.trust = appconfig.TrustTrusted
	m.profile = "default"

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd == nil {
		t.Fatal("expected switch command from Shift+Tab")
	}
	if !updated.streaming {
		t.Fatal("expected streaming state while switching profile")
	}
	if !strings.Contains(updated.messages[len(updated.messages)-1].content, "Switching profile to ") {
		t.Fatalf("unexpected switch message: %q", updated.messages[len(updated.messages)-1].content)
	}
	msg := cmd()
	switchMsg, ok := msg.(switchProfileMsg)
	if !ok {
		t.Fatalf("expected switchProfileMsg, got %T", msg)
	}
	if strings.TrimSpace(switchMsg.profile) == "" || strings.EqualFold(switchMsg.profile, "default") {
		t.Fatalf("expected next profile after default, got %q", switchMsg.profile)
	}
}

func TestShiftTabDoesNothingWhileStreaming(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.profile = "default"
	m.streaming = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd != nil {
		t.Fatal("expected no switch command while streaming")
	}
	if len(updated.messages) != len(m.messages) {
		t.Fatalf("expected no new messages while streaming, got %d -> %d", len(m.messages), len(updated.messages))
	}
}
