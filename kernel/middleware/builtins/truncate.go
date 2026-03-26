package builtins

import (
	"context"
	"strings"

	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
)

// TruncateConfig 配置自动 token 截断行为。
type TruncateConfig struct {
	// MaxContextTokens 触发截断的 token 阈值。
	// 当对话历史总 token 超过此值时，自动保留最近的消息。
	// 默认 80000。
	MaxContextTokens int

	// KeepRecent 截断后保留的最近消息数（不含 system 消息）。
	// 默认 20。
	KeepRecent int

	// TokenCounter 自定义 token 计数函数。
	// 默认使用简单的字符数 / 4 估算。
	TokenCounter func(port.Message) int
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

func (c TruncateConfig) countTokens(msg port.Message) int {
	if c.TokenCounter != nil {
		return c.TokenCounter(msg)
	}
	return estimateTokens(msg)
}

// estimateTokens 简单估算一条消息的 token 数（适用于无 tokenizer 场景）。
func estimateTokens(msg port.Message) int {
	total := len(msg.Content) / 4
	for _, tc := range msg.ToolCalls {
		total += len(tc.Name)/4 + len(tc.Arguments)/4
	}
	for _, tr := range msg.ToolResults {
		total += len(tr.Content) / 4
	}
	if total < 1 && (msg.Content != "" || len(msg.ToolCalls) > 0 || len(msg.ToolResults) > 0) {
		total = 1
	}
	return total
}

// AutoTruncate 构造自动 token 截断 middleware。
// 在每次 LLM 调用前检查对话历史长度，超过阈值时自动截断，保留 system 消息和最近的对话。
//
// 用法：
//
//	k := kernel.New(kernel.Use(builtins.AutoTruncate(builtins.TruncateConfig{
//	    MaxContextTokens: 100000,
//	    KeepRecent:       30,
//	})))
func AutoTruncate(cfg TruncateConfig) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase != middleware.BeforeLLM {
			return next(ctx)
		}

		sess := mc.Session
		if sess == nil {
			return next(ctx)
		}

		// 计算当前总 token
		totalTokens := 0
		for _, msg := range sess.Messages {
			totalTokens += cfg.countTokens(msg)
		}

		if totalTokens <= cfg.maxContextTokens() {
			return next(ctx)
		}

		// 分离 system 消息和对话消息
		var systemMsgs, dialogMsgs []port.Message
		for _, msg := range sess.Messages {
			if msg.Role == port.RoleSystem {
				systemMsgs = append(systemMsgs, msg)
			} else {
				dialogMsgs = append(dialogMsgs, msg)
			}
		}

		keepRecent := cfg.keepRecent()
		if keepRecent > len(dialogMsgs) {
			keepRecent = len(dialogMsgs)
		}

		// 保留最近的对话
		recentMsgs := dialogMsgs[len(dialogMsgs)-keepRecent:]

		// 构造截断摘要作为上下文
		droppedCount := len(dialogMsgs) - keepRecent
		if droppedCount > 0 {
			summary := port.Message{
				Role:    port.RoleSystem,
				Content: buildTruncationNotice(droppedCount, totalTokens, cfg.maxContextTokens()),
			}
			sess.Messages = append(systemMsgs, summary)
			sess.Messages = append(sess.Messages, recentMsgs...)
		}

		return next(ctx)
	}
}

// DefaultAutoTruncate 返回使用默认配置的截断 middleware。
func DefaultAutoTruncate() middleware.Middleware {
	return AutoTruncate(TruncateConfig{})
}

func buildTruncationNotice(droppedCount, totalTokens, maxTokens int) string {
	var b strings.Builder
	b.WriteString("[Context truncated: ")
	b.WriteString(strings.Join([]string{
		intToStr(droppedCount) + " earlier messages removed",
		"keeping most recent conversation",
	}, "; "))
	b.WriteString("]")
	return b.String()
}

func intToStr(n int) string {
	if n < 0 {
		return "-" + intToStr(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return intToStr(n/10) + string(rune('0'+n%10))
}
