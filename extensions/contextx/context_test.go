package contextx

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	kt "github.com/mossagents/moss/testing"
)

func TestCompactConversationTool(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{Responses: []port.CompletionResponse{{
			Message:    port.Message{Role: port.RoleAssistant, Content: "summary line"},
			StopReason: "end_turn",
		}}}),
		kernel.WithUserIO(&port.NoOpIO{}),
		WithSessionStore(store),
	)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("boot: %v", err)
	}
	sess, err := k.NewSession(ctx, session.SessionConfig{Goal: "x"})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: "u1"})
	sess.AppendMessage(port.Message{Role: port.RoleAssistant, Content: "a1"})
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: "u2"})
	sess.AppendMessage(port.Message{Role: port.RoleAssistant, Content: "a2"})

	_, handler, ok := k.ToolRegistry().Get("compact_conversation")
	if !ok {
		t.Fatal("compact_conversation not registered")
	}
	input, _ := json.Marshal(map[string]any{"session_id": sess.ID, "keep_recent": 2})
	raw, err := handler(ctx, input)
	if err != nil {
		t.Fatalf("compact_conversation: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["status"] != "offloaded" {
		t.Fatalf("unexpected status: %+v", out)
	}
	if !strings.Contains(out["summary"].(string), "summary") {
		t.Fatalf("unexpected summary: %+v", out)
	}
}

func TestAutoCompactMiddleware(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	llm := &kt.MockLLM{
		Responses: []port.CompletionResponse{
			{
				Message:    port.Message{Role: port.RoleAssistant, Content: "summary auto"},
				StopReason: "end_turn",
			},
			{
				Message:    port.Message{Role: port.RoleAssistant, Content: "done"},
				StopReason: "end_turn",
			},
		},
	}
	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithUserIO(&port.NoOpIO{}),
		WithSessionStore(store),
		Configure(WithTriggerDialogCount(2), WithKeepRecent(1)),
	)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("boot: %v", err)
	}
	sess, err := k.NewSession(ctx, session.SessionConfig{Goal: "x", MaxSteps: 5})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: "first"})
	sess.AppendMessage(port.Message{Role: port.RoleAssistant, Content: "second"})
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: "run"})
	_, err = k.Run(ctx, sess)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(llm.Calls) < 2 {
		t.Fatalf("expected >=2 llm calls (summary+normal), got %d", len(llm.Calls))
	}
	if v, ok := sess.GetState("last_context_snapshot"); !ok || strings.TrimSpace(v.(string)) == "" {
		t.Fatalf("expected snapshot state, got %v", v)
	}
}
