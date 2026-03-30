package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeliveryQueue_RetryThenSuccess(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32

	dq, err := NewDeliveryQueue(dir, func(_ context.Context, _ OutboundMessage) error {
		n := calls.Add(1)
		if n < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	dq.policy = ExponentialRetryPolicy{MaxAttempts: 5, BaseDelay: 10 * time.Millisecond, MaxDelay: 20 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := dq.Recover(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("recover: %v", err)
	}
	if err := dq.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer dq.Stop(context.Background())

	if err := dq.Publish(OutboundMessage{Channel: "cli", To: "u1", Content: "hello"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected >=3 send attempts, got %d", calls.Load())
}

func TestDeliveryQueue_RecoveryReplaysPending(t *testing.T) {
	dir := t.TempDir()
	var firstRun atomic.Bool

	dq1, err := NewDeliveryQueue(dir, func(_ context.Context, _ OutboundMessage) error {
		firstRun.Store(true)
		return errors.New("always fail")
	})
	if err != nil {
		t.Fatal(err)
	}
	dq1.policy = ExponentialRetryPolicy{MaxAttempts: 5, BaseDelay: 200 * time.Millisecond, MaxDelay: 200 * time.Millisecond}
	ctx := context.Background()
	if err := dq1.Recover(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("recover1: %v", err)
	}
	if err := dq1.Start(ctx); err != nil {
		t.Fatalf("start1: %v", err)
	}
	if err := dq1.Publish(OutboundMessage{MessageID: "msg-recover", Channel: "cli", To: "u1", Content: "recover-me"}); err != nil {
		t.Fatalf("publish1: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	_ = dq1.Stop(context.Background())

	var replayed atomic.Int32
	dq2, err := NewDeliveryQueue(dir, func(_ context.Context, msg OutboundMessage) error {
		if msg.MessageID == "msg-recover" {
			replayed.Add(1)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	dq2.policy = ExponentialRetryPolicy{MaxAttempts: 2, BaseDelay: 10 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	if err := dq2.Recover(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("recover2: %v", err)
	}
	if err := dq2.Start(ctx); err != nil {
		t.Fatalf("start2: %v", err)
	}
	defer dq2.Stop(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if replayed.Load() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected recovered message replay; firstRun=%v replayed=%d queue=%s", firstRun.Load(), replayed.Load(), filepath.Join(dir, "queue.jsonl"))
}

func TestDeliveryQueue_DeadLetterOnExhaustedRetry(t *testing.T) {
	dir := t.TempDir()
	dq, err := NewDeliveryQueue(dir, func(_ context.Context, _ OutboundMessage) error {
		return errors.New("permanent fail")
	})
	if err != nil {
		t.Fatal(err)
	}
	dq.policy = ExponentialRetryPolicy{MaxAttempts: 2, BaseDelay: 10 * time.Millisecond, MaxDelay: 20 * time.Millisecond}

	ctx := context.Background()
	if err := dq.Recover(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("recover: %v", err)
	}
	if err := dq.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer dq.Stop(context.Background())

	if err := dq.Publish(OutboundMessage{MessageID: "msg-dead", Channel: "cli", To: "u1", Content: "x"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	dlqPath := filepath.Join(dir, "deadletter.jsonl")
	data, err := os.ReadFile(dlqPath)
	if err != nil {
		t.Fatalf("read dlq: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("deadletter should not be empty")
	}
}
