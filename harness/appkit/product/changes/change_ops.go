package changes

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/x/stringutil"
	"github.com/mossagents/moss/kernel/workspace"
)

type ChangeStatus string

const (
	ChangeStatusPreparing            ChangeStatus = "preparing"
	ChangeStatusApplied              ChangeStatus = "applied"
	ChangeStatusRolledBack           ChangeStatus = "rolled_back"
	ChangeStatusApplyInconsistent    ChangeStatus = "apply_inconsistent"
	ChangeStatusRollbackInconsistent ChangeStatus = "rollback_inconsistent"
)

type RollbackMode string

const (
	RollbackModeExact RollbackMode = "exact"
)

type ChangeOperation struct {
	ID                 string               `json:"id"`
	RepoRoot           string               `json:"repo_root"`
	BaseHeadSHA        string               `json:"base_head_sha,omitempty"`
	SessionID          string               `json:"session_id,omitempty"`
	RunID              string               `json:"run_id,omitempty"`
	TurnID             string               `json:"turn_id,omitempty"`
	InstructionProfile string               `json:"instruction_profile,omitempty"`
	ModelLane          string               `json:"model_lane,omitempty"`
	VisibleTools       []string             `json:"visible_tools,omitempty"`
	HiddenTools        []string             `json:"hidden_tools,omitempty"`
	PatchID            string               `json:"patch_id,omitempty"`
	CheckpointID       string               `json:"checkpoint_id,omitempty"`
	Summary            string               `json:"summary,omitempty"`
	TargetFiles        []string             `json:"target_files,omitempty"`
	Status             ChangeStatus         `json:"status"`
	RecoveryMode       string               `json:"recovery_mode,omitempty"`
	RecoveryDetails    string               `json:"recovery_details,omitempty"`
	RollbackMode       RollbackMode         `json:"rollback_mode,omitempty"`
	RollbackDetails    string               `json:"rollback_details,omitempty"`
	Capture            *workspace.RepoState `json:"capture,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	RolledBackAt       time.Time            `json:"rolled_back_at,omitempty"`
}

type ChangeSummary struct {
	ID                 string       `json:"id"`
	RepoRoot           string       `json:"repo_root"`
	BaseHeadSHA        string       `json:"base_head_sha,omitempty"`
	SessionID          string       `json:"session_id,omitempty"`
	RunID              string       `json:"run_id,omitempty"`
	TurnID             string       `json:"turn_id,omitempty"`
	InstructionProfile string       `json:"instruction_profile,omitempty"`
	ModelLane          string       `json:"model_lane,omitempty"`
	VisibleTools       []string     `json:"visible_tools,omitempty"`
	PatchID            string       `json:"patch_id,omitempty"`
	CheckpointID       string       `json:"checkpoint_id,omitempty"`
	Summary            string       `json:"summary,omitempty"`
	TargetFiles        []string     `json:"target_files,omitempty"`
	Status             ChangeStatus `json:"status"`
	RecoveryMode       string       `json:"recovery_mode,omitempty"`
	RecoveryDetails    string       `json:"recovery_details,omitempty"`
	RollbackMode       RollbackMode `json:"rollback_mode,omitempty"`
	RollbackDetails    string       `json:"rollback_details,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	RolledBackAt       time.Time    `json:"rolled_back_at,omitempty"`
}

type ApplyChangeRequest struct {
	Patch     string                `json:"patch"`
	Summary   string                `json:"summary,omitempty"`
	SessionID string                `json:"session_id,omitempty"`
	Source    workspace.PatchSource `json:"source,omitempty"`
}

type RollbackChangeRequest struct {
	ChangeID string `json:"change_id"`
}

type ChangeOperationError struct {
	Operation *ChangeOperation
	Message   string
}

func (e *ChangeOperationError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Message)
}

func SummarizeChange(item ChangeOperation) ChangeSummary {
	return ChangeSummary{
		ID:                 item.ID,
		RepoRoot:           item.RepoRoot,
		BaseHeadSHA:        item.BaseHeadSHA,
		SessionID:          item.SessionID,
		RunID:              item.RunID,
		TurnID:             item.TurnID,
		InstructionProfile: item.InstructionProfile,
		ModelLane:          item.ModelLane,
		VisibleTools:       append([]string(nil), item.VisibleTools...),
		PatchID:            item.PatchID,
		CheckpointID:       item.CheckpointID,
		Summary:            item.Summary,
		TargetFiles:        append([]string(nil), item.TargetFiles...),
		Status:             item.Status,
		RecoveryMode:       item.RecoveryMode,
		RecoveryDetails:    item.RecoveryDetails,
		RollbackMode:       item.RollbackMode,
		RollbackDetails:    item.RollbackDetails,
		CreatedAt:          item.CreatedAt,
		RolledBackAt:       item.RolledBackAt,
	}
}

func SummarizeChanges(items []ChangeOperation) []ChangeSummary {
	out := make([]ChangeSummary, 0, len(items))
	for _, item := range items {
		out = append(out, SummarizeChange(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func RenderChangeSummaries(items []ChangeSummary) string {
	if len(items) == 0 {
		return "Changes: none"
	}
	var b strings.Builder
	b.WriteString("Changes:\n")
	for _, item := range items {
		fmt.Fprintf(&b, "- %s | status=%s | patch=%s | files=%d | created=%s | summary=%s\n",
			item.ID,
			item.Status,
			stringutil.FirstNonEmpty(item.PatchID, "(pending)"),
			len(item.TargetFiles),
			item.CreatedAt.UTC().Format(time.RFC3339),
			stringutil.FirstNonEmpty(item.Summary, "(none)"),
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

func RenderChangeDetail(item *ChangeOperation) string {
	if item == nil {
		return "Change: not found"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Change: %s\n", item.ID)
	fmt.Fprintf(&b, "  repo:      %s\n", stringutil.FirstNonEmpty(item.RepoRoot, "(none)"))
	fmt.Fprintf(&b, "  head:      %s\n", stringutil.FirstNonEmpty(item.BaseHeadSHA, "(none)"))
	fmt.Fprintf(&b, "  session:   %s\n", stringutil.FirstNonEmpty(item.SessionID, "(none)"))
	fmt.Fprintf(&b, "  run:       %s\n", stringutil.FirstNonEmpty(item.RunID, "(none)"))
	fmt.Fprintf(&b, "  turn:      %s\n", stringutil.FirstNonEmpty(item.TurnID, "(none)"))
	fmt.Fprintf(&b, "  profile:   %s\n", stringutil.FirstNonEmpty(item.InstructionProfile, "(none)"))
	fmt.Fprintf(&b, "  model lane:%s\n", PadField(stringutil.FirstNonEmpty(item.ModelLane, "(none)")))
	fmt.Fprintf(&b, "  patch:     %s\n", stringutil.FirstNonEmpty(item.PatchID, "(none)"))
	fmt.Fprintf(&b, "  checkpoint:%s\n", PadField(stringutil.FirstNonEmpty(item.CheckpointID, "(none)")))
	fmt.Fprintf(&b, "  status:    %s\n", item.Status)
	fmt.Fprintf(&b, "  recovery:  %s\n", stringutil.FirstNonEmpty(item.RecoveryMode, "(none)"))
	if strings.TrimSpace(item.RecoveryDetails) != "" {
		fmt.Fprintf(&b, "  recovery details: %s\n", item.RecoveryDetails)
	}
	if item.RollbackMode != "" {
		fmt.Fprintf(&b, "  rollback:  %s\n", item.RollbackMode)
	}
	if strings.TrimSpace(item.RollbackDetails) != "" {
		fmt.Fprintf(&b, "  rollback details: %s\n", item.RollbackDetails)
	}
	if len(item.TargetFiles) == 0 {
		b.WriteString("  files:     (none)\n")
	} else {
		b.WriteString("  files:\n")
		for _, path := range item.TargetFiles {
			fmt.Fprintf(&b, "    - %s\n", path)
		}
	}
	if len(item.VisibleTools) > 0 {
		fmt.Fprintf(&b, "  tools:     %s\n", strings.Join(item.VisibleTools, ", "))
	}
	if item.Capture != nil {
		fmt.Fprintf(&b, "  capture:   head=%s branch=%s dirty=%t\n",
			stringutil.FirstNonEmpty(item.Capture.HeadSHA, "(none)"),
			stringutil.FirstNonEmpty(item.Capture.Branch, "(detached)"),
			item.Capture.IsDirty,
		)
	}
	fmt.Fprintf(&b, "  created:   %s\n", item.CreatedAt.UTC().Format(time.RFC3339))
	if !item.RolledBackAt.IsZero() {
		fmt.Fprintf(&b, "  rolled back: %s\n", item.RolledBackAt.UTC().Format(time.RFC3339))
	}
	return strings.TrimRight(b.String(), "\n")
}

func cloneChangeOperation(item *ChangeOperation) *ChangeOperation {
	if item == nil {
		return nil
	}
	cp := *item
	cp.TargetFiles = append([]string(nil), item.TargetFiles...)
	cp.VisibleTools = append([]string(nil), item.VisibleTools...)
	cp.HiddenTools = append([]string(nil), item.HiddenTools...)
	cp.Capture = cloneRepoState(item.Capture)
	return &cp
}

func cloneRepoState(state *workspace.RepoState) *workspace.RepoState {
	if state == nil {
		return nil
	}
	cp := *state
	cp.Staged = append([]workspace.RepoFileState(nil), state.Staged...)
	cp.Unstaged = append([]workspace.RepoFileState(nil), state.Unstaged...)
	cp.Untracked = append([]string(nil), state.Untracked...)
	cp.Ignored = append([]string(nil), state.Ignored...)
	return &cp
}

func filterChangesByRepoRoot(items []ChangeOperation, repoRoot string) []ChangeOperation {
	repoRoot = canonicalRepoRoot(repoRoot)
	if repoRoot == "" {
		return nil
	}
	out := make([]ChangeOperation, 0, len(items))
	for _, item := range items {
		if canonicalRepoRoot(item.RepoRoot) == repoRoot {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func canonicalRepoRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
}

func manualRecoveryDetails(item *ChangeOperation, base string) string {
	parts := []string{strings.TrimSpace(base)}
	if item != nil {
		if item.CheckpointID != "" {
			parts = append(parts, "checkpoint="+item.CheckpointID)
		}
		if item.Capture != nil {
			parts = append(parts, "capture_head="+stringutil.FirstNonEmpty(item.Capture.HeadSHA, "(none)"))
		}
	}
	return strings.Join(CompactStrings(parts), "; ")
}

func CompactStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func PadField(value string) string {
	if value == "" {
		return ""
	}
	return " " + value
}
