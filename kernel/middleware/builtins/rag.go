package builtins

import (
	"context"
	"strings"

	"github.com/mossagents/moss/kernel/middleware"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/knowledge"
)

// RAGConfig 配置 RAG（检索增强生成）注入行为。
type RAGConfig struct {
	// Enabled 控制 middleware 是否启用；nil/true=启用，false=禁用。
	Enabled *bool

	// Manager 提供三层记忆检索能力（必须提供）。
	Manager *knowledge.MemoryManager

	// MaxChars 注入内容的最大字符数，默认 4000。
	MaxChars int

	// EpisodicN 最近事件数，默认 10。
	EpisodicN int

	// SemanticK 语义检索结果数，默认 5。
	SemanticK int

	// Threshold 相似度阈值，默认 0.7。
	Threshold float64

	// QueryExtractor 从最后一条 user message 提取检索查询文本。
	// 默认从最后一条用户消息提取纯文本。
	QueryExtractor func(msgs []mdl.Message) string
}

func (c RAGConfig) maxChars() int {
	if c.MaxChars <= 0 {
		return 4000
	}
	return c.MaxChars
}

func (c RAGConfig) enabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// RAG 构造 RAG 注入 middleware。
// 在每次 LLM 调用前（BeforeLLM 阶段），检索三层记忆并将结果追加到 system message。
//
// 用法：
//
//	k := kernel.New(kernel.Use(builtins.RAG(builtins.RAGConfig{
//	    Manager: memoryManager,
//	})))
func RAG(cfg RAGConfig) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if !cfg.enabled() {
			return next(ctx)
		}
		if mc.Phase != middleware.BeforeLLM {
			return next(ctx)
		}
		if cfg.Manager == nil || mc.Session == nil {
			return next(ctx)
		}

		sess := mc.Session
		msgs := sess.CopyMessages()

		// 提取检索查询
		query := extractQuery(msgs, cfg.QueryExtractor)

		injected, err := cfg.Manager.Inject(ctx, knowledge.InjectConfig{
			SessionID: sess.ID,
			Query:     query,
			EpisodicN: cfg.EpisodicN,
			SemanticK: cfg.SemanticK,
			Threshold: cfg.Threshold,
			MaxChars:  cfg.maxChars(),
		})
		if err != nil || injected == "" {
			return next(ctx)
		}

		// 将记忆注入附加到最后一条 system message，若无 system message 则新增一条
		appendMemoryContext(sess, msgs, injected)

		return next(ctx)
	}
}

// extractQuery 从消息列表中提取最后一条 user message 的文本作为检索查询。
func extractQuery(msgs []mdl.Message, extractor func([]mdl.Message) string) string {
	if extractor != nil {
		return extractor(msgs)
	}
	// 默认：取最后一条 user 消息的纯文本
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == mdl.RoleUser {
			text := mdl.ContentPartsToPlainText(msgs[i].ContentParts)
			if text != "" {
				// 截断过长的查询
				if len(text) > 512 {
					text = text[:512]
				}
				return text
			}
		}
	}
	return ""
}

// appendMemoryContext 将记忆注入写入 system message，替换上一轮注入的旧内容。
//
// 每次调用都先从目标 system message 中移除已有的 <memory_context>…</memory_context>
// 块，再追加本轮最新内容，避免多轮累积导致 token 持续膨胀。
func appendMemoryContext(sess interface {
	CopyMessages() []mdl.Message
	ReplaceMessages([]mdl.Message)
}, msgs []mdl.Message, injected string) {
	newMsgs := make([]mdl.Message, len(msgs))
	copy(newMsgs, msgs)

	// 找到最后一条 system message
	lastSystemIdx := -1
	for i, m := range newMsgs {
		if m.Role == mdl.RoleSystem {
			lastSystemIdx = i
		}
	}

	if lastSystemIdx >= 0 {
		// 先移除上一轮写入的 <memory_context> 块，再追加本轮内容。
		existing := mdl.ContentPartsToPlainText(newMsgs[lastSystemIdx].ContentParts)
		base := stripMemoryContext(existing)
		combined := strings.TrimRight(base, "\n") + "\n\n" + injected
		newMsgs[lastSystemIdx] = mdl.Message{
			Role:         mdl.RoleSystem,
			ContentParts: []mdl.ContentPart{mdl.TextPart(combined)},
		}
	} else {
		// 在消息列表开头插入 system message
		injectedMsg := mdl.Message{
			Role:         mdl.RoleSystem,
			ContentParts: []mdl.ContentPart{mdl.TextPart(injected)},
		}
		newMsgs = append([]mdl.Message{injectedMsg}, newMsgs...)
	}

	sess.ReplaceMessages(newMsgs)
}

// stripMemoryContext 移除文本中所有完整或不完整的 <memory_context>…</memory_context> 块
// 及其前置换行/空格，使重复调用不会累积。
func stripMemoryContext(text string) string {
	const open = "<memory_context>"
	const close = "</memory_context>"
	for {
		start := strings.Index(text, open)
		if start < 0 {
			break
		}
		end := strings.Index(text[start:], close)
		if end < 0 {
			// 未闭合块：截断到 open 处。
			text = strings.TrimRight(text[:start], "\n ")
			break
		}
		end = start + end + len(close)
		// 去掉 open 前的换行/空格，保留其余内容。
		text = strings.TrimRight(text[:start], "\n ") + text[end:]
	}
	return text
}
