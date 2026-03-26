package session

import "github.com/mossagents/moss/kernel/port"

// LastNDialogMessages 返回最近 n 条非 system 消息（保持原始顺序）。
func LastNDialogMessages(messages []port.Message, n int) []port.Message {
	if n <= 0 {
		return nil
	}
	dialog := make([]port.Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == port.RoleSystem {
			continue
		}
		dialog = append(dialog, m)
	}
	if n >= len(dialog) {
		return append([]port.Message(nil), dialog...)
	}
	return append([]port.Message(nil), dialog[len(dialog)-n:]...)
}

// BuildCompactedMessages 构造压缩后的消息序列：
// 1) 保留全部 system 消息
// 2) 插入一个压缩说明 system 消息
// 3) 追加最近 keepRecent 条非 system 消息
func BuildCompactedMessages(messages []port.Message, keepRecent int, notice string) []port.Message {
	var out []port.Message
	for _, m := range messages {
		if m.Role == port.RoleSystem {
			out = append(out, m)
		}
	}
	if notice != "" {
		out = append(out, port.Message{
			Role:    port.RoleSystem,
			Content: notice,
		})
	}
	out = append(out, LastNDialogMessages(messages, keepRecent)...)
	return out
}
