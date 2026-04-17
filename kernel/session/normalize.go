package session

import "github.com/mossagents/moss/kernel/model"

type PromptNormalizationStats struct {
	InputMessages                 int `json:"input_messages,omitempty"`
	OutputMessages                int `json:"output_messages,omitempty"`
	DroppedOrphanToolResults      int `json:"dropped_orphan_tool_results,omitempty"`
	SynthesizedMissingToolResults int `json:"synthesized_missing_tool_results,omitempty"`
}

func (s PromptNormalizationStats) Changed() bool {
	return s.DroppedOrphanToolResults > 0 ||
		s.SynthesizedMissingToolResults > 0 ||
		s.InputMessages != s.OutputMessages
}

// NormalizeForPrompt repairs tool call / tool result history before sending it
// to the model:
// 1. orphan tool results are dropped
// 2. missing tool results are synthesized as aborted errors
func NormalizeForPrompt(messages []model.Message) []model.Message {
	normalized, _ := NormalizeForPromptWithStats(messages)
	return normalized
}

// NormalizeForPromptWithStats repairs tool call / tool result history and
// reports how much cleanup was required.
func NormalizeForPromptWithStats(messages []model.Message) ([]model.Message, PromptNormalizationStats) {
	stats := PromptNormalizationStats{InputMessages: len(messages)}
	if len(messages) == 0 {
		return nil, stats
	}
	out := make([]model.Message, 0, len(messages)+1)
	var pending []string
	pendingSet := map[string]struct{}{}

	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		stats.SynthesizedMissingToolResults += len(pending)
		results := make([]model.ToolResult, 0, len(pending))
		for _, callID := range pending {
			results = append(results, model.ToolResult{
				CallID:       callID,
				ContentParts: []model.ContentPart{model.TextPart("tool execution aborted before result was recorded")},
				IsError:      true,
			})
		}
		out = append(out, model.Message{Role: model.RoleTool, ToolResults: results})
		pending = nil
		pendingSet = map[string]struct{}{}
	}

	for _, msg := range messages {
		switch {
		case len(msg.ToolCalls) > 0:
			flushPending()
			out = append(out, CloneMessage(msg))
			pending = pending[:0]
			pendingSet = map[string]struct{}{}
			for _, call := range msg.ToolCalls {
				if call.ID == "" {
					continue
				}
				pending = append(pending, call.ID)
				pendingSet[call.ID] = struct{}{}
			}
		case len(msg.ToolResults) > 0:
			if len(pending) == 0 {
				stats.DroppedOrphanToolResults += len(msg.ToolResults)
				continue
			}
			filtered := make([]model.ToolResult, 0, len(msg.ToolResults))
			for _, result := range msg.ToolResults {
				if _, ok := pendingSet[result.CallID]; !ok {
					stats.DroppedOrphanToolResults++
					continue
				}
				filtered = append(filtered, result)
				delete(pendingSet, result.CallID)
			}
			if len(filtered) == 0 {
				continue
			}
			normalized := CloneMessage(msg)
			normalized.ToolResults = filtered
			out = append(out, normalized)
			if len(pendingSet) == 0 {
				pending = nil
			}
		default:
			flushPending()
			out = append(out, CloneMessage(msg))
		}
	}
	flushPending()
	stats.OutputMessages = len(out)
	return out, stats
}
