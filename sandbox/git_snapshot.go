package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/workspace"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// GitWorktreeSnapshotStore 使用 git metadata 目录持久化 ghost-state 快照。
type GitWorktreeSnapshotStore struct {
	root     string
	timeout  time.Duration
	observer observe.ExecutionObserver
}

func NewGitWorktreeSnapshotStore(root string) *GitWorktreeSnapshotStore {
	return &GitWorktreeSnapshotStore{
		root:     root,
		timeout:  10 * time.Second,
		observer: observe.NoOpObserver{},
	}
}

func (s *GitWorktreeSnapshotStore) SetObserver(observer observe.ExecutionObserver) {
	if observer == nil {
		s.observer = observe.NoOpObserver{}
		return
	}
	s.observer = observer
}

func (s *GitWorktreeSnapshotStore) Create(ctx context.Context, req workspace.WorktreeSnapshotRequest) (*workspace.WorktreeSnapshot, error) {
	repoRoot, gitDir, journal, err := resolveGitRepo(ctx, s.root, s.timeout)
	if err != nil {
		if isGitRepoError(err) {
			return nil, workspace.ErrWorktreeSnapshotUnavailable
		}
		return nil, err
	}
	capture := req.Capture
	if capture == nil {
		capture, err = NewGitRepoStateCapture(repoRoot).Capture(ctx)
		if err != nil {
			return nil, err
		}
	}
	patches, err := journal.List()
	if err != nil {
		return nil, err
	}
	snapshot := &workspace.WorktreeSnapshot{
		ID:        newSnapshotID(capture.HeadSHA),
		SessionID: strings.TrimSpace(req.SessionID),
		Mode:      workspace.WorktreeSnapshotGhostState,
		RepoRoot:  repoRoot,
		Note:      strings.TrimSpace(req.Note),
		Capture:   *capture,
		Patches:   patches,
		CreatedAt: time.Now().UTC(),
	}
	if err := persistSnapshot(filepath.Join(gitDir, "moss-snapshots"), snapshot); err != nil {
		return nil, err
	}
	observe.ObserveExecutionEvent(ctx, s.observer, observe.ExecutionEvent{
		Type:      observe.ExecutionSnapshotCreated,
		SessionID: snapshot.SessionID,
		Timestamp: snapshot.CreatedAt,
		Data: map[string]any{
			"snapshot_id": snapshot.ID,
			"mode":        snapshot.Mode,
			"repo_root":   snapshot.RepoRoot,
			"patch_count": len(snapshot.Patches),
			"note":        snapshot.Note,
		},
	})
	return snapshot, nil
}

func (s *GitWorktreeSnapshotStore) Load(ctx context.Context, id string) (*workspace.WorktreeSnapshot, error) {
	_, gitDir, _, err := resolveGitRepo(ctx, s.root, s.timeout)
	if err != nil {
		if isGitRepoError(err) {
			return nil, workspace.ErrWorktreeSnapshotUnavailable
		}
		return nil, err
	}
	return loadSnapshot(filepath.Join(gitDir, "moss-snapshots"), id)
}

func (s *GitWorktreeSnapshotStore) List(ctx context.Context) ([]workspace.WorktreeSnapshot, error) {
	_, gitDir, _, err := resolveGitRepo(ctx, s.root, s.timeout)
	if err != nil {
		if isGitRepoError(err) {
			return nil, workspace.ErrWorktreeSnapshotUnavailable
		}
		return nil, err
	}
	dir := filepath.Join(gitDir, "moss-snapshots")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	out := make([]workspace.WorktreeSnapshot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		snapshot, err := loadSnapshot(dir, strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		out = append(out, *snapshot)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *GitWorktreeSnapshotStore) FindBySession(ctx context.Context, sessionID string) ([]workspace.WorktreeSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	items, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]workspace.WorktreeSnapshot, 0, len(items))
	for _, item := range items {
		if item.SessionID == sessionID {
			out = append(out, item)
		}
	}
	return out, nil
}

func newSnapshotID(head string) string {
	short := strings.TrimSpace(head)
	if len(short) > 8 {
		short = short[:8]
	}
	if short == "" {
		short = "nohead"
	}
	return fmt.Sprintf("snapshot-%s-%d", short, time.Now().UnixNano())
}

func persistSnapshot(dir string, snapshot *workspace.WorktreeSnapshot) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	path := filepath.Join(dir, snapshot.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write snapshot tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace snapshot: %w", err)
	}
	return nil
}

func loadSnapshot(dir, id string) (*workspace.WorktreeSnapshot, error) {
	path := filepath.Join(dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, workspace.ErrWorktreeSnapshotNotFound
		}
		return nil, fmt.Errorf("read snapshot %s: %w", id, err)
	}
	var snapshot workspace.WorktreeSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot %s: %w", id, err)
	}
	return &snapshot, nil
}
