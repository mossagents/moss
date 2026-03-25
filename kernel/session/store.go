package session

import (
	"context"
	"errors"
)

// ErrNotSupported 表示存储实现不支持该操作。
var ErrNotSupported = errors.New("operation not supported")

// SessionStore 提供 Session 的持久化存储能力。
// 实现应保证并发安全。
type SessionStore interface {
	// Save 持久化一个 Session（创建或更新）。
	Save(ctx context.Context, sess *Session) error

	// Load 按 ID 加载一个 Session。找不到时返回 (nil, nil)。
	Load(ctx context.Context, id string) (*Session, error)

	// List 返回所有已保存 Session 的摘要信息。
	List(ctx context.Context) ([]SessionSummary, error)

	// Delete 删除指定 ID 的 Session。
	Delete(ctx context.Context, id string) error

	// Watch 监听指定 Session 的变更事件。
	// 用于多实例部署时跨节点 Session 状态同步。
	// 不支持时应返回 ErrNotSupported。
	Watch(ctx context.Context, id string) (<-chan *Session, error)
}

// SessionSummary 是 Session 的摘要信息，用于列表展示。
type SessionSummary struct {
	ID        string        `json:"id"`
	Goal      string        `json:"goal"`
	Mode      string        `json:"mode,omitempty"`
	Status    SessionStatus `json:"status"`
	Steps     int           `json:"steps"`
	CreatedAt string        `json:"created_at"`
	EndedAt   string        `json:"ended_at,omitempty"`
}
