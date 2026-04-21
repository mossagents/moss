package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ─────────────────────────────────────────────
// JSONLEventStore（P2）
// ─────────────────────────────────────────────

// JSONLEventStore 是基于 JSONL 文件的 EventStore 实现。
//
// # 使用约束（§阶段5）
//
// JSONLEventStore 仅允许用于以下场景，禁止在生产运行时中直接注册为 kernel 的事件存储后端：
//
//  1. 单元测试和集成测试（test harness）；
//  2. 单 session 的离线调试与 export / import（通过 EventStore.Export / Import 接口）；
//  3. 轻量场景（不需要并发多 session 写入的工具脚本）。
//
// 生产路径（apps/mosscode、contrib/tui）必须使用 SQLiteEventStore。
// 不得在同一 session 内混用两种实现。
//
// 文件布局：
//   - <storeDir>/<sessionID>.jsonl  — 事件流（每行一个 RuntimeEvent JSON）
//   - <storeDir>/<sessionID>.view.json — 可选的物化视图缓存
type JSONLEventStore struct {
	storeDir   string
	projection *ProjectionEngine

	mu       sync.Mutex
	seqCache map[string]int64  // session_id → last_seq（内存缓存）
	status   map[string]string // session_id → status
}

// NewJSONLEventStore 创建并初始化 JSONLEventStore，确保 storeDir 目录存在。
func NewJSONLEventStore(storeDir string) (*JSONLEventStore, error) {
	if err := os.MkdirAll(storeDir, 0o750); err != nil {
		return nil, fmt.Errorf("mkdir storeDir: %w", err)
	}
	return &JSONLEventStore{
		storeDir:   storeDir,
		projection: NewProjectionEngine(),
		seqCache:   make(map[string]int64),
		status:     make(map[string]string),
	}, nil
}

// ─────────────────────────────────────────────
// AppendEvents
// ─────────────────────────────────────────────

func (s *JSONLEventStore) AppendEvents(
	ctx context.Context,
	sessionID string,
	expectedSeq int64,
	requestID string,
	events []RuntimeEvent,
) error {
	if len(events) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查 session 状态
	if st := s.status[sessionID]; st == "completed" || st == "failed" {
		return ErrSessionEnded
	}

	// 读取当前 last_seq（优先内存缓存）
	currentSeq, ok := s.seqCache[sessionID]
	if !ok {
		// 首次：扫描文件确定 last_seq
		var err error
		currentSeq, err = s.readLastSeq(sessionID)
		if err != nil {
			return fmt.Errorf("read last seq: %w", err)
		}
		s.seqCache[sessionID] = currentSeq
	}

	// 幂等检查（JSONL 实现：线性扫描文件）— 在 OCC 之前，与 SQLite 实现保持一致
	if requestID != "" {
		exists, err := s.requestIDExists(sessionID, requestID)
		if err != nil {
			return fmt.Errorf("idempotency check: %w", err)
		}
		if exists {
			return nil
		}
	}

	// Optimistic concurrency
	if currentSeq != expectedSeq {
		return fmt.Errorf("%w: expected=%d current=%d", ErrSeqConflict, expectedSeq, currentSeq)
	}

	// 打开文件（追加模式）
	path := s.eventFilePath(sessionID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("open event file: %w", err)
	}
	defer f.Close()

	nextSeq := currentSeq + 1
	var lastStatus string
	for i, ev := range events {
		ev.SessionID = sessionID
		ev.Seq = nextSeq
		if ev.RequestID == "" {
			ev.RequestID = requestID
		}
		line, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal event[%d]: %w", i, err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write event[%d]: %w", i, err)
		}
		lastStatus = sessionStatusFromEvent(ev.Type)
		nextSeq++
	}

	// 更新内存缓存
	s.seqCache[sessionID] = nextSeq - 1
	if lastStatus != "" {
		s.status[sessionID] = lastStatus
	}

	return nil
}

// ─────────────────────────────────────────────
// LoadEvents
// ─────────────────────────────────────────────

func (s *JSONLEventStore) LoadEvents(
	ctx context.Context,
	sessionID string,
	afterSeq int64,
) ([]RuntimeEvent, error) {
	path := s.eventFilePath(sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read event file: %w", err)
	}

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
		if ev.Seq > afterSeq {
			events = append(events, ev)
		}
	}
	return events, nil
}

// ─────────────────────────────────────────────
// LoadSessionView
// ─────────────────────────────────────────────

func (s *JSONLEventStore) LoadSessionView(
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
	return s.projection.Replay(sessionID, events)
}

// ─────────────────────────────────────────────
// RebuildProjections
// ─────────────────────────────────────────────

func (s *JSONLEventStore) RebuildProjections(ctx context.Context, sessionID string) error {
	_, err := s.LoadSessionView(ctx, sessionID)
	return err
}

// ─────────────────────────────────────────────
// ListResumeCandidates
// ─────────────────────────────────────────────

func (s *JSONLEventStore) ListResumeCandidates(
	ctx context.Context,
	filter ResumeCandidateFilter,
) ([]ResumeCandidate, error) {
	entries, err := os.ReadDir(s.storeDir)
	if err != nil {
		return nil, fmt.Errorf("readdir: %w", err)
	}

	var candidates []ResumeCandidate
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".jsonl" {
			continue
		}
		sessionID := name[:len(name)-len(".jsonl")]

		s.mu.Lock()
		st := s.status[sessionID]
		seq := s.seqCache[sessionID]
		s.mu.Unlock()

		if st == "completed" || st == "failed" {
			continue
		}
		if filter.WorkspaceID != "" {
			// JSONL 实现不索引 workspace，跳过过滤（生产应使用 SQLite）
		}
		candidates = append(candidates, ResumeCandidate{
			SessionID: sessionID,
			LastSeq:   seq,
			Status:    st,
		})
		if filter.Limit > 0 && len(candidates) >= filter.Limit {
			break
		}
	}
	return candidates, nil
}

// ─────────────────────────────────────────────
// Export / Import
// ─────────────────────────────────────────────

func (s *JSONLEventStore) Export(
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

func (s *JSONLEventStore) Import(
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
	return s.AppendEvents(ctx, sessionID, 0, "", events)
}

// ─────────────────────────────────────────────
// SubscribeEvents（§15.2 集群化预留，不支持）
// ─────────────────────────────────────────────

func (s *JSONLEventStore) SubscribeEvents(
	_ context.Context,
	_ string,
	_ int64,
) (<-chan RuntimeEvent, error) {
	return nil, ErrNotSupported
}

// ─────────────────────────────────────────────
// 内部辅助
// ─────────────────────────────────────────────

func (s *JSONLEventStore) eventFilePath(sessionID string) string {
	return filepath.Join(s.storeDir, sessionID+".jsonl")
}

// readLastSeq 扫描事件文件，返回最大 seq。
func (s *JSONLEventStore) readLastSeq(sessionID string) (int64, error) {
	events, err := s.LoadEvents(context.Background(), sessionID, 0)
	if err != nil {
		return 0, err
	}
	var lastSeq int64
	for _, ev := range events {
		if ev.Seq > lastSeq {
			lastSeq = ev.Seq
		}
	}
	return lastSeq, nil
}

// requestIDExists 线性扫描检查 request_id 是否已存在。
func (s *JSONLEventStore) requestIDExists(sessionID, requestID string) (bool, error) {
	events, err := s.LoadEvents(context.Background(), sessionID, 0)
	if err != nil {
		return false, err
	}
	for _, ev := range events {
		if ev.RequestID == requestID {
			return true, nil
		}
	}
	return false, nil
}
