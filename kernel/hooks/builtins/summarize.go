package builtins

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/model"
)

// SummarizeConfig 配置对话历史摘要压缩行为。
type SummarizeConfig struct {
	Enabled          *bool
	LLM              model.LLM
	MaxContextTokens int
	KeepRecent       int
	MaxSummaryTokens int
	SummaryPrompt    string
	Tokenizer        model.Tokenizer
	TokenCounter     func(model.Message) int
	ModelConfig      *model.ModelConfig
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

func (c SummarizeConfig) countTokens(msg model.Message) int {
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

// AutoSummarize 构造对话历史摘要压缩 hook。
func AutoSummarize(cfg SummarizeConfig) hooks.Hook[hooks.LLMEvent] {
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
		for i, e := range cache {
			if e.hash == hash {
				cache[i].summary = summary
				return
			}
		}
		if len(cache) >= maxCacheSize {
			cache = cache[1:]
		}
		cache = append(cache, cacheEntry{hash: hash, summary: summary})
	}

	return func(ctx context.Context, ev *hooks.LLMEvent) error {
		if !cfg.enabled() {
			return nil
		}
		if cfg.LLM == nil || ev.Session == nil {
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

		keepRecent := cfg.keepRecent()
		if keepRecent >= len(dialogMsgs) {
			return nil
		}

		toCompress := dialogMsgs[:len(dialogMsgs)-keepRecent]
		recentMsgs := dialogMsgs[len(dialogMsgs)-keepRecent:]

		msgHash := model.HashMessages(toCompress)
		summaryText, cached := getCached(msgHash)
		if !cached {
			var err error
			summaryText, err = generateSummary(ctx, cfg, toCompress)
			if err != nil {
				notice := buildTruncationNotice(len(toCompress), totalTokens, cfg.maxContextTokens())
				summaryText = "[摘要生成失败，已截断] " + notice
			} else {
				putCached(msgHash, summaryText)
			}
		}

		summaryMsg := model.Message{
			Role: model.RoleSystem,
			ContentParts: []model.ContentPart{
				model.TextPart(buildSummaryNotice(summaryText, len(toCompress))),
			},
		}

		newMsgs := make([]model.Message, 0, len(systemMsgs)+1+len(recentMsgs))
		newMsgs = append(newMsgs, systemMsgs...)
		newMsgs = append(newMsgs, summaryMsg)
		newMsgs = append(newMsgs, recentMsgs...)
		sess.ReplaceMessages(newMsgs)

		return nil
	}
}

func generateSummary(ctx context.Context, cfg SummarizeConfig, msgs []model.Message) (string, error) {
	if len(msgs) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, model.ContentPartsToPlainText(m.ContentParts)))
		for _, tc := range m.ToolCalls {
			sb.WriteString(fmt.Sprintf("[tool_call:%s]: %s\n", tc.Name, string(tc.Arguments)))
		}
		for _, tr := range m.ToolResults {
			sb.WriteString(fmt.Sprintf("[tool_result]: %s\n", model.ContentPartsToPlainText(tr.ContentParts)))
		}
	}

	modelCfg := model.ModelConfig{}
	if cfg.ModelConfig != nil {
		modelCfg = *cfg.ModelConfig
	}
	if modelCfg.MaxTokens <= 0 {
		modelCfg.MaxTokens = cfg.maxSummaryTokens()
	}

	req := model.CompletionRequest{
		Messages: []model.Message{
			{
				Role:         model.RoleSystem,
				ContentParts: []model.ContentPart{model.TextPart(cfg.summaryPrompt())},
			},
			{
				Role:         model.RoleUser,
				ContentParts: []model.ContentPart{model.TextPart("对话历史：\n" + sb.String())},
			},
		},
		Config: modelCfg,
	}

	resp, err := cfg.LLM.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	return model.ContentPartsToPlainText(resp.Message.ContentParts), nil
}

func buildSummaryNotice(summary string, compressedCount int) string {
	return fmt.Sprintf("[对话历史摘要（已压缩 %d 条消息）]\n%s", compressedCount, summary)
}
