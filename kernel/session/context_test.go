package session

import (
	mdl "github.com/mossagents/moss/kernel/model"
	"testing"
)

func TestLastNDialogMessages(t *testing.T) {
	msgs := []mdl.Message{
		{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("sys")}},
		{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("u1")}},
		{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("a1")}},
		{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("u2")}},
		{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("a2")}},
	}
	got := LastNDialogMessages(msgs, 2)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if mdl.ContentPartsToPlainText(got[0].ContentParts) != "u2" || mdl.ContentPartsToPlainText(got[1].ContentParts) != "a2" {
		t.Fatalf("unexpected messages: %+v", got)
	}
}

func TestBuildCompactedMessages(t *testing.T) {
	msgs := []mdl.Message{
		{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("sys1")}},
		{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("u1")}},
		{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("a1")}},
		{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("sys2")}},
		{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("u2")}},
	}
	out := BuildCompactedMessages(msgs, 1, "truncated")
	if len(out) != 4 {
		t.Fatalf("len=%d, want 4", len(out))
	}
	if mdl.ContentPartsToPlainText(out[0].ContentParts) != "sys1" || mdl.ContentPartsToPlainText(out[1].ContentParts) != "sys2" {
		t.Fatalf("expected preserved system messages, got %+v", out)
	}
	if out[2].Role != mdl.RoleSystem || mdl.ContentPartsToPlainText(out[2].ContentParts) != "truncated" {
		t.Fatalf("expected truncate notice at index 2, got %+v", out[2])
	}
	if mdl.ContentPartsToPlainText(out[3].ContentParts) != "u2" {
		t.Fatalf("expected last dialog message, got %+v", out[3])
	}
}
