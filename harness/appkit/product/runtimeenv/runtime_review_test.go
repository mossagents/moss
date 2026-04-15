package runtimeenv

import (
	"github.com/mossagents/moss/harness/appkit/product/changes"
	"github.com/mossagents/moss/kernel/workspace"
	"strings"
	"testing"
	"time"
)

func timeNowTest() time.Time {
	return time.Unix(10, 0).UTC()
}

func TestRenderReviewReport_StatusMode(t *testing.T) {
	report := ReviewReport{
		Mode: "status",
		Repo: ReviewRepoState{
			Available: true,
			Root:      "/repo",
			Branch:    "main",
			Dirty:     true,
			Staged:    []workspace.RepoFileState{{Path: "a.go", Status: "modified"}},
			Unstaged:  []workspace.RepoFileState{{Path: "b.go", Status: "modified"}},
			Untracked: []string{"c.go"},
			Ignored:   []string{},
		},
	}
	got := RenderReviewReport(report)
	for _, want := range []string{
		"mosscode review (status)",
		"/repo @ main", "dirty=true",
		"Staged:", "a.go (modified)",
		"Unstaged:", "b.go (modified)",
		"Untracked:", "c.go",
		"Ignored: none",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderReviewReport_RepoUnavailable(t *testing.T) {
	report := ReviewReport{
		Mode: "status",
		Repo: ReviewRepoState{Available: false, Error: "no git repo"},
	}
	got := RenderReviewReport(report)
	if !strings.Contains(got, "unavailable") || !strings.Contains(got, "no git repo") {
		t.Fatalf("unexpected output:\n%s", got)
	}
}

func TestRenderReviewReport_SnapshotsMode_Empty(t *testing.T) {
	report := ReviewReport{
		Mode:      "snapshots",
		Repo:      ReviewRepoState{Available: true, Root: "/r", Branch: "main"},
		Snapshots: nil,
	}
	got := RenderReviewReport(report)
	if !strings.Contains(got, "Snapshots: none") {
		t.Fatalf("unexpected output:\n%s", got)
	}
}

func TestRenderReviewReport_SnapshotsMode_Items(t *testing.T) {
	ts := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	report := ReviewReport{
		Mode: "snapshots",
		Repo: ReviewRepoState{Available: true, Root: "/r", Branch: "main"},
		Snapshots: []ReviewSnapshotSummary{
			{ID: "snap-1", SessionID: "sess-1", Head: "abc123", PatchCount: 3, CreatedAt: ts, Note: "checkpoint"},
		},
	}
	got := RenderReviewReport(report)
	for _, want := range []string{"snap-1", "abc123", "patches=3", "checkpoint"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestRenderReviewReport_SnapshotMode_NotFound(t *testing.T) {
	report := ReviewReport{
		Mode:     "snapshot",
		Repo:     ReviewRepoState{Available: true, Root: "/r"},
		Snapshot: nil,
	}
	got := RenderReviewReport(report)
	if !strings.Contains(got, "Snapshot: not found") {
		t.Fatalf("unexpected output:\n%s", got)
	}
}

func TestRenderReviewReport_SnapshotMode_Found(t *testing.T) {
	ts := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	report := ReviewReport{
		Mode: "snapshot",
		Repo: ReviewRepoState{Available: true, Root: "/r", Branch: "main"},
		Snapshot: &ReviewSnapshotSummary{
			ID:         "snap-1",
			SessionID:  "sess-1",
			Mode:       "full",
			Branch:     "feature",
			Head:       "abc123",
			PatchCount: 2,
			Note:       "pre-refactor",
			CreatedAt:  ts,
		},
	}
	got := RenderReviewReport(report)
	for _, want := range []string{
		"Snapshot: snap-1", "session: sess-1", "mode:    full", "branch:  feature",
		"head:    abc123", "patches: 2", "note:    pre-refactor",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestRenderReviewReport_ChangesMode_Empty(t *testing.T) {
	report := ReviewReport{
		Mode:    "changes",
		Repo:    ReviewRepoState{Available: true, Root: "/r"},
		Changes: nil,
	}
	got := RenderReviewReport(report)
	if !strings.Contains(got, "Changes: none") {
		t.Fatalf("unexpected output:\n%s", got)
	}
}

func TestRenderReviewReport_ChangesMode_Items(t *testing.T) {
	report := ReviewReport{
		Mode: "changes",
		Repo: ReviewRepoState{Available: true, Root: "/r"},
		Changes: []changes.ChangeSummary{
			{ID: "chg-1", Status: changes.ChangeStatusApplied, PatchID: "p1", Summary: "update", CreatedAt: timeNowTest()},
		},
	}
	got := RenderReviewReport(report)
	if !strings.Contains(got, "chg-1") {
		t.Fatalf("missing change id:\n%s", got)
	}
}

func TestRenderReviewReport_ChangeMode(t *testing.T) {
	report := ReviewReport{
		Mode:   "change",
		Repo:   ReviewRepoState{Available: true, Root: "/r"},
		Change: &changes.ChangeOperation{ID: "chg-x", Status: changes.ChangeStatusApplied, CreatedAt: timeNowTest()},
	}
	got := RenderReviewReport(report)
	if !strings.Contains(got, "chg-x") {
		t.Fatalf("missing change id:\n%s", got)
	}
}

// ── summarizeSnapshots ────────────────────────────────────────────────────────

func TestSummarizeSnapshots_SortedNewestFirst(t *testing.T) {
	now := time.Now().UTC()
	items := []workspace.WorktreeSnapshot{
		{ID: "old", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "new", CreatedAt: now.Add(-1 * time.Minute)},
		{ID: "mid", CreatedAt: now.Add(-30 * time.Minute)},
	}
	got := summarizeSnapshots(items)
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	if got[0].ID != "new" || got[1].ID != "mid" || got[2].ID != "old" {
		t.Fatalf("unexpected order: %v %v %v", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestSummarizeSnapshots_Empty(t *testing.T) {
	got := summarizeSnapshots(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %d items", len(got))
	}
}

// ── renderRepoFiles / renderStringFiles ───────────────────────────────────────

func TestRenderRepoFiles_Empty(t *testing.T) {
	var b strings.Builder
	renderRepoFiles(&b, "Staged", nil)
	if b.String() != "Staged: none\n" {
		t.Fatalf("unexpected: %q", b.String())
	}
}

func TestRenderRepoFiles_Items(t *testing.T) {
	var b strings.Builder
	renderRepoFiles(&b, "Unstaged", []workspace.RepoFileState{
		{Path: "main.go", Status: "M"},
		{Path: "go.sum", Status: "M"},
	})
	got := b.String()
	if !strings.Contains(got, "Unstaged:\n") || !strings.Contains(got, "main.go (M)") || !strings.Contains(got, "go.sum (M)") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRenderStringFiles_Empty(t *testing.T) {
	var b strings.Builder
	renderStringFiles(&b, "Untracked", nil)
	if b.String() != "Untracked: none\n" {
		t.Fatalf("unexpected: %q", b.String())
	}
}

func TestRenderStringFiles_Items(t *testing.T) {
	var b strings.Builder
	renderStringFiles(&b, "Untracked", []string{"foo.go", "bar.go"})
	got := b.String()
	if !strings.Contains(got, "Untracked:\n") || !strings.Contains(got, "foo.go") || !strings.Contains(got, "bar.go") {
		t.Fatalf("unexpected: %q", got)
	}
}
