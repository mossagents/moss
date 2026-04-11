package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel/observe"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileCheckpointStore 是基于文件系统的 CheckpointStore 实现。
// 每个 checkpoint 保存为独立 JSON 文件：{dir}/{checkpoint_id}.json
type FileCheckpointStore struct {
	dir      string
	mu       sync.Mutex
	observer observe.ExecutionObserver
}

func NewFileCheckpointStore(dir string) (*FileCheckpointStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create checkpoint store dir: %w", err)
	}
	return &FileCheckpointStore{
		dir:      dir,
		observer: observe.NoOpObserver{},
	}, nil
}

func (fs *FileCheckpointStore) SetObserver(observer observe.ExecutionObserver) {
	if observer == nil {
		fs.observer = observe.NoOpObserver{}
		return
	}
	fs.observer = observer
}

func (fs *FileCheckpointStore) Create(ctx context.Context, req CheckpointCreateRequest) (*CheckpointRecord, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	record := &CheckpointRecord{
		ID:                 newCheckpointID(sessionID),
		Version:            CurrentCheckpointVersion,
		SessionID:          sessionID,
		WorktreeSnapshotID: strings.TrimSpace(req.WorktreeSnapshotID),
		PatchIDs:           append([]string(nil), req.PatchIDs...),
		Lineage:            append([]CheckpointLineageRef(nil), req.Lineage...),
		Note:               strings.TrimSpace(req.Note),
		Metadata:           cloneMap(req.Metadata),
		CreatedAt:          time.Now().UTC(),
	}
	if len(record.Lineage) == 0 {
		record.Lineage = []CheckpointLineageRef{{
			Kind: CheckpointLineageSession,
			ID:   sessionID,
		}}
	}
	if err := fs.persist(record); err != nil {
		return nil, err
	}
	observe.ObserveExecutionEvent(ctx, fs.observer, observe.ExecutionEvent{
		Type:      observe.ExecutionCheckpointCreated,
		SessionID: record.SessionID,
		Timestamp: record.CreatedAt,
		Metadata: map[string]any{
			"checkpoint_id":        record.ID,
			"checkpoint_note":      record.Note,
			"worktree_snapshot_id": record.WorktreeSnapshotID,
			"patch_count":          len(record.PatchIDs),
			"lineage_depth":        len(record.Lineage),
		},
	})
	return cloneCheckpoint(record), nil
}

func (fs *FileCheckpointStore) Load(_ context.Context, id string) (*CheckpointRecord, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	record, err := fs.loadLocked(id)
	if err != nil {
		return nil, err
	}
	return cloneCheckpoint(record), nil
}

func (fs *FileCheckpointStore) List(_ context.Context) ([]CheckpointRecord, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}
	out := make([]CheckpointRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		record, err := fs.loadLocked(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		out = append(out, *cloneCheckpoint(record))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (fs *FileCheckpointStore) FindBySession(ctx context.Context, sessionID string) ([]CheckpointRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	items, err := fs.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CheckpointRecord, 0, len(items))
	for _, item := range items {
		if item.SessionID == sessionID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (fs *FileCheckpointStore) loadLocked(id string) (*CheckpointRecord, error) {
	path := fs.path(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCheckpointNotFound
		}
		return nil, fmt.Errorf("read checkpoint %s: %w", id, err)
	}
	var record CheckpointRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint %s: %w", id, err)
	}
	return &record, nil
}

func (fs *FileCheckpointStore) persist(record *CheckpointRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	path := fs.path(record.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write checkpoint tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace checkpoint: %w", err)
	}
	return nil
}

func (fs *FileCheckpointStore) path(id string) string {
	return filepath.Join(fs.dir, sanitizeCheckpointID(id)+".json")
}

func sanitizeCheckpointID(id string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, filepath.Base(id))
	if safe == "" || safe == "." || safe == ".." {
		safe = "_invalid_"
	}
	return safe
}

func newCheckpointID(sessionID string) string {
	base := sanitizeCheckpointID(sessionID)
	if base == "_invalid_" || base == "" {
		base = "session"
	}
	return fmt.Sprintf("checkpoint-%s-%d", base, time.Now().UnixNano())
}

func cloneCheckpoint(record *CheckpointRecord) *CheckpointRecord {
	if record == nil {
		return nil
	}
	cp := *record
	cp.PatchIDs = append([]string(nil), record.PatchIDs...)
	cp.Lineage = append([]CheckpointLineageRef(nil), record.Lineage...)
	cp.Metadata = cloneMap(record.Metadata)
	return &cp
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
