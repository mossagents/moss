package builtins

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/model"
)

// PatchToolCalls ensures every assistant tool call in history has a matching tool result.
// Missing results are backfilled before the next LLM turn to keep provider adapters stable.
func PatchToolCalls() hooks.Hook[hooks.LLMEvent] {
	return func(ctx context.Context, ev *hooks.LLMEvent) error {
		if ev.Session == nil {
			return nil
		}
		if len(ev.Session.Messages) == 0 {
			return nil
		}

		pending := make(map[string]model.ToolCall)
		for _, msg := range ev.Session.Messages {
			for _, tc := range msg.ToolCalls {
				if tc.ID == "" {
					continue
				}
				pending[tc.ID] = tc
			}
			for _, tr := range msg.ToolResults {
				if tr.CallID == "" {
					continue
				}
				delete(pending, tr.CallID)
			}
		}
		if len(pending) == 0 {
			return nil
		}

		patches := make([]model.Message, 0, len(pending))
		for callID, tc := range pending {
			patches = append(patches, model.Message{
				Role: model.RoleTool,
				ToolResults: []model.ToolResult{
					{
						CallID:       callID,
						IsError:      true,
						ContentParts: []model.ContentPart{model.TextPart(fmt.Sprintf("missing tool result patched for call %q (%s)", callID, tc.Name))},
					},
				},
			})
		}
		ev.Session.Messages = append(ev.Session.Messages, patches...)
		return nil
	}
}
