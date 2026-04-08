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

// appendMemoryContext 将记忆注入追加到 system message。
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
		// 追加到现有 system message 末尾
		existing := mdl.ContentPartsToPlainText(newMsgs[lastSystemIdx].ContentParts)
		combined := strings.TrimRight(existing, "\n") + "\n\n" + injected
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
