package tui

import (
	"testing"
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
