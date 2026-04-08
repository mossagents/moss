package builtins

import (
	"context"
	"errors"
	"github.com/mossagents/moss/kernel/middleware"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetry_SuccessOnFirstAttempt(t *testing.T) {
	mw := Retry(RetryConfig{MaxRetries: 3})
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: &session.Session{ID: "test"},
	}

	calls := 0
	err := mw(context.Background(), mc, func(_ context.Context) error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRetry_SuccessAfterRetries(t *testing.T) {
	mw := Retry(RetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
	})
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: &session.Session{ID: "test"},
	}

	var calls int32
	err := mw(context.Background(), mc, func(_ context.Context) error {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return errors.New("transient error")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetry_ExhaustsRetries(t *testing.T) {
	mw := Retry(RetryConfig{
		MaxRetries:   2,
		InitialDelay: 5 * time.Millisecond,
		MaxDelay:     20 * time.Millisecond,
	})
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: &session.Session{ID: "test"},
	}

	var calls int32
	err := mw(context.Background(), mc, func(_ context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("persistent error")
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 初始调用 + 2 次重试 = 3
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected 3 calls (1 initial + 2 retries), got %d", calls)
	}
}

func TestRetry_SkipNonLLMPhase(t *testing.T) {
	mw := Retry(RetryConfig{MaxRetries: 3})
	mc := &middleware.Context{
		Phase:   middleware.BeforeToolCall,
		Session: &session.Session{ID: "test"},
	}

	calls := 0
	err := mw(context.Background(), mc, func(_ context.Context) error {
		calls++
		return errors.New("should not retry")
	})

	if err == nil {
		t.Fatal("expected error for non-LLM phase")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry), got %d", calls)
	}
}

func TestRetry_ShouldRetryFilter(t *testing.T) {
	retryableErr := errors.New("retryable")
	nonRetryableErr := errors.New("non-retryable")

	mw := Retry(RetryConfig{
		MaxRetries:   3,
		InitialDelay: 5 * time.Millisecond,
		ShouldRetry: func(err error) bool {
			return errors.Is(err, retryableErr)
		},
	})
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: &session.Session{ID: "test"},
	}

	calls := 0
	err := mw(context.Background(), mc, func(_ context.Context) error {
		calls++
		return nonRetryableErr
	})

	if !errors.Is(err, nonRetryableErr) {
		t.Fatalf("expected nonRetryableErr, got: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry for non-retryable), got %d", calls)
	}
}

func TestRetry_CancelledContext(t *testing.T) {
	mw := Retry(RetryConfig{
		MaxRetries:   5,
		InitialDelay: 100 * time.Millisecond,
	})
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: &session.Session{ID: "test"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := mw(ctx, mc, func(_ context.Context) error {
		calls++
		return errors.New("keep failing")
	})

	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// ─── AutoTruncate Tests ─────────────────────────────

func TestAutoTruncate_NoTruncationNeeded(t *testing.T) {
	mw := AutoTruncate(TruncateConfig{
		MaxContextTokens: 10000,
		KeepRecent:       5,
	})

	sess := &session.Session{
		ID: "test",
		Messages: []mdl.Message{
			{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("You are a helpful assistant.")}},
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("Hello")}},
			{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("Hi there!")}},
		},
	}
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: sess,
	}

	originalLen := len(sess.Messages)
	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sess.Messages) != originalLen {
		t.Fatalf("expected %d messages (no truncation), got %d", originalLen, len(sess.Messages))
	}
}

func TestAutoTruncate_TriggersWhenOverThreshold(t *testing.T) {
	mw := AutoTruncate(TruncateConfig{
		MaxContextTokens: 10, // 很低的阈值
		KeepRecent:       2,
		TokenCounter: func(msg mdl.Message) int {
			return len(mdl.ContentPartsToPlainText(msg.ContentParts))
		},
	})

	sess := &session.Session{
		ID: "test",
		Messages: []mdl.Message{
			{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("system prompt here")}},
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("first message")}},
			{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("first reply")}},
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("second message")}},
			{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("second reply")}},
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("third message")}},
			{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("third reply")}},
		},
	}
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: sess,
	}

	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 应保留: 1 system + 1 truncation notice + 2 recent dialog = 4
	if len(sess.Messages) != 4 {
		t.Fatalf("expected 4 messages after truncation, got %d", len(sess.Messages))
	}

	// 第一条应是 system
	if sess.Messages[0].Role != mdl.RoleSystem || mdl.ContentPartsToPlainText(sess.Messages[0].ContentParts) != "system prompt here" {
		t.Fatalf("first message should be original system message")
	}

	// 第二条应是截断通知
	if sess.Messages[1].Role != mdl.RoleSystem {
		t.Fatalf("second message should be truncation notice (system role)")
	}

	// 最后两条应是最近的对话
	if mdl.ContentPartsToPlainText(sess.Messages[2].ContentParts) != "third message" || mdl.ContentPartsToPlainText(sess.Messages[3].ContentParts) != "third reply" {
		t.Fatalf("last two messages should be most recent dialog")
	}
}

func TestAutoTruncate_SkipNonLLMPhase(t *testing.T) {
	mw := AutoTruncate(TruncateConfig{MaxContextTokens: 1})
	mc := &middleware.Context{
		Phase:   middleware.AfterLLM,
		Session: &session.Session{ID: "test"},
	}

	err := mw(context.Background(), mc, func(_ context.Context) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultRetry(t *testing.T) {
	mw := DefaultRetry()
	if mw == nil {
		t.Fatal("DefaultRetry should return non-nil middleware")
	}
}

func TestDefaultAutoTruncate(t *testing.T) {
	mw := DefaultAutoTruncate()
	if mw == nil {
		t.Fatal("DefaultAutoTruncate should return non-nil middleware")
	}
}
