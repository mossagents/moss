// Package port 定义 Channel 通道接口。
//
// Channel 抽象了 Agent 的消息入口：CLI 终端、WebChat、Telegram Bot、Webhook 等。
// Kernel 不关心消息从哪来到哪去，只通过 Channel 接口收发。
package channel

import (
	"context"
)

// Channel 是 Agent 消息通道的抽象接口。
// 实现者负责对接具体的传输协议 (CLI stdin/stdout、WebSocket、Telegram API 等)。
type Channel interface {
	// Name 返回通道标识，如 "cli", "telegram", "webhook"。
	Name() string

	// Receive 返回入站消息通道。ctx 取消时 channel 应被 close。
	Receive(ctx context.Context) <-chan InboundMessage

	// Send 向指定目标发送出站消息。
	Send(ctx context.Context, msg OutboundMessage) error

	// Close 关闭通道，释放资源。
	Close() error
}

// InboundMessage 是从外部通道到达的消息。
type InboundMessage struct {
	// ChannelName 消息来源通道名称。
	ChannelName string `json:"channel"`

	// SenderID 发送者标识（电话号码、用户ID、"cli" 等）。
	SenderID string `json:"sender_id"`

	// SessionHint 通道建议的 session 路由键。
	// 空值表示让 Router 按默认策略决定。
	SessionHint string `json:"session_hint,omitempty"`

	// Content 消息文本内容。
	Content string `json:"content"`

	// Metadata 通道特定的元数据。
	Metadata map[string]any `json:"metadata,omitempty"`
}

// OutboundMessage 是发送到外部通道的消息。
type OutboundMessage struct {
	// To 目标标识（发送者 ID、群组 ID 等）。
	To string `json:"to"`

	// Content 消息文本内容。
	Content string `json:"content"`

	// Metadata 通道特定的元数据。
	Metadata map[string]any `json:"metadata,omitempty"`
}
