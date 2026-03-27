package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSlashCommandSessionInfo(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.sessionInfoFn = func() string { return "session summary" }
	updated, _ := m.handleSlashCommand("/session")
	if len(updated.messages) == 0 {
		t.Fatal("expected a system message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.content != "session summary" {
		t.Fatalf("unexpected message content: %q", last.content)
	}
}

func TestSlashCommandTasksValidation(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/tasks bad")
	if len(updated.messages) == 0 {
		t.Fatal("expected validation message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError {
		t.Fatalf("expected error kind, got %v", last.kind)
	}
}

func TestSlashCommandTaskCancel(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.taskCancelFn = func(taskID, reason string) (string, error) {
		if taskID != "t1" {
			t.Fatalf("unexpected taskID: %s", taskID)
		}
		return "cancelled", nil
	}
	updated, _ := m.handleSlashCommand("/task cancel t1 because")
	if len(updated.messages) == 0 {
		t.Fatal("expected cancel output message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.content != "cancelled" {
		t.Fatalf("unexpected message content: %q", last.content)
	}
}

func TestCtrlCSingleClearsInput(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.textarea.SetValue("hello")
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(cancelMsg); ok {
			t.Fatal("single ctrl+c should not quit")
		}
	}
	if updated.textarea.Value() != "" {
		t.Fatalf("expected cleared input, got %q", updated.textarea.Value())
	}
}

func TestCtrlCDoubleQuits(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	now = now.Add(200 * time.Millisecond)
	updated2, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command on double ctrl+c")
	}
	if _, ok := cmd().(cancelMsg); !ok {
		t.Fatal("expected cancelMsg on double ctrl+c")
	}
	_ = updated2
}

func TestEscDoubleCancelsRun(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	cancelled := false
	m.cancelRunFn = func() bool {
		cancelled = true
		return true
	}
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	now = now.Add(200 * time.Millisecond)
	updated2, _ := updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Fatal("expected run to be cancelled on double esc")
	}
	last := updated2.messages[len(updated2.messages)-1]
	if last.kind != msgSystem {
		t.Fatalf("expected system message, got %v", last.kind)
	}
}
