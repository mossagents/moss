package builtins

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/model"
)

// SlidingWindowConfig 配置滑动窗口上下文压缩行为。
type SlidingWindowConfig struct {
	Enabled          *bool
	WindowSize       int
	MaxContextTokens int
	Summarizer       func(ctx context.Context, msgs []model.Message) (string, error)
	Tokenizer        model.Tokenizer
	TokenCounter     func(model.Message) int
}

func (c SlidingWindowConfig) windowSize() int {
	if c.WindowSize <= 0 {
		return 30
	}
	return c.WindowSize
}

func (c SlidingWindowConfig) maxContextTokens() int {
	if c.MaxContextTokens <= 0 {
		return 80000
	}
	return c.MaxContextTokens
}

func (c SlidingWindowConfig) countTokens(msg model.Message) int {
	if c.Tokenizer != nil {
		return c.Tokenizer.CountMessage(msg)
	}
	if c.TokenCounter != nil {
		return c.TokenCounter(msg)
	}
	return estimateTokens(msg)
}

func (c SlidingWindowConfig) enabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// SlidingWindow 构造滑动窗口上下文压缩 hook。
func SlidingWindow(cfg SlidingWindowConfig) hooks.Hook[hooks.LLMEvent] {
	return func(ctx context.Context, ev *hooks.LLMEvent) error {
		if !cfg.enabled() {
			return nil
		}
		if ev.Session == nil {
			return nil
		}

		sess := ev.Session
		msgs := sess.CopyMessages()

		totalTokens := 0
		for _, m := range msgs {
			totalTokens += cfg.countTokens(m)
		}

		if totalTokens <= cfg.maxContextTokens() {
			return nil
		}

		var systemMsgs, dialogMsgs []model.Message
		for _, m := range msgs {
			if m.Role == model.RoleSystem {
				systemMsgs = append(systemMsgs, m)
			} else {
				dialogMsgs = append(dialogMsgs, m)
			}
		}

		win := cfg.windowSize()
		if win >= len(dialogMsgs) {
			return nil
		}

		evicted := dialogMsgs[:len(dialogMsgs)-win]
		recentMsgs := dialogMsgs[len(dialogMsgs)-win:]

		summaryText := buildSlideNotice(len(evicted), totalTokens, cfg.maxContextTokens())
		if cfg.Summarizer != nil {
			if s, err := cfg.Summarizer(ctx, evicted); err == nil && s != "" {
				summaryText = s
			}
		}

		summaryMsg := model.Message{
			Role:         model.RoleSystem,
			ContentParts: []model.ContentPart{model.TextPart(summaryText)},
		}

		newMsgs := make([]model.Message, 0, len(systemMsgs)+1+len(recentMsgs))
		newMsgs = append(newMsgs, systemMsgs...)
		newMsgs = append(newMsgs, summaryMsg)
		newMsgs = append(newMsgs, recentMsgs...)
		sess.ReplaceMessages(newMsgs)

		return nil
	}
}

func buildSlideNotice(evictedCount, totalTokens, maxTokens int) string {
	return fmt.Sprintf(
		"[滑动窗口压缩: 已移除 %d 条历史消息，保留最近对话。tokens: %d/%d]",
		evictedCount, totalTokens, maxTokens,
	)
}
