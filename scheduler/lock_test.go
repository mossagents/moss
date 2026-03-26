package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestLocalLock_TryLock(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	unlock, err := l.TryLock(ctx, "job-1", 5*time.Second)
	if err != nil {
		t.Fatalf("first TryLock should succeed: %v", err)
	}

	// 同一 key 再次获取应失败
	_, err = l.TryLock(ctx, "job-1", 5*time.Second)
	if err != ErrLockHeld {
		t.Fatalf("expected ErrLockHeld, got %v", err)
	}

	// 不同 key 应成功
	unlock2, err := l.TryLock(ctx, "job-2", 5*time.Second)
	if err != nil {
		t.Fatalf("different key should succeed: %v", err)
	}
	unlock2()

	// unlock 后可重新获取
	unlock()
	unlock3, err := l.TryLock(ctx, "job-1", 5*time.Second)
	if err != nil {
		t.Fatalf("after unlock should succeed: %v", err)
	}
	unlock3()
}

func TestLocalLock_TTLExpiry(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	_, err := l.TryLock(ctx, "job-ttl", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("TryLock should succeed: %v", err)
	}

	// TTL 到期前应失败
	_, err = l.TryLock(ctx, "job-ttl", 50*time.Millisecond)
	if err != ErrLockHeld {
		t.Fatalf("expected ErrLockHeld before TTL")
	}

	// 等 TTL 过期
	time.Sleep(80 * time.Millisecond)

	unlock, err := l.TryLock(ctx, "job-ttl", 5*time.Second)
	if err != nil {
		t.Fatalf("after TTL expiry should succeed: %v", err)
	}
	unlock()
}
