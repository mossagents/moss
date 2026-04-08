package builtins

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/mossagents/moss/kernel/middleware"
	mdl "github.com/mossagents/moss/kernel/model"
)

// SummarizeConfig 配置对话历史摘要压缩行为。
type SummarizeConfig struct {
	// Enabled 控制 middleware 是否启用；nil/true=启用，false=禁用。
	Enabled *bool

	// LLM 用于生成摘要的模型（必须提供）。
	LLM mdl.LLM

	// MaxContextTokens 触发摘要压缩的 token 阈值。
	// 当对话历史 token 超过此值时，自动将旧历史压缩为摘要。
	// 默认 80000。
	MaxContextTokens int

	// KeepRecent 摘要后保留的最近消息数（不含 system 消息）。
	// 默认 20。
	KeepRecent int

	// MaxSummaryTokens 单次摘要的最大 token 数（提示给 LLM）。
	// 默认 800。
	MaxSummaryTokens int

	// SummaryPrompt 自定义摘要指令（发给 LLM 的 system prompt）。
	// 为空时使用默认中文摘要指令。
	SummaryPrompt string

	// Tokenizer 用于精确 token 计数。设置后优先于 TokenCounter。
	// nil 时退回 TokenCounter，TokenCounter 也为 nil 时使用字符/4 估算。
	Tokenizer mdl.Tokenizer

	// TokenCounter 自定义 token 计数函数（已弃用，建议改用 Tokenizer）。
	// 当 Tokenizer 未设置时生效。
	TokenCounter func(mdl.Message) int

	// ModelConfig 摘要使用的模型配置（为空时复用 session 模型）。
	ModelConfig *mdl.ModelConfig
}

func (c SummarizeConfig) maxContextTokens() int {
	if c.MaxContextTokens <= 0 {
		return 80000
	}
	return c.MaxContextTokens
}

func (c SummarizeConfig) keepRecent() int {
	if c.KeepRecent <= 0 {
		return 20
	}
	return c.KeepRecent
}

func (c SummarizeConfig) maxSummaryTokens() int {
	if c.MaxSummaryTokens <= 0 {
		return 800
	}
	return c.MaxSummaryTokens
}

func (c SummarizeConfig) countTokens(msg mdl.Message) int {
	if c.Tokenizer != nil {
		return c.Tokenizer.CountMessage(msg)
	}
	if c.TokenCounter != nil {
		return c.TokenCounter(msg)
	}
	return estimateTokens(msg)
}

func (c SummarizeConfig) summaryPrompt() string {
	if c.SummaryPrompt != "" {
		return c.SummaryPrompt
	}
	return defaultSummaryPrompt
}

func (c SummarizeConfig) enabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

const defaultSummaryPrompt = `请将以下对话历史压缩为简洁摘要，重点保留：
1. 用户的核心目标和约束条件
2. 已做出的重要决策
3. 已执行的关键操作及其结果
4. 遇到的错误及解决方式
5. 当前进度状态

输出纯文本，不超过500词。`

// AutoSummarize 构造对话历史摘要压缩 middleware。
// 在每次 LLM 调用前检查历史长度，超过阈值时将旧历史压缩为摘要消息，
// 以保留更多语义信息（相比 AutoTruncate 的直接丢弃）。
//
// 内置摘要缓存：对相同消息集不重复调用 LLM，缓存最近 8 条摘要记录。
//
// 用法：
//
//	k := kernel.New(kernel.Use(builtins.AutoSummarize(builtins.SummarizeConfig{
//	    LLM:              myLLM,
//	    MaxContextTokens: 100000,
//	    KeepRecent:       20,
//	})))
func AutoSummarize(cfg SummarizeConfig) middleware.Middleware {
	// 摘要缓存：hash(待压缩消息) → 摘要文本，最多保留 8 条。
	type cacheEntry struct {
		hash    uint64
		summary string
	}
	var (
		cacheMu sync.Mutex
		cache   []cacheEntry
	)
	const maxCacheSize = 8

	getCached := func(hash uint64) (string, bool) {
		cacheMu.Lock()
		defer cacheMu.Unlock()
		for _, e := range cache {
			if e.hash == hash {
				return e.summary, true
			}
		}
		return "", false
	}
	putCached := func(hash uint64, summary string) {
		cacheMu.Lock()
		defer cacheMu.Unlock()
		// 已存在则更新
		for i, e := range cache {
			if e.hash == hash {
				cache[i].summary = summary
				return
			}
		}
		if len(cache) >= maxCacheSize {
			cache = cache[1:] // 淘汰最旧记录
		}
		cache = append(cache, cacheEntry{hash: hash, summary: summary})
	}

	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if !cfg.enabled() {
			return next(ctx)
		}
		if mc.Phase != middleware.BeforeLLM {
			return next(ctx)
		}
		if cfg.LLM == nil || mc.Session == nil {
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

		// 分离 system 消息和对话消息
		var systemMsgs, dialogMsgs []mdl.Message
		for _, m := range msgs {
			if m.Role == mdl.RoleSystem {
				systemMsgs = append(systemMsgs, m)
			} else {
				dialogMsgs = append(dialogMsgs, m)
			}
		}

		keepRecent := cfg.keepRecent()
		if keepRecent >= len(dialogMsgs) {
			// 无可压缩内容，降级到截断通知
			return next(ctx)
		}

		toCompress := dialogMsgs[:len(dialogMsgs)-keepRecent]
		recentMsgs := dialogMsgs[len(dialogMsgs)-keepRecent:]

		// 查缓存，命中则跳过 LLM 调用
		msgHash := mdl.HashMessages(toCompress)
		summaryText, cached := getCached(msgHash)
		if !cached {
			var err error
			summaryText, err = generateSummary(ctx, cfg, toCompress)
			if err != nil {
				// 摘要生成失败时降级：插入截断通知，继续执行
				notice := buildTruncationNotice(len(toCompress), totalTokens, cfg.maxContextTokens())
				summaryText = "[摘要生成失败，已截断] " + notice
			} else {
				putCached(msgHash, summaryText)
			}
		}

		summaryMsg := mdl.Message{
			Role: mdl.RoleSystem,
			ContentParts: []mdl.ContentPart{
				mdl.TextPart(buildSummaryNotice(summaryText, len(toCompress))),
			},
		}

		newMsgs := make([]mdl.Message, 0, len(systemMsgs)+1+len(recentMsgs))
		newMsgs = append(newMsgs, systemMsgs...)
		newMsgs = append(newMsgs, summaryMsg)
		newMsgs = append(newMsgs, recentMsgs...)
		sess.ReplaceMessages(newMsgs)

		return next(ctx)
	}
}

func generateSummary(ctx context.Context, cfg SummarizeConfig, msgs []mdl.Message) (string, error) {
	if len(msgs) == 0 {
		return "", nil
	}

	// 将待压缩对话格式化为文本
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, mdl.ContentPartsToPlainText(m.ContentParts)))
		for _, tc := range m.ToolCalls {
			sb.WriteString(fmt.Sprintf("[tool_call:%s]: %s\n", tc.Name, string(tc.Arguments)))
		}
		for _, tr := range m.ToolResults {
			sb.WriteString(fmt.Sprintf("[tool_result]: %s\n", mdl.ContentPartsToPlainText(tr.ContentParts)))
		}
	}

	modelCfg := mdl.ModelConfig{}
	if cfg.ModelConfig != nil {
		modelCfg = *cfg.ModelConfig
	}
	if modelCfg.MaxTokens <= 0 {
		modelCfg.MaxTokens = cfg.maxSummaryTokens()
	}

	req := mdl.CompletionRequest{
		Messages: []mdl.Message{
			{
				Role:         mdl.RoleSystem,
				ContentParts: []mdl.ContentPart{mdl.TextPart(cfg.summaryPrompt())},
			},
			{
				Role:         mdl.RoleUser,
				ContentParts: []mdl.ContentPart{mdl.TextPart("对话历史：\n" + sb.String())},
			},
		},
		Config: modelCfg,
	}

	resp, err := cfg.LLM.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	return mdl.ContentPartsToPlainText(resp.Message.ContentParts), nil
}

func buildSummaryNotice(summary string, compressedCount int) string {
	return fmt.Sprintf("[对话历史摘要（已压缩 %d 条消息）]\n%s", compressedCount, summary)
}
