package builtins

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel/middleware"
	mdl "github.com/mossagents/moss/kernel/model"
)

// SlidingWindowConfig 配置滑动窗口上下文压缩行为。
//
// 策略：维护最近 WindowSize 条对话消息，窗口外的内容在首次溢出时生成
// 一条静态摘要（此后不再重新摘要），适合工具调用密集型场景。
type SlidingWindowConfig struct {
	// Enabled 控制 middleware 是否启用；nil/true=启用，false=禁用。
	Enabled *bool

	// WindowSize 滑动窗口大小（保留最近 N 条非 system 消息），默认 30。
	WindowSize int

	// MaxContextTokens 触发压缩的 token 阈值，默认 80000。
	MaxContextTokens int

	// Summarizer 将窗口外消息转为静态摘要的函数（可选）。
	// 若为 nil，则生成简单的丢弃通知，不调用 LLM。
	Summarizer func(ctx context.Context, msgs []mdl.Message) (string, error)

	// Tokenizer 用于精确 token 计数。设置后优先于 TokenCounter。
	// nil 时退回 TokenCounter，TokenCounter 也为 nil 时使用字符/4 估算。
	Tokenizer mdl.Tokenizer

	// TokenCounter 自定义 token 计数函数（已弃用，建议改用 Tokenizer）。
	// 当 Tokenizer 未设置时生效。
	TokenCounter func(mdl.Message) int
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

func (c SlidingWindowConfig) countTokens(msg mdl.Message) int {
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

// SlidingWindow 构造滑动窗口上下文压缩 middleware。
//
// 在每次 LLM 调用前，若 token 数超过阈值，则：
//  1. 保留所有 system messages
//  2. 只保留最近 WindowSize 条对话消息
//  3. 用一条静态摘要消息替换被移除的历史（首次计算后不再更新）
//
// 用法：
//
//	k := kernel.New(kernel.Use(builtins.SlidingWindow(builtins.SlidingWindowConfig{
//	    WindowSize:       30,
//	    MaxContextTokens: 100000,
//	})))
func SlidingWindow(cfg SlidingWindowConfig) middleware.Middleware {
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

		win := cfg.windowSize()
		if win >= len(dialogMsgs) {
			return next(ctx) // 已在窗口内，无需压缩
		}

		evicted := dialogMsgs[:len(dialogMsgs)-win]
		recentMsgs := dialogMsgs[len(dialogMsgs)-win:]

		// 生成或重用静态摘要
		summaryText := buildSlideNotice(len(evicted), totalTokens, cfg.maxContextTokens())
		if cfg.Summarizer != nil {
			if s, err := cfg.Summarizer(ctx, evicted); err == nil && s != "" {
				summaryText = s
			}
		}

		summaryMsg := mdl.Message{
			Role:         mdl.RoleSystem,
			ContentParts: []mdl.ContentPart{mdl.TextPart(summaryText)},
		}

		newMsgs := make([]mdl.Message, 0, len(systemMsgs)+1+len(recentMsgs))
		newMsgs = append(newMsgs, systemMsgs...)
		newMsgs = append(newMsgs, summaryMsg)
		newMsgs = append(newMsgs, recentMsgs...)
		sess.ReplaceMessages(newMsgs)

		return next(ctx)
	}
}

func buildSlideNotice(evictedCount, totalTokens, maxTokens int) string {
	return fmt.Sprintf(
		"[滑动窗口压缩: 已移除 %d 条历史消息，保留最近对话。tokens: %d/%d]",
		evictedCount, totalTokens, maxTokens,
	)
}
