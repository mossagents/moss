package runtimeenv

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/internal/stringutil"
	"github.com/mossagents/moss/kernel/checkpoint"
)

type CheckpointSummary struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id,omitempty"`
	SnapshotID   string    `json:"snapshot_id,omitempty"`
	Note         string    `json:"note,omitempty"`
	PatchCount   int       `json:"patch_count"`
	LineageDepth int       `json:"lineage_depth"`
	CreatedAt    time.Time `json:"created_at"`
}

type CheckpointDetail struct {
	ID           string                            `json:"id"`
	SessionID    string                            `json:"session_id,omitempty"`
	SnapshotID   string                            `json:"snapshot_id,omitempty"`
	Note         string                            `json:"note,omitempty"`
	PatchIDs     []string                          `json:"patch_ids,omitempty"`
	PatchCount   int                               `json:"patch_count"`
	Lineage      []checkpoint.CheckpointLineageRef `json:"lineage,omitempty"`
	LineageDepth int                               `json:"lineage_depth"`
	Metadata     map[string]any                    `json:"metadata,omitempty"`
	MetadataKeys []string                          `json:"metadata_keys,omitempty"`
	CreatedAt    time.Time                         `json:"created_at"`
}

func SummarizeCheckpoint(item checkpoint.CheckpointRecord) CheckpointSummary {
	sessionID := CheckpointSessionID(item)
	return CheckpointSummary{
		ID:           item.ID,
		SessionID:    sessionID,
		SnapshotID:   item.WorktreeSnapshotID,
		Note:         item.Note,
		PatchCount:   len(item.PatchIDs),
		LineageDepth: len(item.Lineage),
		CreatedAt:    item.CreatedAt,
	}
}

func CheckpointSessionID(item checkpoint.CheckpointRecord) string {
	sessionID := item.SessionID
	for _, ref := range item.Lineage {
		if ref.Kind == checkpoint.CheckpointLineageSession && strings.TrimSpace(ref.ID) != "" {
			sessionID = ref.ID
			break
		}
	}
	return sessionID
}

func DescribeCheckpoint(item checkpoint.CheckpointRecord) CheckpointDetail {
	keys := make([]string, 0, len(item.Metadata))
	for key := range item.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return CheckpointDetail{
		ID:           item.ID,
		SessionID:    CheckpointSessionID(item),
		SnapshotID:   item.WorktreeSnapshotID,
		Note:         item.Note,
		PatchIDs:     append([]string(nil), item.PatchIDs...),
		PatchCount:   len(item.PatchIDs),
		Lineage:      append([]checkpoint.CheckpointLineageRef(nil), item.Lineage...),
		LineageDepth: len(item.Lineage),
		Metadata:     cloneAnyMap(item.Metadata),
		MetadataKeys: keys,
		CreatedAt:    item.CreatedAt,
	}
}

func SummarizeCheckpoints(items []checkpoint.CheckpointRecord) []CheckpointSummary {
	out := make([]CheckpointSummary, 0, len(items))
	for _, item := range items {
		out = append(out, SummarizeCheckpoint(item))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func ListCheckpoints(ctx context.Context, limit int) ([]CheckpointSummary, error) {
	store, err := OpenCheckpointStore()
	if err != nil {
		return nil, err
	}
	items, err := store.List(ctx)
	if err != nil {
		if err == checkpoint.ErrCheckpointUnavailable {
			return nil, nil
		}
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}
	out := SummarizeCheckpoints(items)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func ResolveCheckpointRecord(ctx context.Context, store checkpoint.CheckpointStore, selector string) (*checkpoint.CheckpointRecord, error) {
	if store == nil {
		return nil, checkpoint.ErrCheckpointUnavailable
	}
	selector = strings.TrimSpace(selector)
	if selector == "" || strings.EqualFold(selector, "latest") {
		items, err := store.List(ctx)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, checkpoint.ErrCheckpointNotFound
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].ID < items[j].ID
			}
			return items[i].CreatedAt.After(items[j].CreatedAt)
		})
		item := items[0]
		return &item, nil
	}
	item, err := store.Load(ctx, selector)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, checkpoint.ErrCheckpointNotFound
	}
	return item, nil
}

func LoadCheckpointWithStore(ctx context.Context, store checkpoint.CheckpointStore, selector string) (*CheckpointDetail, error) {
	item, err := ResolveCheckpointRecord(ctx, store, selector)
	if err != nil {
		return nil, err
	}
	detail := DescribeCheckpoint(*item)
	return &detail, nil
}

func LoadCheckpoint(ctx context.Context, checkpointID string) (*CheckpointDetail, error) {
	store, err := OpenCheckpointStore()
	if err != nil {
		return nil, err
	}
	return LoadCheckpointWithStore(ctx, store, checkpointID)
}

func RenderCheckpointSummaries(items []CheckpointSummary) string {
	if len(items) == 0 {
		return "Checkpoints: none"
	}
	var b strings.Builder
	b.WriteString("Checkpoints:\n")
	for _, item := range items {
		fmt.Fprintf(&b, "- %s | created=%s | session=%s | snapshot=%s | patches=%d | lineage=%d | note=%s\n",
			item.ID,
			item.CreatedAt.UTC().Format(time.RFC3339),
			stringutil.FirstNonEmpty(item.SessionID, "(none)"),
			stringutil.FirstNonEmpty(item.SnapshotID, "(none)"),
			item.PatchCount,
			item.LineageDepth,
			stringutil.FirstNonEmpty(item.Note, "(none)"),
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

func RenderCheckpointDetail(item *CheckpointDetail) string {
	if item == nil {
		return "Checkpoint: not found"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Checkpoint: %s\n", item.ID)
	fmt.Fprintf(&b, "  created:  %s\n", item.CreatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "  session:  %s\n", stringutil.FirstNonEmpty(item.SessionID, "(none)"))
	fmt.Fprintf(&b, "  snapshot: %s\n", stringutil.FirstNonEmpty(item.SnapshotID, "(none)"))
	fmt.Fprintf(&b, "  patches:  %d", item.PatchCount)
	if len(item.PatchIDs) > 0 {
		fmt.Fprintf(&b, " (%s)", renderCheckpointPatchOverview(item.PatchIDs, 5))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  lineage:  %d\n", item.LineageDepth)
	fmt.Fprintf(&b, "  note:     %s\n", stringutil.FirstNonEmpty(item.Note, "(none)"))
	if len(item.MetadataKeys) == 0 {
		b.WriteString("  metadata: (none)\n")
	} else {
		fmt.Fprintf(&b, "  metadata: %s\n", strings.Join(item.MetadataKeys, ", "))
	}
	b.WriteString("  lineage refs:\n")
	if len(item.Lineage) == 0 {
		b.WriteString("    - (none)\n")
	} else {
		for _, ref := range item.Lineage {
			fmt.Fprintf(&b, "    - %s %s\n", ref.Kind, stringutil.FirstNonEmpty(strings.TrimSpace(ref.ID), "(none)"))
		}
	}
	b.WriteString("Next:\n")
	fmt.Fprintf(&b, "  - mosscode checkpoint fork --checkpoint %s\n", item.ID)
	fmt.Fprintf(&b, "  - mosscode checkpoint replay --checkpoint %s --mode resume\n", item.ID)
	return strings.TrimRight(b.String(), "\n")
}

func renderCheckpointPatchOverview(ids []string, limit int) string {
	if len(ids) == 0 {
		return ""
	}
	if limit <= 0 || len(ids) <= limit {
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("%s, ... (+%d more)", strings.Join(ids[:limit], ", "), len(ids)-limit)
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
