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
}

// WatchableSessionStore 扩展 SessionStore，支持多实例部署下的
// 跨节点 Session 变更订阅（如 Redis pub/sub、etcd Watch 等）。
// 文件存储等不支持 Watch 的实现无需实现此接口。
type WatchableSessionStore interface {
	SessionStore
	// Watch 监听指定 Session 的变更事件。
	// 不支持时应返回 ErrNotSupported。
	Watch(ctx context.Context, id string) (<-chan *Session, error)
}

// SessionSummary 是 Session 的摘要信息，用于列表展示。
type SessionSummary struct {
	ID          string        `json:"id"`
	Goal        string        `json:"goal"`
	Mode        string        `json:"mode,omitempty"`
	Profile     string        `json:"profile,omitempty"`
	EffectiveTrust string     `json:"effective_trust,omitempty"`
	EffectiveApproval string  `json:"effective_approval,omitempty"`
	TaskMode    string        `json:"task_mode,omitempty"`
	Status      SessionStatus `json:"status"`
	Recoverable bool          `json:"recoverable,omitempty"`
	Steps       int           `json:"steps"`
	CreatedAt   string        `json:"created_at"`
	EndedAt     string        `json:"ended_at,omitempty"`
}

// IsRecoverableStatus 判断给定状态的 Session 是否适合作为 resume 候选。
func IsRecoverableStatus(status SessionStatus) bool {
	switch status {
	case StatusCreated, StatusRunning, StatusPaused, StatusFailed:
		return true
	default:
		return false
	}
}
