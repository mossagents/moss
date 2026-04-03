package events

import (
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

func TestFromOutputMessage(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	ev := FromOutputMessage(port.OutputMessage{Type: port.OutputStream, Content: "hi"}, now)
	if ev.Type != EventAssistantDelta {
		t.Fatalf("want %q got %q", EventAssistantDelta, ev.Type)
	}
	if ev.Content != "hi" {
		t.Fatalf("unexpected content: %q", ev.Content)
	}
}

func TestFromExecutionEvent(t *testing.T) {
	ts := time.Unix(200, 0).UTC()
	ev := FromExecutionEvent(port.ExecutionEvent{
		Type:      port.ExecutionToolCompleted,
		Timestamp: ts,
		SessionID: "s1",
		ToolName:  "read_file",
		CallID:    "c1",
	})
	if ev.Type != EventToolCompleted {
		t.Fatalf("want %q got %q", EventToolCompleted, ev.Type)
	}
	if ev.SessionID != "s1" {
		t.Fatalf("unexpected session id: %q", ev.SessionID)
	}
	if ev.Meta["tool_name"] != "read_file" {
		t.Fatalf("missing tool_name meta")
	}
}
