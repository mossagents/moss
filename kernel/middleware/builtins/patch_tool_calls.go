package builtins

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
)

// PatchToolCalls ensures every assistant tool call in history has a matching tool result.
// Missing results are backfilled before the next LLM turn to keep provider adapters stable.
func PatchToolCalls() middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase != middleware.BeforeLLM || mc.Session == nil {
			return next(ctx)
		}
		if len(mc.Session.Messages) == 0 {
			return next(ctx)
		}

		pending := make(map[string]port.ToolCall)
		for _, msg := range mc.Session.Messages {
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
			return next(ctx)
		}

		patches := make([]port.Message, 0, len(pending))
		for callID, tc := range pending {
			patches = append(patches, port.Message{
				Role: port.RoleTool,
				ToolResults: []port.ToolResult{
					{
						CallID:       callID,
						IsError:      true,
						ContentParts: []port.ContentPart{port.TextPart(fmt.Sprintf("missing tool result patched for call %q (%s)", callID, tc.Name))},
					},
				},
			})
		}
		mc.Session.Messages = append(mc.Session.Messages, patches...)
		return next(ctx)
	}
}
