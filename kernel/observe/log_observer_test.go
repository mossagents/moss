package observe

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLogObserverOnLLMCall(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	obs := NewLogObserver(logger, slog.LevelInfo)

	obs.OnLLMCall(context.Background(), LLMCallEvent{
		SessionID:  "s1",
		Model:      "gpt-4",
		Duration:   150 * time.Millisecond,
		StopReason: "end_turn",
		Streamed:   true,
	})

	output := buf.String()
	for _, want := range []string{"llm.call", "session_id=s1", "model=gpt-4", "streamed=true"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestLogObserverOnLLMCallWithError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	obs := NewLogObserver(logger, slog.LevelInfo)

	obs.OnLLMCall(context.Background(), LLMCallEvent{
		SessionID: "s2",
		Model:     "gpt-4",
		Error:     errors.New("timeout"),
	})

	output := buf.String()
	if !strings.Contains(output, "error=timeout") {
		t.Errorf("output missing error:\n%s", output)
	}
}

func TestLogObserverOnToolCall(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	obs := NewLogObserver(logger, slog.LevelInfo)

	obs.OnToolCall(context.Background(), ToolCallEvent{
		SessionID: "s1",
		ToolName:  "read_file",
		Risk:      "low",
		Duration:  50 * time.Millisecond,
	})

	output := buf.String()
	for _, want := range []string{"tool.call", "tool_name=read_file", "risk=low"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestLogObserverOnSessionEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	obs := NewLogObserver(logger, slog.LevelInfo)

	obs.OnSessionEvent(context.Background(), SessionEvent{
		SessionID: "s1",
		Type:      "running",
	})

	output := buf.String()
	if !strings.Contains(output, "session.event") {
		t.Errorf("output missing session.event:\n%s", output)
	}
	if !strings.Contains(output, "type=running") {
		t.Errorf("output missing type=running:\n%s", output)
	}
}

func TestLogObserverOnError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	obs := NewLogObserver(logger, slog.LevelInfo)

	obs.OnError(context.Background(), ErrorEvent{
		SessionID: "s1",
		Phase:     "llm_call",
		Message:   "something broke",
		Error:     errors.New("underlying"),
	})

	output := buf.String()
	for _, want := range []string{"error", "phase=llm_call", "message=\"something broke\""} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestLogObserverLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	obs := NewLogObserver(logger, slog.LevelInfo)

	obs.OnLLMCall(context.Background(), LLMCallEvent{
		SessionID: "s1",
		Model:     "gpt-4",
	})

	if buf.Len() != 0 {
		t.Errorf("expected no output at warn level, got:\n%s", buf.String())
	}
}

func TestLogObserverImplementsObserver(t *testing.T) {
	var _ Observer = (*LogObserver)(nil)
}
