package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

const (
	contextStateVersion           = 1
	contextSummaryFragmentID      = "context:summary"
	contextSummaryFragmentKind    = "summary"
	contextBaselineFragmentPrefix = "baseline:"
)

type contextFragmentKind string

const (
	contextFragmentDialog      contextFragmentKind = "dialog"
	contextFragmentAgentsMD    contextFragmentKind = "agents_md"
	contextFragmentSkill       contextFragmentKind = "skill"
	contextFragmentEnvironment contextFragmentKind = "environment"
	contextFragmentSubagent    contextFragmentKind = "subagent_notification"
	contextFragmentShell       contextFragmentKind = "user_shell_command"
	contextFragmentAborted     contextFragmentKind = "turn_aborted"
	contextFragmentOther       contextFragmentKind = "other"
)

func countDialogMessages(msgs []model.Message) int {
	count := 0
	for _, m := range msgs {
		if m.Role != model.RoleSystem {
			count++
		}
	}
	return count
}

func buildSummary(ctx context.Context, llm model.LLM, msgs []model.Message) string {
	if llm == nil {
		return ""
	}
	reqMsgs := []model.Message{{
		Role:         model.RoleSystem,
		ContentParts: []model.ContentPart{model.TextPart("Summarize the earlier conversation in <=120 words, focusing on decisions, open tasks, and constraints.")},
	}}
	for _, msg := range msgs {
		if !includeMessageInMemorySummary(msg) {
			continue
		}
		reqMsgs = append(reqMsgs, msg)
	}
	resp, err := model.Complete(ctx, llm, model.CompletionRequest{
		Messages: reqMsgs,
		Config:   model.ModelConfig{Temperature: 0},
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(model.ContentPartsToPlainText(resp.Message.ContentParts))
}

func includeMessageInMemorySummary(msg model.Message) bool {
	switch classifyContextFragment(msg) {
	case contextFragmentAgentsMD, contextFragmentSkill:
		return false
	default:
		return true
	}
}

func classifyContextFragment(msg model.Message) contextFragmentKind {
	content := strings.TrimSpace(strings.ToLower(model.ContentPartsToPlainText(msg.ContentParts)))
	if msg.Role != model.RoleSystem {
		return contextFragmentDialog
	}
	switch {
	case strings.Contains(content, "<agents_md>"):
		return contextFragmentAgentsMD
	case strings.Contains(content, "<skill>"):
		return contextFragmentSkill
	case strings.Contains(content, "<environment_context>"):
		return contextFragmentEnvironment
	case strings.Contains(content, "<subagent_notification>"):
		return contextFragmentSubagent
	case strings.Contains(content, "<user_shell_command>"):
		return contextFragmentShell
	case strings.Contains(content, "<turn_aborted>"):
		return contextFragmentAborted
	default:
		return contextFragmentOther
	}
}

func buildBaselineFragments(messages []model.Message) []session.PromptContextFragment {
	fragments := make([]session.PromptContextFragment, 0, len(messages))
	for i, msg := range messages {
		if msg.Role != model.RoleSystem {
			continue
		}
		text := strings.TrimSpace(model.ContentPartsToPlainText(msg.ContentParts))
		if text == "" {
			continue
		}
		kind := string(classifyContextFragment(msg))
		fragments = append(fragments, session.NewPromptContextFragment(
			fmt.Sprintf("%s%d", contextBaselineFragmentPrefix, i),
			kind,
			model.RoleSystem,
			kind,
			text,
		))
	}
	return fragments
}

func newSummaryFragment(snapshotID, summary string, withSummary bool) session.PromptContextFragment {
	label := "context_offload"
	title := "Context offload snapshot"
	if withSummary {
		label = "context_summary"
		title = "Compacted conversation summary"
	}
	body := strings.TrimSpace(summary)
	if snapshotID != "" {
		body = fmt.Sprintf("Snapshot: %s\n\n%s", snapshotID, body)
	}
	return session.NewPromptContextFragment(
		contextSummaryFragmentID,
		contextSummaryFragmentKind,
		model.RoleSystem,
		title,
		session.FormatPromptContextFragment(label, body),
	)
}

func messagesBeforeDialogTail(messages []model.Message, keepRecent int) []model.Message {
	dialogCount := countDialogMessages(messages)
	cut := dialogCount - keepRecent
	if cut <= 0 {
		return append([]model.Message(nil), messages...)
	}
	seenDialog := 0
	out := make([]model.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == model.RoleSystem {
			out = append(out, msg)
			continue
		}
		seenDialog++
		if seenDialog > cut {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
