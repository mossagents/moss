package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mossagi/moss/kernel/session"
)

func TestParseSchedule(t *testing.T) {
	tests := []struct {
		expr     string
		wantDur  time.Duration
		wantOnce bool
		wantErr  bool
	}{
		{"@every 30m", 30 * time.Minute, false, false},
		{"@every 1h", time.Hour, false, false},
		{"@every 1h30m", 90 * time.Minute, false, false},
		{"@every 5s", 5 * time.Second, false, false},
		{"@after 10m", 10 * time.Minute, true, false},
		{"@once", 0, true, false},
		{"@every 100ms", 0, false, true}, // too short
		{"@after 100ms", 0, false, true}, // too short
		{"bad expr", 0, false, true},     // unsupported
		{"@every bad", 0, false, true},   // invalid duration
		{"@after bad", 0, false, true},   // invalid duration
	}

	for _, tt := range tests {
		dur, once, err := parseSchedule(tt.expr)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseSchedule(%q): err=%v, wantErr=%v", tt.expr, err, tt.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if dur != tt.wantDur {
			t.Errorf("parseSchedule(%q): dur=%v, want=%v", tt.expr, dur, tt.wantDur)
		}
		if once != tt.wantOnce {
			t.Errorf("parseSchedule(%q): once=%v, want=%v", tt.expr, once, tt.wantOnce)
		}
	}
}

func TestSchedulerAddRemove(t *testing.T) {
	s := New()

	err := s.AddJob(Job{
		ID:       "test-1",
		Schedule: "@every 1h",
		Goal:     "test job",
		Config:   session.SessionConfig{Goal: "test"},
	})
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	if s.Count() != 1 {
		t.Fatalf("expected 1 job, got %d", s.Count())
	}

	jobs := s.ListJobs()
	if len(jobs) != 1 || jobs[0].ID != "test-1" {
		t.Fatalf("unexpected jobs: %+v", jobs)
	}

	if err := s.RemoveJob("test-1"); err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}
	if s.Count() != 0 {
		t.Fatalf("expected 0 jobs, got %d", s.Count())
	}
}

func TestSchedulerFiresJob(t *testing.T) {
	s := New()

	var mu sync.Mutex
	var fired []string

	err := s.AddJob(Job{
		ID:       "quick",
		Schedule: "@every 1s",
		Goal:     "quick test",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	s.Start(ctx, func(_ context.Context, job Job) {
		mu.Lock()
		fired = append(fired, job.ID)
		mu.Unlock()
	})
	defer s.Stop()

	// 等待至少一次触发
	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	count := len(fired)
	mu.Unlock()

	if count < 1 {
		t.Fatalf("expected at least 1 fire, got %d", count)
	}
}

func TestSchedulerOnce(t *testing.T) {
	s := New()

	var mu sync.Mutex
	var fireCount int

	err := s.AddJob(Job{
		ID:       "one-shot",
		Schedule: "@once",
		Goal:     "run once",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	s.Start(ctx, func(_ context.Context, job Job) {
		mu.Lock()
		fireCount++
		mu.Unlock()
	})
	defer s.Stop()

	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	count := fireCount
	mu.Unlock()

	if count != 1 {
		t.Fatalf("expected exactly 1 fire for @once, got %d", count)
	}

	// Job 应该已被自动移除
	if s.Count() != 0 {
		t.Fatalf("expected 0 jobs after @once, got %d", s.Count())
	}
}
