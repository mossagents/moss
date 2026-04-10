package session

import (
	"github.com/mossagents/moss/kernel/model"
	"strings"
	"testing"
)

func TestBuildPromptMessagesUsesFragmentsAndCompactedDialogBoundary(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart("base prompt")}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("u1")}},
		{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("a1")}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("u2")}},
		{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("a2")}},
	}
	state := PromptContextState{
		Version:              1,
		PromptBudget:         200,
		CompactedDialogCount: 2,
		BaselineFragments: []PromptContextFragment{
			NewPromptContextFragment("baseline:0", "baseline", model.RoleSystem, "baseline", "base prompt"),
		},
		StartupFragments: []PromptContextFragment{
			NewPromptContextFragment("startup:session", "startup", model.RoleSystem, "startup", "<startup_session_context>\nsummary\n</startup_session_context>"),
		},
		DynamicFragments: []PromptContextFragment{
			NewPromptContextFragment("context:summary", "summary", model.RoleSystem, "summary", "<context_summary>\nsnapshot\n</context_summary>"),
		},
	}
	out := BuildPromptMessages(msgs, state)
	if len(out) != 5 {
		t.Fatalf("len=%d, want 5", len(out))
	}
	if got := model.ContentPartsToPlainText(out[0].ContentParts); got != "base prompt" {
		t.Fatalf("baseline=%q", got)
	}
	if got := model.ContentPartsToPlainText(out[1].ContentParts); got == "" || got == "base prompt" {
		t.Fatalf("startup fragment missing: %+v", out[1])
	}
	if got := model.ContentPartsToPlainText(out[2].ContentParts); got == "" || got == "base prompt" {
		t.Fatalf("summary fragment missing: %+v", out[2])
	}
	if got := model.ContentPartsToPlainText(out[3].ContentParts); got != "u2" {
		t.Fatalf("first visible dialog=%q, want u2", got)
	}
	if got := model.ContentPartsToPlainText(out[4].ContentParts); got != "a2" {
		t.Fatalf("last visible dialog=%q, want a2", got)
	}
}

func TestComputePromptFragmentDiff(t *testing.T) {
	fragments := []PromptContextFragment{
		NewPromptContextFragment("baseline:0", "baseline", model.RoleSystem, "baseline", "hello"),
		NewPromptContextFragment("context:summary", "summary", model.RoleSystem, "summary", "world"),
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
	msgs := []model.Message{
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("old request")}},
		{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("old response")}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("你好")}},
		{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("tool planning " + strings.Repeat("x", 80))}},
	}
	out := BuildPromptMessages(msgs, PromptContextState{
		Version:      1,
		PromptBudget: 35,
	})
	var joined []string
	for _, msg := range out {
		joined = append(joined, model.ContentPartsToPlainText(msg.ContentParts))
	}
	text := strings.Join(joined, "\n")
	if !strings.Contains(text, "你好") {
		t.Fatalf("latest user turn was dropped from prompt: %q", text)
	}
}

func TestBuildPromptMessagesForGreetingDropsEarlierDialog(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("帮我分析 README")}},
		{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("我先看下项目结构")}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("你好")}},
	}
	out := BuildPromptMessages(msgs, PromptContextState{
		Version:      1,
		PromptBudget: 200,
	})
	if len(out) != 1 {
		t.Fatalf("len=%d, want 1", len(out))
	}
	if got := model.ContentPartsToPlainText(out[0].ContentParts); got != "你好" {
		t.Fatalf("last visible dialog=%q, want 你好", got)
	}
}
