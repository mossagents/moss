package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // SQLite driver（纯 Go，无 CGO）
)

// ─────────────────────────────────────────────
// SQLiteEventStore
// ─────────────────────────────────────────────

// SQLiteEventStore 是基于 SQLite 的 EventStore 实现。
// 设计原则（§10）：
//   - WAL 模式以支持并发读写；
//   - optimistic concurrency 通过 expected_seq 与 MAX(seq) 比较；
//   - 幂等保障通过 request_id 唯一索引；
//   - 单事务原子追加多条事件；
//   - 不允许直接修改 session_events 表中的已有行。
type SQLiteEventStore struct {
	db         *sql.DB
	projection *ProjectionEngine
}

// NewSQLiteEventStore 打开（或创建）指定 DSN 的 SQLite 数据库，
// 初始化表结构并返回 SQLiteEventStore。
// dsn 示例："/path/to/events.db" 或 "file:/path/to/events.db?cache=shared"
func NewSQLiteEventStore(dsn string) (*SQLiteEventStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// 连接池：SQLite 在 WAL 模式下建议使用单个写连接
	db.SetMaxOpenConns(1)

	store := &SQLiteEventStore{
		db:         db,
		projection: NewProjectionEngine(),
	}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return store, nil
}

// Close 关闭底层数据库连接。
func (s *SQLiteEventStore) Close() error {
	return s.db.Close()
}

// ─────────────────────────────────────────────
// migrate：建表 + pragma
// ─────────────────────────────────────────────

func (s *SQLiteEventStore) migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA foreign_keys=ON`,
		// sessions 表：记录每个 session 的最新 seq 和状态
		`CREATE TABLE IF NOT EXISTS sessions (
			session_id   TEXT PRIMARY KEY,
			last_seq     INTEGER NOT NULL DEFAULT 0,
			status       TEXT NOT NULL DEFAULT 'running',
			blueprint_version TEXT,
			created_at   TEXT NOT NULL,
			updated_at   TEXT NOT NULL
		)`,
		// session_events 表：不可变事件追加日志
		`CREATE TABLE IF NOT EXISTS session_events (
			session_id  TEXT NOT NULL,
			seq         INTEGER NOT NULL,
			event_type  TEXT NOT NULL,
			request_id  TEXT,
			payload_json TEXT,
			blueprint_version TEXT,
			timestamp   TEXT NOT NULL,
			PRIMARY KEY (session_id, seq)
		)`,
		// request_id 幂等索引（§10）
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_events_request_id
			ON session_events(request_id)
			WHERE request_id IS NOT NULL`,
		// session_views 表：物化视图缓存
		`CREATE TABLE IF NOT EXISTS session_views (
			session_id      TEXT PRIMARY KEY,
			view_seq        INTEGER NOT NULL,
			view_json       TEXT NOT NULL,
			updated_at      TEXT NOT NULL
		)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:min(40, len(stmt))], err)
		}
	}
	return nil
}

// ─────────────────────────────────────────────
// AppendEvents（§10：原子追加 + optimistic concurrency + 幂等）
// ─────────────────────────────────────────────

func (s *SQLiteEventStore) AppendEvents(
	ctx context.Context,
	sessionID string,
	expectedSeq int64,
	requestID string,
	events []RuntimeEvent,
) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// 1. 幂等检查：request_id 是否已存在
	if requestID != "" {
		var cnt int
		err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM session_events WHERE request_id = ?`, requestID,
		).Scan(&cnt)
		if err != nil {
			return fmt.Errorf("idempotency check: %w", err)
		}
		if cnt > 0 {
			// 已处理，幂等返回
			return tx.Commit()
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// 2. Upsert sessions 行（初始化或获取当前 seq）
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sessions (session_id, last_seq, status, created_at, updated_at)
		VALUES (?, 0, 'running', ?, ?)
		ON CONFLICT(session_id) DO NOTHING
	`, sessionID, now, now)
	if err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}

	// 3. 读取当前 last_seq 和 status（加行锁）
	var currentSeq int64
	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT last_seq, status FROM sessions WHERE session_id = ?`, sessionID,
	).Scan(&currentSeq, &status)
	if err != nil {
		return fmt.Errorf("fetch session: %w", err)
	}

	// 4. 不允许向已结束的 session 追加
	if status == "completed" || status == "failed" {
		return ErrSessionEnded
	}

	// 5. Optimistic concurrency 校验
	if currentSeq != expectedSeq {
		return fmt.Errorf("%w: expected=%d current=%d", ErrSeqConflict, expectedSeq, currentSeq)
	}

	// 6. 逐条插入事件，seq 连续递增
	nextSeq := currentSeq + 1
	var lastStatus string
	for i, ev := range events {
		payloadJSON, err := json.Marshal(wrapPayload(ev))
		if err != nil {
			return fmt.Errorf("marshal payload[%d]: %w", i, err)
		}
		ts := ev.Timestamp
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		// requestID 仅写入批次第一条事件行，作为幂等键的代表（§10）
		// 其余事件 request_id 为 NULL，避免触发 UNIQUE 约束
		var rowRequestID any
		if i == 0 {
			rowRequestID = nullableString(requestID)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO session_events
				(session_id, seq, event_type, request_id, payload_json, blueprint_version, timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`,
			sessionID,
			nextSeq,
			string(ev.Type),
			rowRequestID,
			string(payloadJSON),
			nullableString(ev.BlueprintVersion),
			ts.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("insert event seq=%d: %w", nextSeq, err)
		}
		lastStatus = sessionStatusFromEvent(ev.Type)
		nextSeq++
	}

	// 7. 更新 sessions.last_seq 和 status
	finalSeq := nextSeq - 1
	finalStatus := lastStatus
	if finalStatus == "" {
		finalStatus = status
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE sessions SET last_seq=?, status=?, updated_at=? WHERE session_id=?
	`, finalSeq, finalStatus, now, sessionID)
	if err != nil {
		return fmt.Errorf("update session seq: %w", err)
	}

	return tx.Commit()
}

// ─────────────────────────────────────────────
// LoadEvents（§10）
// ─────────────────────────────────────────────

func (s *SQLiteEventStore) LoadEvents(
	ctx context.Context,
	sessionID string,
	afterSeq int64,
) ([]RuntimeEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, event_type, request_id, payload_json, blueprint_version, timestamp
		FROM session_events
		WHERE session_id = ? AND seq > ?
		ORDER BY seq ASC
	`, sessionID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []RuntimeEvent
	for rows.Next() {
		var (
			seq              int64
			evType           string
			requestID        sql.NullString
			payloadJSON      string
			blueprintVersion sql.NullString
			tsStr            string
		)
		if err := rows.Scan(&seq, &evType, &requestID, &payloadJSON, &blueprintVersion, &tsStr); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		ts, _ := time.Parse(time.RFC3339Nano, tsStr)
		ev := RuntimeEvent{
			SessionID:        sessionID,
			Seq:              seq,
			Timestamp:        ts,
			Type:             EventType(evType),
			RequestID:        requestID.String,
			BlueprintVersion: blueprintVersion.String,
		}
		// 反序列化 payload
		ev.Payload, err = unwrapPayload(EventType(evType), []byte(payloadJSON))
		if err != nil {
			return nil, fmt.Errorf("unwrap payload seq=%d: %w", seq, err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return events, nil
}

// ─────────────────────────────────────────────
// LoadSessionView（§10）
// ─────────────────────────────────────────────

func (s *SQLiteEventStore) LoadSessionView(
	ctx context.Context,
	sessionID string,
) (*MaterializedState, error) {
	// 尝试从缓存读取
	var viewSeq int64
	var viewJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT view_seq, view_json FROM session_views WHERE session_id = ?
	`, sessionID).Scan(&viewSeq, &viewJSON)

	var cached *MaterializedState
	if err == nil {
		cached = &MaterializedState{}
		if jsonErr := json.Unmarshal([]byte(viewJSON), cached); jsonErr != nil {
			cached = nil // 缓存损坏，全量重建
		}
	}

	// 加载 cached.CurrentSeq 之后的增量事件
	var afterSeq int64
	if cached != nil {
		afterSeq = cached.CurrentSeq
	}
	newEvents, err := s.LoadEvents(ctx, sessionID, afterSeq)
	if err != nil {
		return nil, err
	}

	if cached == nil {
		// 全量 replay
		return s.rebuildAndCache(ctx, sessionID)
	}

	if len(newEvents) == 0 {
		return cached, nil
	}

	// 增量 apply
	for _, ev := range newEvents {
		if err := s.projection.Apply(cached, ev); err != nil {
			// 增量失败，降级全量重建
			return s.rebuildAndCache(ctx, sessionID)
		}
	}
	if err := s.persistView(ctx, cached); err != nil {
		// 缓存写失败不影响返回
		_ = err
	}
	return cached, nil
}

// ─────────────────────────────────────────────
// RebuildProjections（§10）
// ─────────────────────────────────────────────

func (s *SQLiteEventStore) RebuildProjections(
	ctx context.Context,
	sessionID string,
) error {
	_, err := s.rebuildAndCache(ctx, sessionID)
	return err
}

func (s *SQLiteEventStore) rebuildAndCache(
	ctx context.Context,
	sessionID string,
) (*MaterializedState, error) {
	events, err := s.LoadEvents(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, ErrSessionNotFound
	}
	state, err := s.projection.Replay(sessionID, events)
	if err != nil {
		return nil, fmt.Errorf("replay: %w", err)
	}
	if err := s.persistView(ctx, state); err != nil {
		_ = err // 缓存写失败不影响返回
	}
	return state, nil
}

func (s *SQLiteEventStore) persistView(ctx context.Context, state *MaterializedState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO session_views (session_id, view_seq, view_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			view_seq=excluded.view_seq,
			view_json=excluded.view_json,
			updated_at=excluded.updated_at
	`, state.SessionID, state.CurrentSeq, string(data), now)
	return err
}

// ─────────────────────────────────────────────
// ListResumeCandidates（§10）
// ─────────────────────────────────────────────

func (s *SQLiteEventStore) ListResumeCandidates(
	ctx context.Context,
	filter ResumeCandidateFilter,
) ([]ResumeCandidate, error) {
	query := `SELECT session_id, last_seq, status, blueprint_version FROM sessions WHERE status NOT IN ('completed', 'failed')`
	args := []any{}

	if filter.Limit > 0 {
		query += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT %d", filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list resume candidates: %w", err)
	}
	defer rows.Close()

	var candidates []ResumeCandidate
	for rows.Next() {
		var (
			sessionID        string
			lastSeq          int64
			status           string
			blueprintVersion sql.NullString
		)
		if err := rows.Scan(&sessionID, &lastSeq, &status, &blueprintVersion); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		candidates = append(candidates, ResumeCandidate{
			SessionID:    sessionID,
			LastSeq:      lastSeq,
			BlueprintRef: blueprintVersion.String,
			Status:       status,
		})
	}
	return candidates, rows.Err()
}

// ─────────────────────────────────────────────
// Export / Import（§10）
// ─────────────────────────────────────────────

func (s *SQLiteEventStore) Export(
	ctx context.Context,
	sessionID string,
	format ExportFormat,
) ([]byte, error) {
	events, err := s.LoadEvents(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	switch format {
	case ExportFormatJSONL:
		return marshalJSONL(events)
	case ExportFormatJSON:
		return json.MarshalIndent(events, "", "  ")
	default:
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
}

func (s *SQLiteEventStore) Import(
	ctx context.Context,
	sessionID string,
	data []byte,
	format ExportFormat,
) error {
	var events []RuntimeEvent
	var err error
	switch format {
	case ExportFormatJSONL:
		events, err = unmarshalJSONL(data)
	case ExportFormatJSON:
		err = json.Unmarshal(data, &events)
	default:
		return fmt.Errorf("unsupported import format: %s", format)
	}
	if err != nil {
		return fmt.Errorf("unmarshal import data: %w", err)
	}
	if len(events) == 0 {
		return nil
	}

	// 以 expectedSeq=0 导入（全新 session）
	// 若 session 已存在，调用方需先清空或选择不同 sessionID
	return s.AppendEvents(ctx, sessionID, 0, "", events)
}

// ─────────────────────────────────────────────
// SubscribeEvents（§10、§15.2 集群化预留）
// ─────────────────────────────────────────────

// SubscribeEvents 在单节点 SQLite 实现中不支持，返回 ErrNotSupported。
// 集群化扩展请实现基于消息队列或分布式日志的 EventStore 变体（§15.2）。
func (s *SQLiteEventStore) SubscribeEvents(
	_ context.Context,
	_ string,
	_ int64,
) (<-chan RuntimeEvent, error) {
	return nil, ErrNotSupported
}

// ─────────────────────────────────────────────
// 内部辅助函数
// ─────────────────────────────────────────────

// payloadEnvelope 用于 JSON 序列化时保留 payload 类型信息。
type payloadEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func wrapPayload(ev RuntimeEvent) payloadEnvelope {
	raw, _ := json.Marshal(ev.Payload)
	return payloadEnvelope{
		Type:    string(ev.Type),
		Payload: raw,
	}
}

func unwrapPayload(evType EventType, data []byte) (any, error) {
	var env payloadEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	target := newPayloadForType(evType)
	if target == nil || len(env.Payload) == 0 || string(env.Payload) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(env.Payload, target); err != nil {
		return nil, fmt.Errorf("unmarshal %s payload: %w", evType, err)
	}
	return target, nil
}

// newPayloadForType 为给定 EventType 返回对应 Payload 的空指针（用于 JSON 反序列化目标）。
func newPayloadForType(evType EventType) any {
	switch evType {
	case EventTypeSessionCreated:
		return &SessionCreatedPayload{}
	case EventTypeTurnStarted:
		return &TurnStartedPayload{}
	case EventTypeTurnCompleted:
		return &TurnCompletedPayload{}
	case EventTypePromptMaterialized:
		return &PromptMaterializedPayload{}
	case EventTypeToolCalled:
		return &ToolCalledPayload{}
	case EventTypeToolCompleted:
		return &ToolCompletedPayload{}
	case EventTypeApprovalRequested:
		return &ApprovalRequestedPayload{}
	case EventTypeApprovalResolved:
		return &ApprovalResolvedPayload{}
	case EventTypePermissionsAmended:
		return &PermissionsAmendedPayload{}
	case EventTypeContextCompacted:
		return &ContextCompactedPayload{}
	case EventTypeSessionForked:
		return &SessionForkedPayload{}
	case EventTypeCheckpointCreated:
		return &CheckpointCreatedPayload{}
	case EventTypeSessionCompleted:
		return &SessionCompletedPayload{}
	case EventTypeSessionFailed:
		return &SessionFailedPayload{}
	case EventTypeTaskStarted:
		return &TaskStartedPayload{}
	case EventTypeTaskCompleted:
		return &TaskCompletedPayload{}
	case EventTypeTaskAbandoned:
		return &TaskAbandonedPayload{}
	case EventTypeRoleTransitioned:
		return &RoleTransitionedPayload{}
	case EventTypePlanUpdated:
		return &PlanUpdatedPayload{}
	case EventTypeMemoryConsolidated:
		return &MemoryConsolidatedPayload{}
	case EventTypeBudgetExhausted:
		return &BudgetExhaustedPayload{}
	case EventTypeBudgetLimitUpdated:
		return &BudgetLimitUpdatedPayload{}
	case EventTypeSubagentSpawned:
		return &SubagentSpawnedPayload{}
	case EventTypeSubagentCompleted:
		return &SubagentCompletedPayload{}
	case EventTypeLLMCalled:
		return &LLMCalledPayload{}
	default:
		return nil
	}
}

// sessionStatusFromEvent 将终态事件映射为 sessions.status 值。
func sessionStatusFromEvent(evType EventType) string {
	switch evType {
	case EventTypeSessionCompleted:
		return "completed"
	case EventTypeSessionFailed:
		return "failed"
	default:
		return ""
	}
}

// nullableString 将空字符串转换为 nil（用于 SQLite NULL）。
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// marshalJSONL 将事件列表序列化为 JSONL 格式。
func marshalJSONL(events []RuntimeEvent) ([]byte, error) {
	var buf []byte
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			return nil, err
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return buf, nil
}

// unmarshalJSONL 从 JSONL 格式反序列化事件列表。
func unmarshalJSONL(data []byte) ([]RuntimeEvent, error) {
	var events []RuntimeEvent
	for len(data) > 0 {
		idx := 0
		for idx < len(data) && data[idx] != '\n' {
			idx++
		}
		line := data[:idx]
		if idx < len(data) {
			data = data[idx+1:]
		} else {
			data = nil
		}
		if len(line) == 0 {
			continue
		}
		var ev RuntimeEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("unmarshal jsonl line: %w", err)
		}
		events = append(events, ev)
	}
	return events, nil
}

// min 兼容 Go 1.20 以下版本的 min 实现。
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
