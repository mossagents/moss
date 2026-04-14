package io

import (
	"context"
	"sync"
	"testing"
)

func TestSyncIO_SendAsk(t *testing.T) {
	buf := NewBufferIO()
	s := NewSyncIO(buf)

	ctx := context.Background()
	if err := s.Send(ctx, OutputMessage{Type: OutputText, Content: "hello"}); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	texts := buf.SentTexts()
	if len(texts) != 1 || texts[0] != "hello" {
		t.Fatalf("unexpected sent texts: %v", texts)
	}
}

func TestSyncIO_Unwrap(t *testing.T) {
	buf := NewBufferIO()
	s := NewSyncIO(buf)
	if s.Unwrap() != buf {
		t.Fatal("Unwrap should return the underlying IO")
	}
}

func TestSyncIO_NilInnerBecomesNoOp(t *testing.T) {
	s := NewSyncIO(nil)
	if s.Unwrap() == nil {
		t.Fatal("nil inner should be replaced with NoOpIO, not nil")
	}
	// Should not panic
	ctx := context.Background()
	if err := s.Send(ctx, OutputMessage{Type: OutputText, Content: "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncIO_ConcurrentSend(t *testing.T) {
	buf := NewBufferIO()
	s := NewSyncIO(buf)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Send(ctx, OutputMessage{Type: OutputText, Content: "msg"})
		}()
	}
	wg.Wait()
	if len(buf.SentTexts()) != 50 {
		t.Fatalf("expected 50 messages, got %d", len(buf.SentTexts()))
	}
}
