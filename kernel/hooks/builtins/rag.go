package builtins

import (
	"context"
	"strings"

	"github.com/mossagents/moss/extensions/knowledge"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/model"
)

// RAGConfig 配置 RAG（检索增强生成）注入行为。
type RAGConfig struct {
	Enabled        *bool
	Manager        *knowledge.MemoryManager
	MaxChars       int
	EpisodicN      int
	SemanticK      int
	Threshold      float64
	QueryExtractor func(msgs []model.Message) string
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

// RAG 构造 RAG 注入 hook。
// 在每次 LLM 调用前检索三层记忆并将结果追加到 system message。
func RAG(cfg RAGConfig) hooks.Hook[hooks.LLMEvent] {
	return func(ctx context.Context, ev *hooks.LLMEvent) error {
		if !cfg.enabled() {
			return nil
		}
		if cfg.Manager == nil || ev.Session == nil {
			return nil
		}

		sess := ev.Session
		msgs := sess.CopyMessages()

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
			return nil
		}

		appendMemoryContext(sess, msgs, injected)

		return nil
	}
}

func extractQuery(msgs []model.Message, extractor func([]model.Message) string) string {
	if extractor != nil {
		return extractor(msgs)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleUser {
			text := model.ContentPartsToPlainText(msgs[i].ContentParts)
			if text != "" {
				if len(text) > 512 {
					text = text[:512]
				}
				return text
			}
		}
	}
	return ""
}

func appendMemoryContext(sess interface {
	CopyMessages() []model.Message
	ReplaceMessages([]model.Message)
}, msgs []model.Message, injected string) {
	newMsgs := make([]model.Message, len(msgs))
	copy(newMsgs, msgs)

	lastSystemIdx := -1
	for i, m := range newMsgs {
		if m.Role == model.RoleSystem {
			lastSystemIdx = i
		}
	}

	if lastSystemIdx >= 0 {
		existing := model.ContentPartsToPlainText(newMsgs[lastSystemIdx].ContentParts)
		base := stripMemoryContext(existing)
		combined := strings.TrimRight(base, "\n") + "\n\n" + injected
		newMsgs[lastSystemIdx] = model.Message{
			Role:         model.RoleSystem,
			ContentParts: []model.ContentPart{model.TextPart(combined)},
		}
	} else {
		injectedMsg := model.Message{
			Role:         model.RoleSystem,
			ContentParts: []model.ContentPart{model.TextPart(injected)},
		}
		newMsgs = append([]model.Message{injectedMsg}, newMsgs...)
	}

	sess.ReplaceMessages(newMsgs)
}

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
			text = strings.TrimRight(text[:start], "\n ")
			break
		}
		end = start + end + len(close)
		text = strings.TrimRight(text[:start], "\n ") + text[end:]
	}
	return text
}
