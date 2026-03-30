package product

import (
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

func TestSelectResumeSummaryLatest(t *testing.T) {
	summaries := []session.SessionSummary{
		{ID: "done-1", Status: session.StatusCompleted, Recoverable: false},
		{ID: "run-1", Status: session.StatusRunning, Recoverable: true},
		{ID: "fail-1", Status: session.StatusFailed, Recoverable: true},
	}
	selected, recoverable, err := SelectResumeSummary(summaries, "", true)
	if err != nil {
		t.Fatalf("select resume: %v", err)
	}
	if selected == nil || selected.ID != "run-1" {
		t.Fatalf("expected latest recoverable session run-1, got %+v", selected)
	}
	if len(recoverable) != 2 {
		t.Fatalf("expected 2 recoverable sessions, got %d", len(recoverable))
	}
}

func TestSelectResumeSummarySpecificRequiresRecoverable(t *testing.T) {
	summaries := []session.SessionSummary{
		{ID: "done-1", Status: session.StatusCompleted, Recoverable: false},
	}
	selected, _, err := SelectResumeSummary(summaries, "done-1", false)
	if err == nil {
		t.Fatal("expected non-recoverable session error")
	}
	if selected != nil {
		t.Fatalf("expected nil selection, got %+v", selected)
	}
}

func TestSummarizeSnapshot(t *testing.T) {
	now := time.Now().UTC()
	summary := SummarizeSnapshot(port.WorktreeSnapshot{
		ID:        "snap-1",
		SessionID: "sess-1",
		Mode:      port.WorktreeSnapshotGhostState,
		Note:      "before review",
		Capture: port.RepoState{
			HeadSHA: "abc123",
			Branch:  "main",
		},
		Patches:   []port.PatchSnapshotRef{{PatchID: "p1"}},
		CreatedAt: now,
	})
	if summary.ID != "snap-1" || summary.SessionID != "sess-1" {
		t.Fatalf("unexpected snapshot summary %+v", summary)
	}
	if summary.PatchCount != 1 || summary.Head != "abc123" || summary.Branch != "main" {
		t.Fatalf("unexpected snapshot summary fields %+v", summary)
	}
}
