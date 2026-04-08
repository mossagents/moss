package builtins

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mossagents/moss/kernel/middleware"
	mdl "github.com/mossagents/moss/kernel/model"
)

// MessageScorer 对单条消息进行重要性评分。
type MessageScorer interface {
	Score(msg mdl.Message) float64
}

// MessageScorerFunc 是 MessageScorer 的函数适配器。
type MessageScorerFunc func(msg mdl.Message) float64

func (f MessageScorerFunc) Score(msg mdl.Message) float64 { return f(msg) }

// RuleScorer 基于规则对消息进行重要性评分，无需 LLM 开销。
//
// 评分规则：
//   - system message：1.0（始终最高）
//   - 包含 error/failed/exception/panic 关键词的消息：+0.3
//   - tool result 消息：+0.1
//   - 基础分：0.5
type RuleScorer struct{}

func (RuleScorer) Score(msg mdl.Message) float64 {
	if msg.Role == mdl.RoleSystem {
		return 1.0
	}
	score := 0.5
	text := strings.ToLower(mdl.ContentPartsToPlainText(msg.ContentParts))
	if strings.Contains(text, "error") ||
		strings.Contains(text, "failed") ||
		strings.Contains(text, "exception") ||
		strings.Contains(text, "panic") {
		score += 0.3
	}
	if len(msg.ToolResults) > 0 {
		score += 0.1
	}
	if score > 1.0 {
		return 1.0
	}
	return score
}

// PriorityConfig 配置基于消息重要性评分的上下文压缩行为。
//
// 策略：对每条消息评分，按分数降序保留，直到满足 token 限制。
// system messages 和最近 KeepRecent 条消息不参与评分淘汰。
type PriorityConfig struct {
	// Enabled 控制 middleware 是否启用；nil/true=启用，false=禁用。
	Enabled *bool

	// Scorer 消息重要性评分器，默认使用 RuleScorer。
	Scorer MessageScorer

	// MinScore 保留的最低分数阈值（0.0-1.0），低于此分数的消息优先被淘汰。
	// 默认 0.0（不强制过滤，只按 token 预算淘汰）。
	MinScore float64

	// KeepRecent 始终保留最近 N 条消息（不参与评分淘汰），默认 10。
	KeepRecent int

	// MaxContextTokens 触发压缩的 token 阈值，默认 80000。
	MaxContextTokens int

	// Tokenizer 用于精确 token 计数。设置后优先于 TokenCounter。
	// nil 时退回 TokenCounter，TokenCounter 也为 nil 时使用字符/4 估算。
	Tokenizer mdl.Tokenizer

	// TokenCounter 自定义 token 计数函数（已弃用，建议改用 Tokenizer）。
	// 当 Tokenizer 未设置时生效。
	TokenCounter func(mdl.Message) int
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

// PriorityCompress 构造基于消息重要性评分的上下文压缩 middleware。
//
// 在每次 LLM 调用前，若 token 数超过阈值：
//  1. system messages 始终保留
//  2. 最近 KeepRecent 条消息始终保留
//  3. 剩余消息按评分降序排列，在 token 预算内尽量保留高分消息
//  4. 被淘汰的消息用压缩通知替代
//
// 用法：
//
//	k := kernel.New(kernel.Use(builtins.PriorityCompress(builtins.PriorityConfig{
//	    Scorer:           builtins.RuleScorer{},
//	    KeepRecent:       10,
//	    MaxContextTokens: 100000,
//	})))
func PriorityCompress(cfg PriorityConfig) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if !cfg.enabled() {
			return next(ctx)
		}
		if mc.Phase != middleware.BeforeLLM {
			return next(ctx)
		}
		if mc.Session == nil {
			return next(ctx)
		}

		sess := mc.Session
		msgs := sess.CopyMessages()

		totalTokens := 0
		for _, m := range msgs {
			totalTokens += cfg.countTokens(m)
		}

		if totalTokens <= cfg.maxContextTokens() {
			return next(ctx)
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
			return next(ctx)
		}

		// 计算保留预算：maxTokens - systemMsgs tokens - recentMsgs tokens
		scorer := cfg.scorer()
		budgetTokens := cfg.maxContextTokens()
		for _, m := range systemMsgs {
			budgetTokens -= cfg.countTokens(m)
		}
		for _, m := range recentMsgs {
			budgetTokens -= cfg.countTokens(m)
		}

		// 按评分降序排列候选消息
		type scoredMsg struct {
			msg   mdl.Message
			score float64
			orig  int // 原始位置，用于恢复顺序
		}
		scored := make([]scoredMsg, len(candidateMsgs))
		for i, m := range candidateMsgs {
			scored[i] = scoredMsg{msg: m, score: scorer.Score(m), orig: i}
		}
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].score > scored[j].score
		})

		// 贪心选取：分数高的优先保留，直到 token 预算耗尽
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

		return next(ctx)
	}
}

func buildPriorityNotice(dropped, totalTokens, maxTokens int) string {
	return fmt.Sprintf(
		"[优先级压缩: 已按重要性评分移除 %d 条低优先级消息。tokens: %d/%d]",
		dropped, totalTokens, maxTokens,
	)
}
