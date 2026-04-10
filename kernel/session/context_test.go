package session

import (
	"github.com/mossagents/moss/kernel/model"
	"testing"
)

func TestLastNDialogMessages(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart("sys")}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("u1")}},
		{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("a1")}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("u2")}},
		{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("a2")}},
	}
	got := LastNDialogMessages(msgs, 2)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if model.ContentPartsToPlainText(got[0].ContentParts) != "u2" || model.ContentPartsToPlainText(got[1].ContentParts) != "a2" {
		t.Fatalf("unexpected messages: %+v", got)
	}
}

func TestBuildCompactedMessages(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart("sys1")}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("u1")}},
		{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("a1")}},
		{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart("sys2")}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("u2")}},
	}
	out := BuildCompactedMessages(msgs, 1, "truncated")
	if len(out) != 4 {
		t.Fatalf("len=%d, want 4", len(out))
	}
	if model.ContentPartsToPlainText(out[0].ContentParts) != "sys1" || model.ContentPartsToPlainText(out[1].ContentParts) != "sys2" {
		t.Fatalf("expected preserved system messages, got %+v", out)
	}
	if out[2].Role != model.RoleSystem || model.ContentPartsToPlainText(out[2].ContentParts) != "truncated" {
		t.Fatalf("expected truncate notice at index 2, got %+v", out[2])
	}
	if model.ContentPartsToPlainText(out[3].ContentParts) != "u2" {
		t.Fatalf("expected last dialog message, got %+v", out[3])
	}
}
