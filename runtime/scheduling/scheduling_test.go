package scheduling

import (
	"context"
	"github.com/mossagents/moss/scheduler"
	"strings"
	"testing"
	"time"
)

func TestSchedulerAdapterListAndText(t *testing.T) {
	s := scheduler.New()
	now := time.Now()
	if err := s.AddJob(scheduler.Job{
		ID:       "b-job",
		Schedule: "@every 2h",
		Goal:     "  second  ",
		LastRun:  now.Add(-time.Hour),
		RunCount: 2,
	}); err != nil {
		t.Fatalf("AddJob b-job: %v", err)
	}
	if err := s.AddJob(scheduler.Job{
		ID:       "a-job",
		Schedule: "@every 1h",
		Goal:     "first",
	}); err != nil {
		t.Fatalf("AddJob a-job: %v", err)
	}

	items, err := (SchedulerAdapter{Scheduler: s}).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != "a-job" || items[1].ID != "b-job" {
		t.Fatalf("items not sorted by id: %#v", items)
	}
	if items[1].Goal != "second" {
		t.Fatalf("expected trimmed goal, got %q", items[1].Goal)
	}

	text, err := (SchedulerAdapter{Scheduler: s}).ListText()
	if err != nil {
		t.Fatalf("ListText: %v", err)
	}
	for _, want := range []string{"Schedules (2):", "a-job | @every 1h", "b-job | @every 2h", "runs: 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ListText missing %q in %q", want, text)
		}
	}
}

func TestSchedulerAdapterCancelAndRunNow(t *testing.T) {
	s := scheduler.New()
	triggered := make(chan string, 1)
	s.Start(context.Background(), func(_ context.Context, job scheduler.Job) {
		triggered <- job.ID
	})
	defer s.Stop()

	if err := s.AddJob(scheduler.Job{ID: "review", Schedule: "@every 1h", Goal: "run review"}); err != nil {
		t.Fatalf("AddJob review: %v", err)
	}
	adapter := SchedulerAdapter{Scheduler: s}

	out, err := adapter.RunNow("review")
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !strings.Contains(out, "started immediately") {
		t.Fatalf("unexpected RunNow output: %q", out)
	}

	select {
	case id := <-triggered:
		if id != "review" {
			t.Fatalf("unexpected triggered id: %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for triggered job")
	}

	out, err = adapter.Cancel("review")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !strings.Contains(out, "deleted") {
		t.Fatalf("unexpected Cancel output: %q", out)
	}
}

