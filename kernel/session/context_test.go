package session

import (
	"testing"

	"github.com/mossagents/moss/kernel/port"
)

func TestLastNDialogMessages(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleSystem, Content: "sys"},
		{Role: port.RoleUser, Content: "u1"},
		{Role: port.RoleAssistant, Content: "a1"},
		{Role: port.RoleUser, Content: "u2"},
		{Role: port.RoleAssistant, Content: "a2"},
	}
	got := LastNDialogMessages(msgs, 2)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if got[0].Content != "u2" || got[1].Content != "a2" {
		t.Fatalf("unexpected messages: %+v", got)
	}
}

func TestBuildCompactedMessages(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleSystem, Content: "sys1"},
		{Role: port.RoleUser, Content: "u1"},
		{Role: port.RoleAssistant, Content: "a1"},
		{Role: port.RoleSystem, Content: "sys2"},
		{Role: port.RoleUser, Content: "u2"},
	}
	out := BuildCompactedMessages(msgs, 1, "truncated")
	if len(out) != 4 {
		t.Fatalf("len=%d, want 4", len(out))
	}
	if out[0].Content != "sys1" || out[1].Content != "sys2" {
		t.Fatalf("expected preserved system messages, got %+v", out)
	}
	if out[2].Role != port.RoleSystem || out[2].Content != "truncated" {
		t.Fatalf("expected truncate notice at index 2, got %+v", out[2])
	}
	if out[3].Content != "u2" {
		t.Fatalf("expected last dialog message, got %+v", out[3])
	}
}
