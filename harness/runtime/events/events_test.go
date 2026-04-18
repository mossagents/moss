package events

import (
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
)

var now = time.Unix(100, 0).UTC()

func TestFromOutputMessage_AllTypes(t *testing.T) {
	cases := []struct {
		typ  io.OutputType
		want StreamEventType
	}{
		{io.OutputText, EventAssistantMessage},
		{io.OutputStream, EventAssistantDelta},
		{io.OutputReasoning, EventAssistantReasoning},
		{io.OutputRefusal, EventAssistantRefusal},
		{io.OutputHostedTool, EventAssistantHostedTool},
		{io.OutputStreamEnd, EventAssistantDone},
		{io.OutputProgress, EventProgress},
		{io.OutputToolStart, EventToolStarted},
		{io.OutputToolResult, EventToolCompleted},
		{io.OutputType("unknown_type"), EventUnknown},
	}
	for _, tc := range cases {
		ev := FromOutputMessage(io.OutputMessage{Type: tc.typ, Content: "x"}, now)
		if ev.Type != tc.want {
			t.Errorf("type=%q: want %q got %q", tc.typ, tc.want, ev.Type)
		}
		if ev.Content != "x" {
			t.Errorf("type=%q: unexpected content %q", tc.typ, ev.Content)
		}
		if ev.Timestamp != now {
			t.Errorf("type=%q: unexpected timestamp", tc.typ)
		}
	}
}

func TestFromOutputMessage_MetaCloned(t *testing.T) {
	meta := map[string]any{"k": "v"}
	ev := FromOutputMessage(io.OutputMessage{Type: io.OutputText, Meta: meta}, now)
	if ev.Metadata["k"] != "v" {
		t.Fatal("metadata not cloned")
	}
	meta["k"] = "changed"
	if ev.Metadata["k"] != "v" {
		t.Fatal("metadata not isolated from source")
	}
}

func TestFromOutputMessage_NoMeta(t *testing.T) {
	ev := FromOutputMessage(io.OutputMessage{Type: io.OutputText}, now)
	if ev.Metadata != nil {
		t.Fatalf("expected nil metadata, got %v", ev.Metadata)
	}
}

func TestFromExecutionEvent_AllTypes(t *testing.T) {
	ts := time.Unix(200, 0).UTC()
	cases := []struct {
		typ  observe.ExecutionEventType
		want StreamEventType
	}{
		{observe.ExecutionRunStarted, EventRunStarted},
		{observe.ExecutionRunCompleted, EventRunCompleted},
		{observe.ExecutionRunFailed, EventRunFailed},
		{observe.ExecutionRunCancelled, EventRunFailed},
		{observe.ExecutionToolStarted, EventToolStarted},
		{observe.ExecutionToolCompleted, EventToolCompleted},
		{observe.ExecutionHostedToolStarted, EventHostedToolStarted},
		{observe.ExecutionHostedToolProgress, EventHostedToolProgress},
		{observe.ExecutionHostedToolCompleted, EventHostedToolCompleted},
		{observe.ExecutionHostedToolFailed, EventHostedToolFailed},
		{observe.ExecutionIterationProgress, EventProgress},
		{observe.ExecutionEventType("other"), EventUnknown},
	}
	for _, tc := range cases {
		ev := FromExecutionEvent(observe.ExecutionEvent{Type: tc.typ, Timestamp: ts})
		if ev.Type != tc.want {
			t.Errorf("type=%q: want %q got %q", tc.typ, tc.want, ev.Type)
		}
	}
}

func TestFromExecutionEvent_Metadata(t *testing.T) {
	ts := time.Unix(200, 0).UTC()
	ev := FromExecutionEvent(observe.ExecutionEvent{
		Type:      observe.ExecutionToolCompleted,
		Timestamp: ts,
		SessionID: "s1",
		ToolName:  "read_file",
		CallID:    "c1",
		Model:     "gpt-4",
		Error:     "some error",
		Metadata:  map[string]any{"extra": "data"},
	})
	if ev.Type != EventToolCompleted {
		t.Fatalf("want EventToolCompleted, got %q", ev.Type)
	}
	if ev.SessionID != "s1" {
		t.Fatalf("unexpected session id: %q", ev.SessionID)
	}
	if ev.Content != "some error" {
		t.Fatalf("unexpected content: %q", ev.Content)
	}
	if ev.Metadata["tool_name"] != "read_file" {
		t.Fatal("missing tool_name in metadata")
	}
	if ev.Metadata["call_id"] != "c1" {
		t.Fatal("missing call_id in metadata")
	}
	if ev.Metadata["model"] != "gpt-4" {
		t.Fatal("missing model in metadata")
	}
	if ev.Metadata["extra"] != "data" {
		t.Fatal("extra metadata not preserved")
	}
}

func TestFromExecutionEvent_NilMetaWithFields(t *testing.T) {
	ev := FromExecutionEvent(observe.ExecutionEvent{
		Type:     observe.ExecutionRunStarted,
		ToolName: "tool_x",
		CallID:   "cx",
		Model:    "m1",
	})
	if ev.Metadata == nil {
		t.Fatal("expected metadata to be initialized")
	}
	if ev.Metadata["tool_name"] != "tool_x" {
		t.Fatal("tool_name not set")
	}
	if ev.Metadata["call_id"] != "cx" {
		t.Fatal("call_id not set")
	}
}

func TestFromExecutionEvent_NoExtraFields(t *testing.T) {
	ev := FromExecutionEvent(observe.ExecutionEvent{Type: observe.ExecutionRunStarted})
	if ev.Metadata == nil {
		t.Fatal("expected non-nil empty metadata map")
	}
	if len(ev.Metadata) != 0 {
		t.Fatalf("unexpected metadata entries: %v", ev.Metadata)
	}
}

func TestCloneMap_Empty(t *testing.T) {
	if cloneMap(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
	if cloneMap(map[string]any{}) != nil {
		t.Fatal("expected nil for empty map")
	}
}

func TestCloneMap_Clones(t *testing.T) {
	src := map[string]any{"a": 1, "b": "two"}
	dst := cloneMap(src)
	if dst["a"] != 1 || dst["b"] != "two" {
		t.Fatalf("unexpected clone: %v", dst)
	}
	src["a"] = 99
	if dst["a"] != 1 {
		t.Fatal("clone should be independent of source")
	}
}
