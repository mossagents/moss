package model

import "hash/fnv"

// Tokenizer 对消息或字符串进行 token 数估算。
// 可注入到压缩 middleware，替换默认的字符/4 估算。
//
// 实现建议：
//   - 对于精确场景，使用模型原生 tokenizer（如 tiktoken）
//   - 对于轻量场景，使用 SimpleTokenizer（字符/4 估算，误差约 10-30%）
type Tokenizer interface {
	// CountMessage 返回单条消息的 token 数估算。
	CountMessage(msg Message) int
	// CountString 返回单个字符串的 token 数估算。
	CountString(s string) int
}

// SimpleTokenizer 基于字符数估算 token（1 token ≈ 4 chars）。
// 对英文误差约 10%，对中文误差约 30-50%（中文每字符约 1-2 token）。
// 适用于无法获取精确 tokenizer 的场景，作为合理的保守估算。
type SimpleTokenizer struct{}

// CountMessage 估算单条消息的 token 数。
func (SimpleTokenizer) CountMessage(msg Message) int {
	total := len(ContentPartsToPlainText(msg.ContentParts)) / 4
	for _, tc := range msg.ToolCalls {
		total += len(tc.Name)/4 + len(string(tc.Arguments))/4
	}
	for _, tr := range msg.ToolResults {
		total += len(ContentPartsToPlainText(tr.ContentParts)) / 4
	}
	// 保证非空消息至少计 1 token
	if total < 1 && (len(msg.ContentParts) > 0 || len(msg.ToolCalls) > 0 || len(msg.ToolResults) > 0) {
		total = 1
	}
	return total
}

// CountString 估算字符串的 token 数。
func (SimpleTokenizer) CountString(s string) int {
	n := len(s) / 4
	if n < 1 && len(s) > 0 {
		return 1
	}
	return n
}

// CountMessages 汇总一组消息的总 token 数，方便批量调用。
func (t SimpleTokenizer) CountMessages(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += t.CountMessage(m)
	}
	return total
}

// FuncTokenizer 将 func(Message) int 适配为 Tokenizer 接口。
// 用于将已有的自定义 token 计数函数迁移到新接口，保持向后兼容。
//
// 用法：
//
//	tok := model.FuncTokenizer{Fn: func(m model.Message) int { return myCounter(m) }}
type FuncTokenizer struct {
	Fn func(Message) int
}

// CountMessage 调用 Fn 计算单条消息的 token 数。
func (f FuncTokenizer) CountMessage(msg Message) int {
	if f.Fn == nil {
		return (SimpleTokenizer{}).CountMessage(msg)
	}
	return f.Fn(msg)
}

// CountString 将字符串包装为临时消息后调用 Fn，使计数行为与 Fn 一致。
func (f FuncTokenizer) CountString(s string) int {
	if f.Fn == nil {
		return (SimpleTokenizer{}).CountString(s)
	}
	return f.Fn(Message{
		ContentParts: []ContentPart{TextPart(s)},
	})
}

// HashMessages 对消息序列内容做 FNV-64a 哈希，用于摘要缓存 key。
// 只对消息文本内容哈希，不包括时间戳等易变字段。
func HashMessages(msgs []Message) uint64 {
	h := fnv.New64a()
	for _, m := range msgs {
		_, _ = h.Write([]byte(string(m.Role)))
		_, _ = h.Write([]byte(ContentPartsToPlainText(m.ContentParts)))
		for _, tc := range m.ToolCalls {
			_, _ = h.Write([]byte(tc.Name))
			_, _ = h.Write(tc.Arguments)
		}
		for _, tr := range m.ToolResults {
			_, _ = h.Write([]byte(ContentPartsToPlainText(tr.ContentParts)))
		}
		_, _ = h.Write([]byte{0}) // 消息分隔符
	}
	return h.Sum64()
}
