package product

import (
	"github.com/mossagents/moss/internal/strutil"
	"context"
	"fmt"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
	"sort"
	"strings"
	"time"
)

type ReviewReport struct {
	Mode      string                  `json:"mode"`
	Workspace string                  `json:"workspace"`
	Repo      ReviewRepoState         `json:"repo"`
	Snapshots []ReviewSnapshotSummary `json:"snapshots,omitempty"`
	Snapshot  *ReviewSnapshotSummary  `json:"snapshot,omitempty"`
	Changes   []ChangeSummary         `json:"changes,omitempty"`
	Change    *ChangeOperation        `json:"change,omitempty"`
}

type ReviewRepoState struct {
	Available bool               `json:"available"`
	Root      string             `json:"root,omitempty"`
	Head      string             `json:"head,omitempty"`
	Branch    string             `json:"branch,omitempty"`
	Dirty     bool               `json:"dirty"`
	Staged    []workspace.RepoFileState `json:"staged,omitempty"`
	Unstaged  []workspace.RepoFileState `json:"unstaged,omitempty"`
	Untracked []string           `json:"untracked,omitempty"`
	Ignored   []string           `json:"ignored,omitempty"`
	Error     string             `json:"error,omitempty"`
}

type ReviewSnapshotSummary struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id,omitempty"`
	Mode       string    `json:"mode"`
	Head       string    `json:"head,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Note       string    `json:"note,omitempty"`
	PatchCount int       `json:"patch_count"`
	CreatedAt  time.Time `json:"created_at"`
}

func BuildReviewReport(ctx context.Context, workspace string, args []string) (ReviewReport, error) {
	mode := "status"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		mode = strings.ToLower(strings.TrimSpace(args[0]))
	}
	report := ReviewReport{
		Mode:      mode,
		Workspace: workspace,
	}
	capture, err := sandbox.NewGitRepoStateCapture(workspace).Capture(ctx)
	if err != nil {
		report.Repo = ReviewRepoState{Available: false, Error: err.Error()}
	} else {
		report.Repo = ReviewRepoState{
			Available: true,
			Root:      capture.RepoRoot,
			Head:      capture.HeadSHA,
			Branch:    capture.Branch,
			Dirty:     capture.IsDirty,
			Staged:    capture.Staged,
			Unstaged:  capture.Unstaged,
			Untracked: append([]string(nil), capture.Untracked...),
			Ignored:   append([]string(nil), capture.Ignored...),
		}
	}
	snapshots, err := listSnapshots(ctx, workspace)
	if err != nil && report.Repo.Error == "" {
		report.Repo.Error = err.Error()
	}

	switch mode {
	case "status":
		return report, nil
	case "snapshots":
		report.Snapshots = summarizeSnapshots(snapshots)
		return report, nil
	case "snapshot":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return ReviewReport{}, fmt.Errorf("usage: mosscode review snapshot <id>")
		}
		for _, snapshot := range snapshots {
			if snapshot.ID == args[1] {
				summary := SummarizeSnapshot(snapshot)
				report.Snapshot = &summary
				return report, nil
			}
		}
		return ReviewReport{}, fmt.Errorf("snapshot %q not found", args[1])
	case "changes":
		if !report.Repo.Available {
			return report, nil
		}
		items, err := listChangeOperationsByRepoRoot(ctx, report.Repo.Root, 20)
		if err != nil {
			return ReviewReport{}, err
		}
		report.Changes = items
		return report, nil
	case "change":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return ReviewReport{}, fmt.Errorf("usage: mosscode review change <id>")
		}
		if !report.Repo.Available {
			return ReviewReport{}, fmt.Errorf("repository state capture is unavailable")
		}
		item, err := LoadChangeOperationByRepoRoot(ctx, report.Repo.Root, strings.TrimSpace(args[1]))
		if err != nil {
			return ReviewReport{}, err
		}
		report.Change = item
		return report, nil
	default:
		return ReviewReport{}, fmt.Errorf("unknown review mode %q (supported: status, snapshots, snapshot, changes, change)", mode)
	}
}

func RenderReviewReport(report ReviewReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mosscode review (%s)\n", report.Mode)
	if report.Repo.Available {
		fmt.Fprintf(&b, "Repo: %s @ %s", report.Repo.Root, strutil.FirstNonEmpty(report.Repo.Branch, "(detached)"))
		if report.Repo.Dirty {
			b.WriteString(" dirty=true")
		}
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "Repo: unavailable err=%s\n", report.Repo.Error)
	}
	switch report.Mode {
	case "status":
		renderRepoFiles(&b, "Staged", report.Repo.Staged)
		renderRepoFiles(&b, "Unstaged", report.Repo.Unstaged)
		renderStringFiles(&b, "Untracked", report.Repo.Untracked)
		renderStringFiles(&b, "Ignored", report.Repo.Ignored)
	case "snapshots":
		if len(report.Snapshots) == 0 {
			b.WriteString("Snapshots: none\n")
			return b.String()
		}
		b.WriteString("Snapshots:\n")
		for _, snapshot := range report.Snapshots {
			fmt.Fprintf(&b, "- %s | created=%s | head=%s | patches=%d | session=%s | note=%s\n",
				snapshot.ID, snapshot.CreatedAt.UTC().Format(time.RFC3339), strutil.FirstNonEmpty(snapshot.Head, "(none)"), snapshot.PatchCount, strutil.FirstNonEmpty(snapshot.SessionID, "(none)"), strutil.FirstNonEmpty(snapshot.Note, "(none)"))
		}
	case "snapshot":
		if report.Snapshot == nil {
			b.WriteString("Snapshot: not found\n")
			return b.String()
		}
		snapshot := report.Snapshot
		fmt.Fprintf(&b, "Snapshot: %s\n", snapshot.ID)
		fmt.Fprintf(&b, "  session: %s\n", strutil.FirstNonEmpty(snapshot.SessionID, "(none)"))
		fmt.Fprintf(&b, "  mode:    %s\n", snapshot.Mode)
		fmt.Fprintf(&b, "  branch:  %s\n", strutil.FirstNonEmpty(snapshot.Branch, "(detached)"))
		fmt.Fprintf(&b, "  head:    %s\n", strutil.FirstNonEmpty(snapshot.Head, "(none)"))
		fmt.Fprintf(&b, "  patches: %d\n", snapshot.PatchCount)
		fmt.Fprintf(&b, "  note:    %s\n", strutil.FirstNonEmpty(snapshot.Note, "(none)"))
	case "changes":
		if len(report.Changes) == 0 {
			b.WriteString("Changes: none\n")
			return b.String()
		}
		b.WriteString(RenderChangeSummaries(report.Changes))
		b.WriteString("\n")
	case "change":
		b.WriteString(RenderChangeDetail(report.Change))
		b.WriteString("\n")
	}
	return b.String()
}

func SummarizeSnapshot(item workspace.WorktreeSnapshot) ReviewSnapshotSummary {
	return ReviewSnapshotSummary{
		ID:         item.ID,
		SessionID:  item.SessionID,
		Mode:       string(item.Mode),
		Head:       item.Capture.HeadSHA,
		Branch:     item.Capture.Branch,
		Note:       item.Note,
		PatchCount: len(item.Patches),
		CreatedAt:  item.CreatedAt,
	}
}

func summarizeSnapshots(items []workspace.WorktreeSnapshot) []ReviewSnapshotSummary {
	out := make([]ReviewSnapshotSummary, 0, len(items))
	for _, item := range items {
		out = append(out, SummarizeSnapshot(item))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func renderRepoFiles(b *strings.Builder, label string, items []workspace.RepoFileState) {
	if len(items) == 0 {
		fmt.Fprintf(b, "%s: none\n", label)
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(b, "- %s (%s)\n", item.Path, item.Status)
	}
}

func renderStringFiles(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		fmt.Fprintf(b, "%s: none\n", label)
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", item)
	}
}
