package retry

import (
	"testing"
	"time"
)

func TestBreaker_ClosedToOpen(t *testing.T) {
	b := NewBreaker(BreakerConfig{MaxFailures: 3, ResetAfter: 100 * time.Millisecond})

	// 正常放行
	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("should allow request %d", i)
		}
		b.RecordFailure()
	}

	// 第 3 次失败后应熔断
	if b.Allow() {
		t.Error("should reject after max failures")
	}
	if b.State() != StateOpen {
		t.Errorf("expected StateOpen, got %d", b.State())
	}
}

func TestBreaker_OpenToHalfOpen(t *testing.T) {
	b := NewBreaker(BreakerConfig{MaxFailures: 1, ResetAfter: 50 * time.Millisecond})

	if !b.Allow() {
		t.Fatal("should allow first request")
	}
	b.RecordFailure()

	if b.Allow() {
		t.Error("should be open")
	}

	// 等待 reset
	time.Sleep(60 * time.Millisecond)

	if b.State() != StateHalfOpen {
		t.Errorf("expected StateHalfOpen, got %d", b.State())
	}
	if !b.Allow() {
		t.Error("half-open should allow one probe request")
	}
}

func TestBreaker_HalfOpenSuccess(t *testing.T) {
	b := NewBreaker(BreakerConfig{MaxFailures: 1, ResetAfter: 50 * time.Millisecond})

	b.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	// 半开状态允许一个试探
	b.Allow()
	b.RecordSuccess()

	if b.State() != StateClosed {
		t.Errorf("expected StateClosed after success, got %d", b.State())
	}
	if !b.Allow() {
		t.Error("should allow after recovery")
	}
}

func TestBreaker_HalfOpenFailure(t *testing.T) {
	b := NewBreaker(BreakerConfig{MaxFailures: 1, ResetAfter: 50 * time.Millisecond})

	b.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	b.Allow() // 半开试探
	b.RecordFailure()

	if b.State() != StateOpen {
		t.Errorf("expected StateOpen after half-open failure, got %d", b.State())
	}
}

func TestBreaker_SuccessResetsCount(t *testing.T) {
	b := NewBreaker(BreakerConfig{MaxFailures: 3, ResetAfter: time.Second})

	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess() // 重置

	b.RecordFailure()
	b.RecordFailure()

	// 还没到 3 次，应该允许
	if !b.Allow() {
		t.Error("success should have reset failure count")
	}
}
