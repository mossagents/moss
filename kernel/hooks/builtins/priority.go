package builtins

import (
	"context"
	"fmt"
	"sort"

	"github.com/mossagents/moss/kernel/hooks"
	mdl "github.com/mossagents/moss/kernel/model"
)

// MessageScorer 对单条消息进行重要性评分。
type MessageScorer interface {
	Score(msg mdl.Message) float64
}

// MessageScorerFunc 是 MessageScorer 的函数适配器。
type MessageScorerFunc func(msg mdl.Message) float64

func (f MessageScorerFunc) Score(msg mdl.Message) float64 { return f(msg) }

// RuleScorer 基于规则对消息进行重要性评分。
type RuleScorer struct{}

func (RuleScorer) Score(msg mdl.Message) float64 {
	if msg.Role == mdl.RoleSystem {
		return 1.0
	}
	score := 0.5
	text := mdl.ContentPartsToPlainText(msg.ContentParts)
	for _, kw := range []string{"error", "failed", "exception", "panic"} {
		if containsLower(text, kw) {
			score += 0.3
			break
		}
	}
	if len(msg.ToolResults) > 0 {
		score += 0.1
	}
	if score > 1.0 {
		return 1.0
	}
	return score
}

func containsLower(text, keyword string) bool {
	lower := make([]byte, len(text))
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		lower[i] = c
	}
	return len(keyword) <= len(lower) && indexOf(string(lower), keyword) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// PriorityConfig 配置基于消息重要性评分的上下文压缩行为。
type PriorityConfig struct {
	Enabled          *bool
	Scorer           MessageScorer
	MinScore         float64
	KeepRecent       int
	MaxContextTokens int
	Tokenizer        mdl.Tokenizer
	TokenCounter     func(mdl.Message) int
}

func (c PriorityConfig) scorer() MessageScorer {
	if c.Scorer != nil {
		return c.Scorer
	}
	return RuleScorer{}
}

func (c PriorityConfig) keepRecent() int {
	if c.KeepRecent <= 0 {
		return 10
	}
	return c.KeepRecent
}

func (c PriorityConfig) maxContextTokens() int {
	if c.MaxContextTokens <= 0 {
		return 80000
	}
	return c.MaxContextTokens
}

func (c PriorityConfig) countTokens(msg mdl.Message) int {
	if c.Tokenizer != nil {
		return c.Tokenizer.CountMessage(msg)
	}
	if c.TokenCounter != nil {
		return c.TokenCounter(msg)
	}
	return estimateTokens(msg)
}

func (c PriorityConfig) enabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// PriorityCompress 构造基于消息重要性评分的上下文压缩 hook。
func PriorityCompress(cfg PriorityConfig) hooks.Hook[hooks.LLMEvent] {
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

		var systemMsgs, dialogMsgs []mdl.Message
		for _, m := range msgs {
			if m.Role == mdl.RoleSystem {
				systemMsgs = append(systemMsgs, m)
			} else {
				dialogMsgs = append(dialogMsgs, m)
			}
		}

		keepRecent := cfg.keepRecent()
		if keepRecent > len(dialogMsgs) {
			keepRecent = len(dialogMsgs)
		}

		recentMsgs := dialogMsgs[len(dialogMsgs)-keepRecent:]
		candidateMsgs := dialogMsgs[:len(dialogMsgs)-keepRecent]

		if len(candidateMsgs) == 0 {
			return nil
		}

		scorer := cfg.scorer()
		budgetTokens := cfg.maxContextTokens()
		for _, m := range systemMsgs {
			budgetTokens -= cfg.countTokens(m)
		}
		for _, m := range recentMsgs {
			budgetTokens -= cfg.countTokens(m)
		}

		type scoredMsg struct {
			msg   mdl.Message
			score float64
			orig  int
		}
		scored := make([]scoredMsg, len(candidateMsgs))
		for i, m := range candidateMsgs {
			scored[i] = scoredMsg{msg: m, score: scorer.Score(m), orig: i}
		}
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].score > scored[j].score
		})

		kept := make([]bool, len(candidateMsgs))
		remaining := budgetTokens
		for _, sm := range scored {
			if cfg.MinScore > 0 && sm.score < cfg.MinScore {
				break
			}
			cost := cfg.countTokens(sm.msg)
			if remaining-cost >= 0 {
				kept[sm.orig] = true
				remaining -= cost
			}
		}

		var keptMsgs []mdl.Message
		droppedCount := 0
		for i, m := range candidateMsgs {
			if kept[i] {
				keptMsgs = append(keptMsgs, m)
			} else {
				droppedCount++
			}
		}

		newMsgs := make([]mdl.Message, 0, len(systemMsgs)+len(keptMsgs)+1+len(recentMsgs))
		newMsgs = append(newMsgs, systemMsgs...)
		newMsgs = append(newMsgs, keptMsgs...)
		if droppedCount > 0 {
			noticeMsg := mdl.Message{
				Role: mdl.RoleSystem,
				ContentParts: []mdl.ContentPart{
					mdl.TextPart(buildPriorityNotice(droppedCount, totalTokens, cfg.maxContextTokens())),
				},
			}
			newMsgs = append(newMsgs, noticeMsg)
		}
		newMsgs = append(newMsgs, recentMsgs...)
		sess.ReplaceMessages(newMsgs)

		return nil
	}
}

func buildPriorityNotice(dropped, totalTokens, maxTokens int) string {
	return fmt.Sprintf(
		"[优先级压缩: 已按重要性评分移除 %d 条低优先级消息。tokens: %d/%d]",
		dropped, totalTokens, maxTokens,
	)
}
