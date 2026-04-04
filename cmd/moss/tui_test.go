package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel/port"
)

func TestCliUserIOSend(t *testing.T) {
	var buf bytes.Buffer
	io := &cliUserIO{writer: &buf}

	for _, msg := range []port.OutputMessage{
		{Type: port.OutputReasoning, Content: "plan next step"},
		{Type: port.OutputText, Content: "hello"},
	} {
		if err := io.Send(context.Background(), msg); err != nil {
			t.Fatal(err)
		}
	}
	if got := buf.String(); !strings.Contains(got, "plan next step") || !strings.Contains(got, "hello\n") {
		t.Fatalf("unexpected cli output: %q", got)
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
