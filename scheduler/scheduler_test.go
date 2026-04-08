package scheduler

import (
	"context"
	"errors"
	"github.com/mossagents/moss/kernel/session"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingStore struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingStore) SaveJobs(_ context.Context, _ []Job) error {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.release
	return nil
}

func (b *blockingStore) LoadJobs(_ context.Context) ([]Job, error) { return nil, nil }

type failingStore struct{}

func (failingStore) SaveJobs(_ context.Context, _ []Job) error { return errors.New("persist failed") }
func (failingStore) LoadJobs(_ context.Context) ([]Job, error) { return nil, nil }

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

func TestSchedulerTriggerRunsImmediately(t *testing.T) {
	s := New()
	if err := s.AddJob(Job{
		ID:       "manual",
		Schedule: "@every 1h",
		Goal:     "manual trigger",
	}); err != nil {
		t.Fatal(err)
	}

	triggered := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, func(_ context.Context, job Job) {
		select {
		case triggered <- job.ID:
		default:
		}
	})
	defer s.Stop()

	if err := s.Trigger("manual"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	select {
	case got := <-triggered:
		if got != "manual" {
			t.Fatalf("unexpected triggered job: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected triggered job to run immediately")
	}
}

func TestSchedulerPersistenceDoesNotHoldLock(t *testing.T) {
	store := &blockingStore{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	s := New(WithPersistence(store))

	done := make(chan struct{})
	go func() {
		_ = s.AddJob(Job{ID: "lock-check", Schedule: "@every 1h", Goal: "check"})
		close(done)
	}()

	select {
	case <-store.started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected save to start")
	}

	// If lock is not held during persist, Count should return immediately.
	countDone := make(chan int, 1)
	go func() {
		countDone <- s.Count()
	}()
	select {
	case c := <-countDone:
		if c != 1 {
			t.Fatalf("unexpected count: %d", c)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Count blocked, likely lock held during persistence")
	}

	close(store.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("AddJob did not finish")
	}
}

func TestSchedulerPersistErrorHandlerOverflowDrops(t *testing.T) {
	var calls atomic.Int64
	handler := func(error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
	}
	s := New(WithPersistence(failingStore{}), WithPersistErrorHandler(handler))

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx, func(context.Context, Job) {})
	defer func() {
		cancel()
		s.Stop()
	}()

	for i := 0; i < 200; i++ {
		_ = s.AddJob(Job{ID: "job-" + time.Now().Add(time.Duration(i)).Format("150405.000000000"), Schedule: "@every 1h", Goal: "overflow"})
	}

	time.Sleep(300 * time.Millisecond)
	if s.PersistErrorDrops() == 0 {
		t.Fatal("expected persist error drops > 0")
	}
	if calls.Load() == 0 {
		t.Fatal("expected persist error handler to be called")
	}
}
