package session

import (
	"github.com/mossagents/moss/kernel/model"
)

// LastNDialogMessages 返回最近 n 条非 system 消息（保持原始顺序）。
func LastNDialogMessages(messages []model.Message, n int) []model.Message {
	if n <= 0 {
		return nil
	}
	dialog := make([]model.Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == model.RoleSystem {
			continue
		}
		dialog = append(dialog, m)
	}
	if n >= len(dialog) {
		return append([]model.Message(nil), dialog...)
	}
	return append([]model.Message(nil), dialog[len(dialog)-n:]...)
}

// BuildCompactedMessages 构造压缩后的消息序列：
// 1) 保留全部 system 消息
// 2) 插入一个压缩说明 system 消息
// 3) 追加最近 keepRecent 条非 system 消息
func BuildCompactedMessages(messages []model.Message, keepRecent int, notice string) []model.Message {
	var out []model.Message
	for _, m := range messages {
		if m.Role == model.RoleSystem {
			out = append(out, m)
		}
	}
	if notice != "" {
		out = append(out, model.Message{
			Role:         model.RoleSystem,
			ContentParts: []model.ContentPart{model.TextPart(notice)},
		})
	}
	out = append(out, LastNDialogMessages(messages, keepRecent)...)
	return out
}
