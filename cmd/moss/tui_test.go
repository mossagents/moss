package main

import (
	"bytes"
	"context"
	intr "github.com/mossagents/moss/kernel/interaction"
	"strings"
	"testing"
)

func TestCliUserIOSend(t *testing.T) {
	var buf bytes.Buffer
	io := &cliUserIO{writer: &buf}

	for _, msg := range []intr.OutputMessage{
		{Type: intr.OutputReasoning, Content: "plan next step"},
		{Type: intr.OutputText, Content: "hello"},
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
