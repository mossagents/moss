package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/mossagents/moss/kernel/port"
)

func TestCliUserIOSend(t *testing.T) {
	var buf bytes.Buffer
	io := &cliUserIO{writer: &buf}

	err := io.Send(context.Background(), port.OutputMessage{
		Type:    port.OutputText,
		Content: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "hello\n" {
		t.Fatalf("expected %q, got %q", "hello\n", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("expected 'short', got %q", got)
	}
	if got := truncate("a long string here", 5); got != "a lon..." {
		t.Fatalf("expected 'a lon...', got %q", got)
	}
}
