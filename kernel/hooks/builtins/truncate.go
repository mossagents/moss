package builtins

import (
	"context"
	"strconv"
	"strings"

	"github.com/mossagents/moss/kernel/hooks"
	mdl "github.com/mossagents/moss/kernel/model"
)

// TruncateConfig 配置自动 token 截断行为。
type TruncateConfig struct {
	MaxContextTokens int
	KeepRecent       int
	Tokenizer        mdl.Tokenizer
	TokenCounter     func(mdl.Message) int
}

func (c TruncateConfig) maxContextTokens() int {
	if c.MaxContextTokens <= 0 {
		return 80000
	}
	return c.MaxContextTokens
}

func (c TruncateConfig) keepRecent() int {
	if c.KeepRecent <= 0 {
		return 20
	}
	return c.KeepRecent
}

func (c TruncateConfig) countTokens(msg mdl.Message) int {
	if c.Tokenizer != nil {
		return c.Tokenizer.CountMessage(msg)
	}
	if c.TokenCounter != nil {
		return c.TokenCounter(msg)
	}
	return estimateTokens(msg)
}

// estimateTokens 简单估算一条消息的 token 数。
func estimateTokens(msg mdl.Message) int {
	total := len(mdl.ContentPartsToPlainText(msg.ContentParts)) / 4
	for _, tc := range msg.ToolCalls {
		total += len(tc.Name)/4 + len(tc.Arguments)/4
	}
	for _, tr := range msg.ToolResults {
		total += len(mdl.ContentPartsToPlainText(tr.ContentParts)) / 4
	}
	if total < 1 && (len(msg.ContentParts) > 0 || len(msg.ToolCalls) > 0 || len(msg.ToolResults) > 0) {
		total = 1
	}
	return total
}

// AutoTruncate 构造自动 token 截断 hook。
// 在每次 LLM 调用前检查对话历史长度，超过阈值时自动截断，保留 system 消息和最近的对话。
func AutoTruncate(cfg TruncateConfig) hooks.Hook[hooks.LLMEvent] {
	return func(ctx context.Context, ev *hooks.LLMEvent) error {
		sess := ev.Session
		if sess == nil {
			return nil
		}

		totalTokens := 0
		for _, msg := range sess.CopyMessages() {
			totalTokens += cfg.countTokens(msg)
		}

		if totalTokens <= cfg.maxContextTokens() {
			return nil
		}

		var systemMsgs, dialogMsgs []mdl.Message
		for _, msg := range sess.CopyMessages() {
			if msg.Role == mdl.RoleSystem {
				systemMsgs = append(systemMsgs, msg)
			} else {
				dialogMsgs = append(dialogMsgs, msg)
			}
		}

		keepRecent := cfg.keepRecent()
		if keepRecent > len(dialogMsgs) {
			keepRecent = len(dialogMsgs)
		}

		recentMsgs := dialogMsgs[len(dialogMsgs)-keepRecent:]
		droppedCount := len(dialogMsgs) - keepRecent
		if droppedCount > 0 {
			summary := mdl.Message{
				Role:         mdl.RoleSystem,
				ContentParts: []mdl.ContentPart{mdl.TextPart(buildTruncationNotice(droppedCount, totalTokens, cfg.maxContextTokens()))},
			}
			newMsgs := make([]mdl.Message, 0, len(systemMsgs)+1+len(recentMsgs))
			newMsgs = append(newMsgs, systemMsgs...)
			newMsgs = append(newMsgs, summary)
			newMsgs = append(newMsgs, recentMsgs...)
			sess.ReplaceMessages(newMsgs)
		}

		return nil
	}
}

// DefaultAutoTruncate 返回使用默认配置的截断 hook。
func DefaultAutoTruncate() hooks.Hook[hooks.LLMEvent] {
	return AutoTruncate(TruncateConfig{})
}

func buildTruncationNotice(droppedCount, totalTokens, maxTokens int) string {
	var b strings.Builder
	b.WriteString("[Context truncated: ")
	parts := []string{
		strconv.Itoa(droppedCount) + " earlier messages removed",
		"keeping most recent conversation",
	}
	if maxTokens > 0 {
		parts = append(parts, "tokens: "+strconv.Itoa(totalTokens)+"/"+strconv.Itoa(maxTokens))
	}
	b.WriteString(strings.Join(parts, "; "))
	b.WriteString("]")
	return b.String()
}
