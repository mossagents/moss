package session

import (
	"testing"

	"github.com/mossagents/moss/kernel/port"
)

func TestBuildPromptMessagesUsesFragmentsAndCompactedDialogBoundary(t *testing.T) {
	msgs := []port.Message{
		{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("base prompt")}},
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("u1")}},
		{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("a1")}},
		{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("u2")}},
		{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("a2")}},
	}
	state := PromptContextState{
		Version:              1,
		PromptBudget:         200,
		CompactedDialogCount: 2,
		BaselineFragments: []PromptContextFragment{
			NewPromptContextFragment("baseline:0", "baseline", port.RoleSystem, "baseline", "base prompt"),
		},
		StartupFragments: []PromptContextFragment{
			NewPromptContextFragment("startup:session", "startup", port.RoleSystem, "startup", "<startup_session_context>\nsummary\n</startup_session_context>"),
		},
		DynamicFragments: []PromptContextFragment{
			NewPromptContextFragment("context:summary", "summary", port.RoleSystem, "summary", "<context_summary>\nsnapshot\n</context_summary>"),
		},
	}
	out := BuildPromptMessages(msgs, state)
	if len(out) != 5 {
		t.Fatalf("len=%d, want 5", len(out))
	}
	if got := port.ContentPartsToPlainText(out[0].ContentParts); got != "base prompt" {
		t.Fatalf("baseline=%q", got)
	}
	if got := port.ContentPartsToPlainText(out[1].ContentParts); got == "" || got == "base prompt" {
		t.Fatalf("startup fragment missing: %+v", out[1])
	}
	if got := port.ContentPartsToPlainText(out[2].ContentParts); got == "" || got == "base prompt" {
		t.Fatalf("summary fragment missing: %+v", out[2])
	}
	if got := port.ContentPartsToPlainText(out[3].ContentParts); got != "u2" {
		t.Fatalf("first visible dialog=%q, want u2", got)
	}
	if got := port.ContentPartsToPlainText(out[4].ContentParts); got != "a2" {
		t.Fatalf("last visible dialog=%q, want a2", got)
	}
}

func TestComputePromptFragmentDiff(t *testing.T) {
	fragments := []PromptContextFragment{
		NewPromptContextFragment("baseline:0", "baseline", port.RoleSystem, "baseline", "hello"),
		NewPromptContextFragment("context:summary", "summary", port.RoleSystem, "summary", "world"),
	}
	changed, hashes := ComputePromptFragmentDiff(nil, fragments)
	if len(changed) != 2 {
		t.Fatalf("changed=%v", changed)
	}
	changed, _ = ComputePromptFragmentDiff(hashes, fragments[:1])
	if len(changed) != 1 || changed[0] != "context:summary" {
		t.Fatalf("unexpected diff: %v", changed)
	}
}
