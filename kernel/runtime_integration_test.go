package kernel

import (
	"context"
	"testing"

	"github.com/mossagents/moss/kernel/io"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	kt "github.com/mossagents/moss/kernel/testing"
)

func TestStartRuntimeSession_NoEventStore(t *testing.T) {
	k := New(WithLLM(&kt.MockLLM{}), WithUserIO(&io.NoOpIO{}))
	_, err := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{PermissionProfile: "workspace-write"})
	if err == nil {
		t.Fatal("expected error when EventStore not configured")
	}
}

func TestStartRuntimeSession_WithSQLiteStore(t *testing.T) {
	store, err := kruntime.NewSQLiteEventStore(":memory:")
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}

	k := New(
		WithLLM(&kt.MockLLM{}),
		WithUserIO(&io.NoOpIO{}),
		WithEventStore(store),
	)

	bp, err := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{
		PermissionProfile: "workspace-write",
		ModelProfile:      "default",
	})
	if err != nil {
		t.Fatalf("StartRuntimeSession: %v", err)
	}
	if bp.Identity.SessionID == "" {
		t.Error("blueprint should have a session ID")
	}
	if bp.EffectiveToolPolicy.TrustLevel == "" {
		t.Error("blueprint should have an effective tool policy")
	}

	// 验证事件已写入 store
	events, err := store.LoadEvents(context.Background(), bp.Identity.SessionID, -1)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != kruntime.EventTypeSessionCreated {
		t.Errorf("expected session_created, got %s", events[0].Type)
	}
}

func TestStartRuntimeSession_EventStoreGetter(t *testing.T) {
	store, err := kruntime.NewSQLiteEventStore(":memory:")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	k := New(WithEventStore(store))
	if k.EventStore() == nil {
		t.Error("EventStore() should return the configured store")
	}
	if k.RuntimeResolver() != nil {
		t.Error("RuntimeResolver() should be nil when not configured")
	}
}

func TestStartRuntimeSession_CustomResolver(t *testing.T) {
	store, _ := kruntime.NewSQLiteEventStore(":memory:")
	resolver := kruntime.NewDefaultRequestResolver(kruntime.NewDefaultPolicyCompiler())

	k := New(
		WithLLM(&kt.MockLLM{}),
		WithUserIO(&io.NoOpIO{}),
		WithEventStore(store),
		WithRuntimeResolver(resolver),
	)
	if k.RuntimeResolver() == nil {
		t.Fatal("RuntimeResolver() should return the configured resolver")
	}

	bp, err := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{
		PermissionProfile: "read-only",
	})
	if err != nil {
		t.Fatalf("StartRuntimeSession: %v", err)
	}
	if bp.Identity.SessionID == "" {
		t.Error("expected session ID")
	}
}
