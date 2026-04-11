package session

import "github.com/mossagents/moss/kernel/model"

// CloneMessage returns a deep-enough copy of a message for safe reuse across
// session branches and yielded events.
func CloneMessage(msg model.Message) model.Message {
	out := msg
	if len(msg.ContentParts) > 0 {
		out.ContentParts = append([]model.ContentPart(nil), msg.ContentParts...)
	}
	if len(msg.ToolCalls) > 0 {
		out.ToolCalls = make([]model.ToolCall, len(msg.ToolCalls))
		for i, call := range msg.ToolCalls {
			out.ToolCalls[i] = call
			if len(call.Arguments) > 0 {
				out.ToolCalls[i].Arguments = append([]byte(nil), call.Arguments...)
			}
		}
	}
	if len(msg.ToolResults) > 0 {
		out.ToolResults = make([]model.ToolResult, len(msg.ToolResults))
		for i, result := range msg.ToolResults {
			out.ToolResults[i] = result
			if len(result.ContentParts) > 0 {
				out.ToolResults[i].ContentParts = append([]model.ContentPart(nil), result.ContentParts...)
			}
		}
	}
	return out
}

// CloneMessages deep-clones a message slice for branch-local session reuse.
func CloneMessages(msgs []model.Message) []model.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]model.Message, len(msgs))
	for i, msg := range msgs {
		out[i] = CloneMessage(msg)
	}
	return out
}

func cloneSessionConfig(cfg SessionConfig) SessionConfig {
	out := cfg
	if len(cfg.Metadata) == 0 {
		return out
	}
	out.Metadata = make(map[string]any, len(cfg.Metadata))
	for k, v := range cfg.Metadata {
		out.Metadata[k] = v
	}
	return out
}

// Clone returns a branch-local copy of the session with independent message,
// state, and budget containers.
func (s *Session) Clone() *Session {
	if s == nil {
		return nil
	}
	return &Session{
		ID:                    s.ID,
		Status:                s.Status,
		Config:                cloneSessionConfig(s.Config),
		Title:                 s.GetTitle(),
		Messages:              CloneMessages(s.CopyMessages()),
		State:                 s.CopyState(),
		Budget:                s.Budget.Clone(),
		CreatedAt:             s.CreatedAt,
		EndedAt:               s.EndedAt,
		materializationDomain: nextMaterializationDomain(),
	}
}
