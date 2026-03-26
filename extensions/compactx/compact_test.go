package compactx

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	kt "github.com/mossagents/moss/testing"
)

func TestOffloadContextTool(t *testing.T) {
	ctx := context.Background()
	mgr := session.NewManager()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	sess, err := mgr.Create(ctx, session.SessionConfig{Goal: "demo"})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: "u1"})
	sess.AppendMessage(port.Message{Role: port.RoleAssistant, Content: "a1"})
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: "u2"})
	sess.AppendMessage(port.Message{Role: port.RoleAssistant, Content: "a2"})

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, store, mgr); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}
	_, handler, ok := reg.Get("offload_context")
	if !ok {
		t.Fatal("offload_context not registered")
	}

	input, _ := json.Marshal(map[string]any{
		"session_id":  sess.ID,
		"keep_recent": 2,
		"note":        "manual compact",
	})
	raw, err := handler(ctx, input)
	if err != nil {
		t.Fatalf("offload_context: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "offloaded" {
		t.Fatalf("unexpected status: %+v", resp)
	}
	snapshotID, _ := resp["snapshot_session"].(string)
	if snapshotID == "" {
		t.Fatalf("missing snapshot_session: %+v", resp)
	}

	got, ok := mgr.Get(sess.ID)
	if !ok {
		t.Fatal("session should still exist in manager")
	}
	if len(got.Messages) < 3 {
		t.Fatalf("compacted messages too short: %+v", got.Messages)
	}
	if got.Messages[len(got.Messages)-1].Content != "a2" {
		t.Fatalf("expected most recent dialog kept, got %+v", got.Messages[len(got.Messages)-1])
	}

	snap, err := store.Load(ctx, snapshotID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snap == nil || len(snap.Messages) < 4 {
		t.Fatalf("invalid snapshot: %+v", snap)
	}
}

func TestCompactxPromptHint(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&port.NoOpIO{}),
	)
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	WithSessionStore(store)(k)
	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "test"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(sess.Messages) == 0 {
		t.Fatal("expected system prompt message")
	}
	if !strings.Contains(sess.Messages[0].Content, "offload_context") {
		t.Fatalf("expected offload prompt hint, got %q", sess.Messages[0].Content)
	}
}
