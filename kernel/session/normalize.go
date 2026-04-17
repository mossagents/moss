package session

import "github.com/mossagents/moss/kernel/model"

// NormalizeForPrompt repairs tool call / tool result history before sending it
// to the model:
// 1. orphan tool results are dropped
// 2. missing tool results are synthesized as aborted errors
func NormalizeForPrompt(messages []model.Message) []model.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]model.Message, 0, len(messages)+1)
	var pending []string
	pendingSet := map[string]struct{}{}

	flushPending := func() {
		if len(pending) == 0 {
			return
		}
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
				continue
			}
			filtered := make([]model.ToolResult, 0, len(msg.ToolResults))
			for _, result := range msg.ToolResults {
				if _, ok := pendingSet[result.CallID]; !ok {
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
	return out
}
