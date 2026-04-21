package runtime

import (
	"context"
	"errors"
)

// ─────────────────────────────────────────────
// 公共错误
// ─────────────────────────────────────────────

// ErrSeqConflict 表示 AppendEvents 的 optimistic concurrency 校验失败。
var ErrSeqConflict = errors.New("seq conflict: expected_seq does not match current seq")

// ErrSessionNotFound 表示指定 session 不存在。
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionEnded 表示向已结束（session_completed / session_failed）的 session 追加事件。
var ErrSessionEnded = errors.New("session has ended, cannot append events")

// ErrNoBlueprintVersion 表示可恢复事件缺少 blueprint version（§10 一致性要求）。
var ErrNoBlueprintVersion = errors.New("recoverable event missing blueprint_version")

// ─────────────────────────────────────────────
// ResumeCandidateFilter
// ─────────────────────────────────────────────

// ResumeCandidateFilter 用于 ListResumeCandidates 的过滤条件。
type ResumeCandidateFilter struct {
	// WorkspaceID 若非空，只返回该工作区下的 session。
	WorkspaceID string
	// AgentName 若非空，只返回该 agent 名下的 session。
	AgentName string
	// Limit 最多返回多少条，0 表示不限。
	Limit int
}

// ResumeCandidate 是可恢复 session 的摘要信息。
type ResumeCandidate struct {
	SessionID    string `json:"session_id"`
	AgentName    string `json:"agent_name,omitempty"`
	WorkspaceID  string `json:"workspace_id,omitempty"`
	LastSeq      int64  `json:"last_seq"`
	BlueprintRef string `json:"blueprint_ref,omitempty"`
	// Status 最后一个已知状态（running / paused / failed）。
	Status string `json:"status"`
}

// ExportFormat 标识导出格式。
type ExportFormat string

const (
	ExportFormatJSONL ExportFormat = "jsonl"
	ExportFormatJSON  ExportFormat = "json"
)

// ─────────────────────────────────────────────
// EventStore 接口（§10）
// ─────────────────────────────────────────────

// EventStore 是唯一允许写入和读取运行时事实的接口。
// 所有实现必须满足 §10 的一致性要求。
//
// 使用规则：
//   - 运行时不允许从 projection 反写事实层；
//   - 不允许绕过 EventStore 接口直接访问底层存储；
//   - 同一 session 不允许混用两种实现。
type EventStore interface {
	// AppendEvents 原子追加一批事件到指定 session。
	//
	// expectedSeq 用于 optimistic concurrency 校验：
	//   - 若当前 session 最新 seq != expectedSeq，返回 ErrSeqConflict；
	//   - 新建 session 传 expectedSeq=0；
	//   - 一次调用可追加多条事件，seq 由实现连续递增分配，调用方不得预设。
	//
	// requestID 用于幂等保障：相同 requestID 的调用只写入一次。
	//
	// 注意：session_failed 或 session_completed 之后不允许再追加，返回 ErrSessionEnded。
	AppendEvents(ctx context.Context, sessionID string, expectedSeq int64, requestID string, events []RuntimeEvent) error

	// LoadEvents 加载指定 session 在 afterSeq 之后的所有事件（不含 afterSeq）。
	// afterSeq=0 表示加载全部事件。
	LoadEvents(ctx context.Context, sessionID string, afterSeq int64) ([]RuntimeEvent, error)

	// LoadSessionView 返回指定 session 的当前物化视图（MaterializedState）。
	// 若视图尚未构建，实现可触发全量 replay 后返回。
	LoadSessionView(ctx context.Context, sessionID string) (*MaterializedState, error)

	// RebuildProjections 强制重建指定 session 的所有 projection。
	// 实现可以全量 replay 事件流，不得从 projection 反写事实层。
	RebuildProjections(ctx context.Context, sessionID string) error

	// ListResumeCandidates 返回满足 filter 条件的可恢复 session 摘要。
	ListResumeCandidates(ctx context.Context, filter ResumeCandidateFilter) ([]ResumeCandidate, error)

	// Export 将指定 session 的事件流导出为指定格式。
	// 用于 JSONL / audit 输出（§10）。
	Export(ctx context.Context, sessionID string, format ExportFormat) ([]byte, error)

	// Import 从外部数据导入事件流到指定 session。
	// 用于 JSONL 导入（§10）。
	Import(ctx context.Context, sessionID string, data []byte, format ExportFormat) error

	// SubscribeEvents 流式订阅指定 session 在 afterSeq 之后产生的新事件。
	// 这是集群化预留扩展方法（§10、§15.2）；
	// 单节点实现可返回 ErrNotSupported，但接口不得移除此方法。
	SubscribeEvents(ctx context.Context, sessionID string, afterSeq int64) (<-chan RuntimeEvent, error)
}

// ErrNotSupported 表示该操作在当前实现中不支持（如单节点实现的 SubscribeEvents）。
var ErrNotSupported = errors.New("operation not supported by this EventStore implementation")
