package session

import (
	mdl "github.com/mossagents/moss/kernel/model"
	"strings"
	"testing"
)

func TestBuildPromptMessagesUsesFragmentsAndCompactedDialogBoundary(t *testing.T) {
	msgs := []mdl.Message{
		{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("base prompt")}},
		{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("u1")}},
		{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("a1")}},
		{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("u2")}},
		{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("a2")}},
	}
	state := PromptContextState{
		Version:              1,
		PromptBudget:         200,
		CompactedDialogCount: 2,
		BaselineFragments: []PromptContextFragment{
			NewPromptContextFragment("baseline:0", "baseline", mdl.RoleSystem, "baseline", "base prompt"),
		},
		StartupFragments: []PromptContextFragment{
			NewPromptContextFragment("startup:session", "startup", mdl.RoleSystem, "startup", "<startup_session_context>\nsummary\n</startup_session_context>"),
		},
		DynamicFragments: []PromptContextFragment{
			NewPromptContextFragment("context:summary", "summary", mdl.RoleSystem, "summary", "<context_summary>\nsnapshot\n</context_summary>"),
		},
	}
	out := BuildPromptMessages(msgs, state)
	if len(out) != 5 {
		t.Fatalf("len=%d, want 5", len(out))
	}
	if got := mdl.ContentPartsToPlainText(out[0].ContentParts); got != "base prompt" {
		t.Fatalf("baseline=%q", got)
	}
	if got := mdl.ContentPartsToPlainText(out[1].ContentParts); got == "" || got == "base prompt" {
		t.Fatalf("startup fragment missing: %+v", out[1])
	}
	if got := mdl.ContentPartsToPlainText(out[2].ContentParts); got == "" || got == "base prompt" {
		t.Fatalf("summary fragment missing: %+v", out[2])
	}
	if got := mdl.ContentPartsToPlainText(out[3].ContentParts); got != "u2" {
		t.Fatalf("first visible dialog=%q, want u2", got)
	}
	if got := mdl.ContentPartsToPlainText(out[4].ContentParts); got != "a2" {
		t.Fatalf("last visible dialog=%q, want a2", got)
	}
}

func TestComputePromptFragmentDiff(t *testing.T) {
	fragments := []PromptContextFragment{
		NewPromptContextFragment("baseline:0", "baseline", mdl.RoleSystem, "baseline", "hello"),
		NewPromptContextFragment("context:summary", "summary", mdl.RoleSystem, "summary", "world"),
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

func TestBuildPromptMessagesPinsLatestUserTurnWithinBudget(t *testing.T) {
	msgs := []mdl.Message{
		{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("old request")}},
		{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("old response")}},
		{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("你好")}},
		{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("tool planning " + strings.Repeat("x", 80))}},
	}
	out := BuildPromptMessages(msgs, PromptContextState{
		Version:      1,
		PromptBudget: 35,
	})
	var joined []string
	for _, msg := range out {
		joined = append(joined, mdl.ContentPartsToPlainText(msg.ContentParts))
	}
	text := strings.Join(joined, "\n")
	if !strings.Contains(text, "你好") {
		t.Fatalf("latest user turn was dropped from prompt: %q", text)
	}
}

func TestBuildPromptMessagesForGreetingDropsEarlierDialog(t *testing.T) {
	msgs := []mdl.Message{
		{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("帮我分析 README")}},
		{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("我先看下项目结构")}},
		{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("你好")}},
	}
	out := BuildPromptMessages(msgs, PromptContextState{
		Version:      1,
		PromptBudget: 200,
	})
	if len(out) != 1 {
		t.Fatalf("len=%d, want 1", len(out))
	}
	if got := mdl.ContentPartsToPlainText(out[0].ContentParts); got != "你好" {
		t.Fatalf("last visible dialog=%q, want 你好", got)
	}
}
