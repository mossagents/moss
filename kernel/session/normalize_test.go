package session

import (
	"encoding/json"
	"testing"

	"github.com/mossagents/moss/kernel/model"
)

func TestNormalizeForPromptInsertsAbortedToolResult(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{}`)}}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("next")}},
	}
	normalized := NormalizeForPrompt(msgs)
	if len(normalized) != 3 {
		t.Fatalf("len=%d, want 3", len(normalized))
	}
	if normalized[1].Role != model.RoleTool || len(normalized[1].ToolResults) != 1 {
		t.Fatalf("missing synthesized tool result: %+v", normalized)
	}
	if !normalized[1].ToolResults[0].IsError {
		t.Fatal("expected synthesized tool result to be error")
	}
}

func TestNormalizeForPromptDropsOrphanToolResult(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleTool, ToolResults: []model.ToolResult{{CallID: "orphan", ContentParts: []model.ContentPart{model.TextPart("ignored")}}}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hello")}},
	}
	normalized := NormalizeForPrompt(msgs)
	if len(normalized) != 1 {
		t.Fatalf("len=%d, want 1", len(normalized))
	}
	if got := model.ContentPartsToPlainText(normalized[0].ContentParts); got != "hello" {
		t.Fatalf("remaining message=%q, want hello", got)
	}
}

func TestNormalizeForPromptWithStatsReportsDroppedAndSynthesized(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleTool, ToolResults: []model.ToolResult{{CallID: "orphan", ContentParts: []model.ContentPart{model.TextPart("ignored")}}}},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{}`)}}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("continue")}},
	}
	normalized, stats := NormalizeForPromptWithStats(msgs)
	if len(normalized) != 3 {
		t.Fatalf("len=%d, want 3", len(normalized))
	}
	if stats.DroppedOrphanToolResults != 1 {
		t.Fatalf("dropped orphan tool results=%d, want 1", stats.DroppedOrphanToolResults)
	}
	if stats.SynthesizedMissingToolResults != 1 {
		t.Fatalf("synthesized missing tool results=%d, want 1", stats.SynthesizedMissingToolResults)
	}
	if !stats.Changed() {
		t.Fatal("expected normalization stats to report a change")
	}
	if normalized[1].Role != model.RoleTool || len(normalized[1].ToolResults) != 1 {
		t.Fatalf("missing synthesized tool result: %+v", normalized)
	}
}
