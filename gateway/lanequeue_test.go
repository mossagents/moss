package gateway

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLaneQueue_FIFOPerLane(t *testing.T) {
	q := NewLaneQueue()
	q.SetLaneConcurrency("alpha", 1)

	var mu sync.Mutex
	var seq []int
	f1 := q.Enqueue(context.Background(), "alpha", func(_ context.Context) error {
		mu.Lock()
		seq = append(seq, 1)
		mu.Unlock()
		time.Sleep(40 * time.Millisecond)
		return nil
	})
	f2 := q.Enqueue(context.Background(), "alpha", func(_ context.Context) error {
		mu.Lock()
		seq = append(seq, 2)
		mu.Unlock()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := f1.Wait(ctx); err != nil {
		t.Fatalf("wait f1: %v", err)
	}
	if err := f2.Wait(ctx); err != nil {
		t.Fatalf("wait f2: %v", err)
	}

	if len(seq) != 2 || seq[0] != 1 || seq[1] != 2 {
		t.Fatalf("fifo order = %v, want [1 2]", seq)
	}
}

func TestLaneQueue_ParallelAcrossLanes(t *testing.T) {
	q := NewLaneQueue()
	q.SetLaneConcurrency("a", 1)
	q.SetLaneConcurrency("b", 1)

	start := time.Now()
	fa := q.Enqueue(context.Background(), "a", func(_ context.Context) error {
		time.Sleep(120 * time.Millisecond)
		return nil
	})
	fb := q.Enqueue(context.Background(), "b", func(_ context.Context) error {
		time.Sleep(120 * time.Millisecond)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := fa.Wait(ctx); err != nil {
		t.Fatalf("wait lane a: %v", err)
	}
	if err := fb.Wait(ctx); err != nil {
		t.Fatalf("wait lane b: %v", err)
	}

	if elapsed := time.Since(start); elapsed > 230*time.Millisecond {
		t.Fatalf("expected parallel execution, elapsed=%v", elapsed)
	}
}

func TestLaneQueue_PropagatesCancellationContext(t *testing.T) {
	q := NewLaneQueue()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})

	f := q.Enqueue(ctx, "cancel-lane", func(taskCtx context.Context) error {
		defer close(done)
		select {
		case <-taskCtx.Done():
			return taskCtx.Err()
		case <-time.After(2 * time.Second):
			t.Fatal("task context was not canceled")
			return nil
		}
	})

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	if err := f.Wait(waitCtx); err == nil {
		t.Fatal("expected cancellation error")
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("task did not observe cancellation in time")
	}
}
