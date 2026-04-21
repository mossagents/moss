package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSlashCommandModeReportsCurrentMode(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.collaborationMode = "plan"
	updated, _ := m.handleSlashCommand("/mode")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem {
		t.Fatalf("expected system message, got %v", last.kind)
	}
	for _, want := range []string{"Current mode: Plan (plan)", "Usage: /mode <execute|plan|investigate>"} {
		if !strings.Contains(last.content, want) {
			t.Fatalf("mode status missing %q:\n%s", want, last.content)
		}
	}
}

func TestSlashCommandAskReturnsSwitchMsg(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, cmd := m.handleSlashCommand("/ask trace the current architecture")
	if !updated.streaming {
		t.Fatal("expected ask mode switch to mark chat as streaming")
	}
	if cmd == nil {
		t.Fatal("expected switch command")
	}
	msg := cmd()
	switchMsg, ok := msg.(switchModeMsg)
	if !ok {
		t.Fatalf("expected switchModeMsg, got %T", msg)
	}
	if switchMsg.mode != "investigate" {
		t.Fatalf("mode = %q, want investigate", switchMsg.mode)
	}
	if switchMsg.prompt != "trace the current architecture" {
		t.Fatalf("prompt = %q, want investigate prompt", switchMsg.prompt)
	}
}

func TestSlashCommandModeSetReturnsSwitchMsg(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, cmd := m.handleSlashCommand("/mode plan")
	if !updated.streaming {
		t.Fatal("expected mode switch to mark chat as streaming")
	}
	if cmd == nil {
		t.Fatal("expected switch command")
	}
	msg := cmd()
	switchMsg, ok := msg.(switchModeMsg)
	if !ok {
		t.Fatalf("expected switchModeMsg, got %T", msg)
	}
	if switchMsg.mode != "plan" {
		t.Fatalf("mode = %q, want plan", switchMsg.mode)
	}
}

func TestShiftTabCyclesMode(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.collaborationMode = "execute"

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd == nil {
		t.Fatal("expected switch command from Shift+Tab")
	}
	if !updated.streaming {
		t.Fatal("expected streaming state while switching mode")
	}
	if len(updated.messages) != 0 {
		t.Fatalf("expected no immediate switch transcript message, got %+v", updated.messages)
	}
	msg := cmd()
	switchMsg, ok := msg.(switchModeMsg)
	if !ok {
		t.Fatalf("expected switchModeMsg, got %T", msg)
	}
	if switchMsg.mode != "plan" {
		t.Fatalf("expected next mode to be plan, got %q", switchMsg.mode)
	}

	m.collaborationMode = "plan"
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd == nil {
		t.Fatal("expected switch command from Shift+Tab while in plan mode")
	}
	msg = cmd()
	switchMsg, ok = msg.(switchModeMsg)
	if !ok {
		t.Fatalf("expected switchModeMsg, got %T", msg)
	}
	if switchMsg.mode != "investigate" {
		t.Fatalf("expected next mode to be investigate, got %q", switchMsg.mode)
	}

	m.collaborationMode = "investigate"
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd == nil {
		t.Fatal("expected switch command from Shift+Tab while in investigate mode")
	}
	msg = cmd()
	switchMsg, ok = msg.(switchModeMsg)
	if !ok {
		t.Fatalf("expected switchModeMsg, got %T", msg)
	}
	if switchMsg.mode != "execute" {
		t.Fatalf("expected next mode to be execute, got %q", switchMsg.mode)
	}
}

func TestShiftTabDoesNothingWhileStreaming(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.collaborationMode = "execute"
	m.streaming = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd != nil {
		t.Fatal("expected no switch command while streaming")
	}
	if len(updated.messages) != len(m.messages) {
		t.Fatalf("expected no new messages while streaming, got %d -> %d", len(m.messages), len(updated.messages))
	}
}
