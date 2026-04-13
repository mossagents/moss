package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/memory"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
)

type contextMemoryService struct {
	store    memory.MemoryStore
	pipeline *memoryPipelineManager
}

func newContextMemoryService(st *state) contextMemoryService {
	if st == nil {
		return contextMemoryService{}
	}
	return contextMemoryService{
		store:    st.store,
		pipeline: st.pipeline,
	}
}

func (s contextMemoryService) CompactSessionContext(
	ctx context.Context,
	sessionStore session.SessionStore,
	sess *session.Session,
	keepRecent int,
	note string,
	llm model.LLM,
	withSummary bool,
) (map[string]any, error) {
	return compactSessionContext(ctx, sessionStore, s.store, s.pipeline, sess, keepRecent, note, llm, withSummary)
}

func compactSessionContext(
	ctx context.Context,
	store session.SessionStore,
	memoryStore memory.MemoryStore,
	memoryPipeline *memoryPipelineManager,
	sess *session.Session,
	keepRecent int,
	note string,
	llm model.LLM,
	withSummary bool,
) (map[string]any, error) {
	if sess == nil {
		return nil, fmt.Errorf("session is required")
	}
	dialogCount := countDialogMessages(sess.Messages)
	if keepRecent <= 0 {
		keepRecent = 20
	}
	if dialogCount <= keepRecent {
		return map[string]any{
			"status":       "noop",
			"session_id":   sess.ID,
			"dialog_count": dialogCount,
			"keep_recent":  keepRecent,
		}, nil
	}
	original := append([]model.Message(nil), sess.Messages...)
	snapshotID := fmt.Sprintf("%s_context_%d", sess.ID, time.Now().UnixNano())
	summaryText := strings.TrimSpace(note)
	if withSummary {
		summaryText = buildSummary(ctx, llm, messagesBeforeDialogTail(original, keepRecent))
		if summaryText == "" {
			summaryText = "Earlier context compacted into a structured summary."
		}
		if strings.TrimSpace(note) != "" {
			summaryText += " Note: " + strings.TrimSpace(note)
		}
	}
	if !withSummary && summaryText == "" {
		summaryText = fmt.Sprintf("Context offloaded to snapshot %s.", snapshotID)
	}
	snapshot := &session.Session{
		ID:       snapshotID,
		Status:   session.StatusCompleted,
		Config:   sess.Config,
		Messages: original,
		State: map[string]any{
			"context_snapshot_of": sess.ID,
			"context_summary":     summaryText,
			"context_keep_recent": keepRecent,
			"context_mode":        map[bool]string{true: "summary", false: "offload"}[withSummary],
			"note":                note,
		},
		Budget:    sess.Budget.Clone(),
		CreatedAt: time.Now(),
		EndedAt:   time.Now(),
	}
	session.MarkHistoryHidden(snapshot)
	if store != nil {
		if err := store.Save(ctx, snapshot); err != nil {
			return nil, fmt.Errorf("save context snapshot: %w", err)
		}
	}
	memoryRecordPath := ""
	if memoryStore != nil {
		record, err := persistContextSummaryMemory(ctx, memoryStore, sess, snapshotID, summaryText, withSummary)
		if err != nil {
			return nil, err
		}
		memoryRecordPath = record.Path
		if err := syncMemoryArtifacts(ctx, memoryPipeline); err != nil {
			return nil, err
		}
	}
	state := session.ReadPromptContextState(sess)
	state.Version = contextStateVersion
	state.BaselineFragments = buildBaselineFragments(sess.Messages)
	state.KeepRecent = keepRecent
	state.CompactedDialogCount = maxInt(0, dialogCount-keepRecent)
	state.LastSnapshotID = snapshotID
	state.LastSummary = summaryText
	state.DynamicFragments = []session.PromptContextFragment{
		newSummaryFragment(snapshotID, summaryText, withSummary),
	}
	changed, hashes := session.ComputePromptFragmentDiff(state.FragmentHashes, session.FlattenPromptContextFragments(state))
	state.FragmentHashes = hashes
	state.LastFragmentDiff = changed
	state.LastPromptBuiltAt = time.Now().UTC()
	session.WritePromptContextState(sess, state)
	if withSummary {
		sess.SetState("last_context_snapshot", snapshotID)
		sess.SetState("last_context_summary", summaryText)
		sess.SetState("last_context_offload_at", time.Now().Format(time.RFC3339))
	} else {
		sess.SetState("last_offload_snapshot", snapshotID)
		sess.SetState("last_offload_at", time.Now().Format(time.RFC3339))
	}
	if store != nil {
		if err := store.Save(ctx, sess); err != nil {
			return nil, fmt.Errorf("save compacted session: %w", err)
		}
	}
	logging.GetLogger().DebugContext(ctx, "prompt context compacted",
		"session_id", sess.ID,
		"snapshot_session", snapshotID,
		"dialog_before", dialogCount,
		"keep_recent", keepRecent,
		"with_summary", withSummary,
	)
	return map[string]any{
		"status":                   "offloaded",
		"session_id":               sess.ID,
		"snapshot_session":         snapshotID,
		"memory_record_path":       memoryRecordPath,
		"dialog_before":            dialogCount,
		"kept_recent":              keepRecent,
		"compacted_dialog_count":   state.CompactedDialogCount,
		"message_count_now":        len(session.BuildPromptMessages(sess.Messages, state)),
		"full_history_messages":    len(sess.Messages),
		"summary":                  summaryText,
		"last_prompt_fragment_ids": append([]string(nil), state.LastFragmentDiff...),
	}, nil
}

func persistContextSummaryMemory(
	ctx context.Context,
	store memory.MemoryStore,
	sess *session.Session,
	snapshotID string,
	summary string,
	withSummary bool,
) (*memory.MemoryRecord, error) {
	if store == nil || sess == nil {
		return nil, fmt.Errorf("context summary memory requires memory store and session")
	}
	mode := "offload"
	if withSummary {
		mode = "summary"
	}
	content := strings.TrimSpace(summary)
	recordPath := normalizeMemoryPath(fmt.Sprintf("context_snapshots/%s/%s.md", strings.TrimSpace(sess.ID), strings.TrimSpace(snapshotID)))
	return store.Upsert(ctx, memory.MemoryRecord{
		Path:       recordPath,
		Content:    content,
		Summary:    content,
		Tags:       []string{"context", "session:" + strings.TrimSpace(sess.ID), mode},
		Stage:      memory.MemoryStageSnapshot,
		Status:     memory.MemoryStatusActive,
		Group:      normalizeMemoryPath("context_snapshots/" + strings.TrimSpace(sess.ID)),
		SourceKind: "context_" + mode,
		SourceID:   strings.TrimSpace(snapshotID),
		SourcePath: strings.TrimSpace(snapshotID),
	})
}
