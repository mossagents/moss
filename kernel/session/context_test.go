package session

import (
	"testing"

	"github.com/mossagents/moss/kernel/port"
)

func TestLastNDialogMessages(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("sys")}},
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("u1")}},
		{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("a1")}},
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("u2")}},
		{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("a2")}},
	}
	got := LastNDialogMessages(msgs, 2)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if port.ContentPartsToPlainText(got[0].ContentParts) != "u2" || port.ContentPartsToPlainText(got[1].ContentParts) != "a2" {
		t.Fatalf("unexpected messages: %+v", got)
	}
}

func TestBuildCompactedMessages(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("sys1")}},
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("u1")}},
		{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("a1")}},
		{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("sys2")}},
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("u2")}},
	}
	out := BuildCompactedMessages(msgs, 1, "truncated")
	if len(out) != 4 {
		t.Fatalf("len=%d, want 4", len(out))
	}
	if port.ContentPartsToPlainText(out[0].ContentParts) != "sys1" || port.ContentPartsToPlainText(out[1].ContentParts) != "sys2" {
		t.Fatalf("expected preserved system messages, got %+v", out)
	}
	if out[2].Role != port.RoleSystem || port.ContentPartsToPlainText(out[2].ContentParts) != "truncated" {
		t.Fatalf("expected truncate notice at index 2, got %+v", out[2])
	}
	if port.ContentPartsToPlainText(out[3].ContentParts) != "u2" {
		t.Fatalf("expected last dialog message, got %+v", out[3])
	}
}
