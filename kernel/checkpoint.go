package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/workspace"
	"strings"
	"time"
)

const (
	checkpointSnapshotHiddenKey = "checkpoint_snapshot_hidden"
	checkpointSnapshotSourceKey = "checkpoint_snapshot_source_session_id"
	checkpointSourceKindKey     = "checkpoint_source_kind"
	checkpointSourceIDKey       = "checkpoint_source_id"
	checkpointReplayModeKey     = "checkpoint_replay_mode"
	checkpointNoteKey           = "checkpoint_note"
	checkpointDegradedKey       = "checkpoint_degraded"
	checkpointDetailsKey        = "checkpoint_details"
	checkpointRestoredKey       = "checkpoint_restored_worktree"
)

// ForkResult 描述一次 session fork 的结构化结果。
type ForkResult struct {
	SourceKind       checkpoint.ForkSourceKind `json:"source_kind"`
	SourceID         string              `json:"source_id,omitempty"`
	CheckpointID     string              `json:"checkpoint_id,omitempty"`
	SessionID        string              `json:"session_id,omitempty"`
	RestoredWorktree bool                `json:"restored_worktree,omitempty"`
	Degraded         bool                `json:"degraded,omitempty"`
	Details          string              `json:"details,omitempty"`
}

// CreateCheckpoint captures the current session into a recoverable checkpoint.
func (k *Kernel) CreateCheckpoint(ctx context.Context, sess *session.Session, req checkpoint.CheckpointCreateRequest) (*checkpoint.CheckpointRecord, error) {
	if k.checkpoints == nil {
		return nil, checkpoint.ErrCheckpointUnavailable
	}
	if k.store == nil {
		return nil, checkpoint.ErrCheckpointNotRecoverable
	}
	if sess == nil {
		return nil, fmt.Errorf("session is required")
	}

	frozen := cloneSession(sess)
	frozen.ID = checkpointSnapshotSessionID(sess.ID)
	frozen.Status = session.StatusPaused
	frozen.EndedAt = time.Now().UTC()
	frozen.SetMetadata(checkpointSnapshotHiddenKey, true)
	frozen.SetMetadata(checkpointSnapshotSourceKey, sess.ID)
	if err := k.store.Save(ctx, frozen); err != nil {
		return nil, fmt.Errorf("save checkpoint session snapshot: %w", err)
	}

	createReq := req
	createReq.SessionID = frozen.ID
	createReq.Lineage = mergeCheckpointLineage(sess, req.Lineage)

	if k.snapshots != nil && strings.TrimSpace(createReq.WorktreeSnapshotID) == "" {
		snapshot, err := k.snapshots.Create(ctx, workspace.WorktreeSnapshotRequest{
			SessionID: sess.ID,
			Note:      strings.TrimSpace(req.Note),
		})
		if err == nil && snapshot != nil {
			createReq.WorktreeSnapshotID = snapshot.ID
			if len(createReq.PatchIDs) == 0 {
				createReq.PatchIDs = snapshotPatchIDs(snapshot)
			}
		} else if err != nil && !errors.Is(err, workspace.ErrWorktreeSnapshotUnavailable) {
			return nil, err
		}
	}
	record, err := k.checkpoints.Create(ctx, createReq)
	if err != nil {
		return nil, err
	}
	return record, nil
}

// ForkSession creates a new live session from a checkpoint or a source session.
// When SourceKind=session, the kernel prefers the latest checkpoint for that session.
func (k *Kernel) ForkSession(ctx context.Context, req checkpoint.ForkRequest) (*session.Session, *ForkResult, error) {
	sourceSession, checkpointRecord, result, err := k.resolveForkSource(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	cloned, err := k.instantiateClonedSession(ctx, sourceSession, forkSessionConfigMetadata(sourceSession.Config.Metadata, map[string]any{
		checkpointSourceKindKey: string(result.SourceKind),
		checkpointSourceIDKey:   result.SourceID,
		checkpointNoteKey:       strings.TrimSpace(req.Note),
		checkpointDegradedKey:   result.Degraded,
		checkpointDetailsKey:    result.Details,
		checkpointRestoredKey:   result.RestoredWorktree,
	}))
	if err != nil {
		return nil, nil, err
	}
	if checkpointRecord != nil {
		result.CheckpointID = checkpointRecord.ID
	}
	result.SessionID = cloned.ID
	k.emitExecutionEvent(ctx, observe.ExecutionSessionForked, cloned.ID, map[string]any{
		"source_kind":       result.SourceKind,
		"source_id":         result.SourceID,
		"checkpoint_id":     result.CheckpointID,
		"restored_worktree": result.RestoredWorktree,
		"degraded":          result.Degraded,
		"details":           result.Details,
	})
	return cloned, result, nil
}

// ReplayFromCheckpoint prepares a fresh session from a checkpoint.
func (k *Kernel) ReplayFromCheckpoint(ctx context.Context, req checkpoint.ReplayRequest) (*session.Session, *checkpoint.ReplayResult, error) {
	if k.checkpoints == nil {
		return nil, nil, checkpoint.ErrCheckpointUnavailable
	}
	record, err := k.checkpoints.Load(ctx, strings.TrimSpace(req.CheckpointID))
	if err != nil {
		return nil, nil, err
	}
	source, err := k.loadCheckpointSession(ctx, record)
	if err != nil {
		return nil, nil, err
	}
	result := &checkpoint.ReplayResult{
		CheckpointID: record.ID,
		Mode:         req.Mode,
	}
	if result.Mode == "" {
		result.Mode = checkpoint.ReplayModeResume
	}
	if req.RestoreWorktree {
		result.RestoredWorktree, result.Degraded, result.Details = k.restoreCheckpointWorktree(ctx, record)
	}

	replaySource := source
	if result.Mode == checkpoint.ReplayModeRerun {
		replaySource = rerunSession(source)
	}
	cloned, err := k.instantiateClonedSession(ctx, replaySource, forkSessionConfigMetadata(replaySource.Config.Metadata, map[string]any{
		checkpointSourceKindKey: string(checkpoint.CheckpointLineageCheckpoint),
		checkpointSourceIDKey:   record.ID,
		checkpointReplayModeKey: string(result.Mode),
		checkpointNoteKey:       strings.TrimSpace(req.Note),
		checkpointDegradedKey:   result.Degraded,
		checkpointDetailsKey:    result.Details,
		checkpointRestoredKey:   result.RestoredWorktree,
	}))
	if err != nil {
		return nil, nil, err
	}
	result.SessionID = cloned.ID
	k.emitExecutionEvent(ctx, observe.ExecutionReplayPrepared, cloned.ID, map[string]any{
		"checkpoint_id":       record.ID,
		"mode":                result.Mode,
		"restored_worktree":   result.RestoredWorktree,
		"degraded":            result.Degraded,
		"details":             result.Details,
		"source_session_id":   record.SessionID,
		"replayed_session_id": cloned.ID,
	})
	return cloned, result, nil
}

func (k *Kernel) resolveForkSource(ctx context.Context, req checkpoint.ForkRequest) (*session.Session, *checkpoint.CheckpointRecord, *ForkResult, error) {
	sourceKind := req.SourceKind
	if sourceKind == "" {
		sourceKind = checkpoint.ForkSourceSession
	}
	sourceID := strings.TrimSpace(req.SourceID)
	if sourceID == "" {
		return nil, nil, nil, fmt.Errorf("source_id is required")
	}
	result := &ForkResult{
		SourceKind: sourceKind,
		SourceID:   sourceID,
	}

	switch sourceKind {
	case checkpoint.ForkSourceCheckpoint:
		if k.checkpoints == nil {
			return nil, nil, nil, checkpoint.ErrCheckpointUnavailable
		}
		record, err := k.checkpoints.Load(ctx, sourceID)
		if err != nil {
			return nil, nil, nil, err
		}
		if req.RestoreWorktree {
			result.RestoredWorktree, result.Degraded, result.Details = k.restoreCheckpointWorktree(ctx, record)
		}
		sourceSession, err := k.loadCheckpointSession(ctx, record)
		if err != nil {
			return nil, nil, nil, err
		}
		return sourceSession, record, result, nil

	case checkpoint.ForkSourceSession:
		if k.checkpoints != nil {
			records, err := k.checkpoints.FindBySession(ctx, sourceID)
			if err == nil && len(records) > 0 {
				record := records[0]
				result.SourceKind = checkpoint.ForkSourceCheckpoint
				result.SourceID = record.ID
				result.CheckpointID = record.ID
				if req.RestoreWorktree {
					result.RestoredWorktree, result.Degraded, result.Details = k.restoreCheckpointWorktree(ctx, &record)
				}
				sourceSession, err := k.loadCheckpointSession(ctx, &record)
				if err != nil {
					return nil, nil, nil, err
				}
				return sourceSession, &record, result, nil
			}
		}
		sourceSession, err := k.loadLiveSession(ctx, sourceID)
		if err != nil {
			return nil, nil, nil, err
		}
		return sourceSession, nil, result, nil
	default:
		return nil, nil, nil, fmt.Errorf("unsupported fork source kind %q", sourceKind)
	}
}

func (k *Kernel) loadLiveSession(ctx context.Context, id string) (*session.Session, error) {
	if live, ok := k.sessions.Get(id); ok && live != nil {
		return cloneSession(live), nil
	}
	if k.store == nil {
		return nil, fmt.Errorf("session %q not found", id)
	}
	loaded, err := k.store.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	if loaded == nil {
		return nil, fmt.Errorf("session %q not found", id)
	}
	return loaded, nil
}

func (k *Kernel) loadCheckpointSession(ctx context.Context, record *checkpoint.CheckpointRecord) (*session.Session, error) {
	if record == nil {
		return nil, checkpoint.ErrCheckpointNotFound
	}
	if k.store == nil {
		return nil, checkpoint.ErrCheckpointNotRecoverable
	}
	loaded, err := k.store.Load(ctx, record.SessionID)
	if err != nil {
		return nil, err
	}
	if loaded == nil {
		return nil, checkpoint.ErrCheckpointNotRecoverable
	}
	return loaded, nil
}

func (k *Kernel) restoreCheckpointWorktree(ctx context.Context, record *checkpoint.CheckpointRecord) (bool, bool, string) {
	if record == nil || strings.TrimSpace(record.WorktreeSnapshotID) == "" {
		return false, true, "checkpoint has no worktree snapshot"
	}
	if k.snapshots == nil || k.reverts == nil {
		return false, true, "worktree restore is unavailable in the current kernel"
	}
	snapshot, err := k.snapshots.Load(ctx, record.WorktreeSnapshotID)
	if err != nil {
		if errors.Is(err, workspace.ErrWorktreeSnapshotNotFound) || errors.Is(err, workspace.ErrWorktreeSnapshotUnavailable) {
			return false, true, err.Error()
		}
		return false, true, err.Error()
	}
	if _, err := k.reverts.Revert(ctx, workspace.PatchRevertRequest{
		Capture:          &snapshot.Capture,
		RestoreTracked:   true,
		RestoreUntracked: true,
	}); err != nil {
		if errors.Is(err, workspace.ErrPatchRevertUnavailable) {
			return false, true, err.Error()
		}
		return false, true, err.Error()
	}
	if isExactSnapshotRestore(snapshot) {
		return true, false, ""
	}
	return false, true, "restored repository capture, but exact checkpoint patch state could not be reconstructed"
}

func isExactSnapshotRestore(snapshot *workspace.WorktreeSnapshot) bool {
	if snapshot == nil {
		return false
	}
	return !snapshot.Capture.IsDirty &&
		len(snapshot.Capture.Staged) == 0 &&
		len(snapshot.Capture.Unstaged) == 0 &&
		len(snapshot.Capture.Untracked) == 0 &&
		len(snapshot.Patches) == 0
}

func (k *Kernel) instantiateClonedSession(ctx context.Context, source *session.Session, metadata map[string]any) (*session.Session, error) {
	if source == nil {
		return nil, fmt.Errorf("source session is required")
	}
	cfg := cloneSessionConfig(source.Config)
	cfg.Metadata = metadata
	live, err := k.sessions.Create(ctx, cfg)
	if err != nil {
		return nil, err
	}
	live.Status = session.StatusCreated
	live.Messages = cloneMessages(source.Messages)
	live.State = cloneState(source.State)
	live.Budget = source.Budget.Clone()
	live.EndedAt = time.Time{}
	if k.store != nil {
		if err := k.store.Save(ctx, live); err != nil {
			return nil, fmt.Errorf("save cloned session: %w", err)
		}
	}
	return live, nil
}

func rerunSession(source *session.Session) *session.Session {
	if source == nil {
		return nil
	}
	cloned := cloneSession(source)
	filtered := make([]model.Message, 0, len(cloned.Messages))
	for _, msg := range cloned.Messages {
		if msg.Role == model.RoleSystem || msg.Role == model.RoleUser {
			filtered = append(filtered, cloneMessage(msg))
		}
	}
	cloned.Messages = filtered
	cloned.State = make(map[string]any)
	cloned.Budget.ResetUsage()
	cloned.Status = session.StatusCreated
	cloned.EndedAt = time.Time{}
	return cloned
}

func mergeCheckpointLineage(sess *session.Session, extra []checkpoint.CheckpointLineageRef) []checkpoint.CheckpointLineageRef {
	merged := make([]checkpoint.CheckpointLineageRef, 0, len(extra)+2)
	seen := make(map[string]bool)
	add := func(kind checkpoint.CheckpointLineageKind, id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		key := string(kind) + ":" + id
		if seen[key] {
			return
		}
		seen[key] = true
		merged = append(merged, checkpoint.CheckpointLineageRef{Kind: kind, ID: id})
	}
	if sess != nil {
		if v, ok := sess.GetMetadata(checkpointSourceKindKey); ok {
			if kind, ok := v.(string); ok {
				switch checkpoint.CheckpointLineageKind(kind) {
				case checkpoint.CheckpointLineageCheckpoint, checkpoint.CheckpointLineageSession, checkpoint.CheckpointLineageReplay:
					if v2, ok2 := sess.GetMetadata(checkpointSourceIDKey); ok2 {
						if id, ok3 := v2.(string); ok3 {
							add(checkpoint.CheckpointLineageKind(kind), id)
						}
					}
				}
			}
		}
	}
	for _, item := range extra {
		add(item.Kind, item.ID)
	}
	if sess != nil {
		add(checkpoint.CheckpointLineageSession, sess.ID)
	}
	return merged
}

func checkpointSnapshotSessionID(sourceID string) string {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		sourceID = "session"
	}
	return fmt.Sprintf("checkpoint-session-%s-%d", sourceID, time.Now().UnixNano())
}

func snapshotPatchIDs(snapshot *workspace.WorktreeSnapshot) []string {
	if snapshot == nil || len(snapshot.Patches) == 0 {
		return nil
	}
	out := make([]string, 0, len(snapshot.Patches))
	for _, item := range snapshot.Patches {
		if id := strings.TrimSpace(item.PatchID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func forkSessionConfigMetadata(base map[string]any, extra map[string]any) map[string]any {
	out := cloneState(base)
	if out == nil {
		out = make(map[string]any)
	}
	for key, value := range extra {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) == "" {
				continue
			}
		}
		out[key] = value
	}
	delete(out, checkpointSnapshotHiddenKey)
	return out
}

func cloneSession(source *session.Session) *session.Session {
	if source == nil {
		return nil
	}
	return &session.Session{
		ID:        source.ID,
		Status:    source.Status,
		Config:    cloneSessionConfig(source.Config),
		Messages:  cloneMessages(source.Messages),
		State:     cloneState(source.State),
		Budget:    source.Budget.Clone(),
		CreatedAt: source.CreatedAt,
		EndedAt:   source.EndedAt,
	}
}

func cloneSessionConfig(cfg session.SessionConfig) session.SessionConfig {
	cfg.Metadata = cloneState(cfg.Metadata)
	return cfg
}

func cloneMessages(items []model.Message) []model.Message {
	if len(items) == 0 {
		return nil
	}
	out := make([]model.Message, len(items))
	for i, item := range items {
		out[i] = cloneMessage(item)
	}
	return out
}

func cloneMessage(msg model.Message) model.Message {
	cp := msg
	if len(msg.ToolCalls) > 0 {
		cp.ToolCalls = make([]model.ToolCall, len(msg.ToolCalls))
		for i, call := range msg.ToolCalls {
			cp.ToolCalls[i] = model.ToolCall{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: cloneJSON(call.Arguments),
			}
		}
	}
	if len(msg.ToolResults) > 0 {
		cp.ToolResults = append([]model.ToolResult(nil), msg.ToolResults...)
	}
	return cp
}

func cloneJSON(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(in))
	copy(out, in)
	return out
}

func cloneState(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (k *Kernel) emitExecutionEvent(ctx context.Context, typ observe.ExecutionEventType, sessionID string, data map[string]any) {
	observer := k.observer
	if observer == nil {
		observer = observe.NoOpObserver{}
	}
	observe.ObserveExecutionEvent(ctx, observer, observe.ExecutionEvent{
		Type:      typ,
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
		Metadata:      data,
	})
}
