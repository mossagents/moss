package session

import (
	"context"
	"testing"
)

func TestResolveKey(t *testing.T) {
	tests := []struct {
		name        string
		scope       DMScope
		channel     string
		senderID    string
		sessionHint string
		want        string
	}{
		{"main scope default", DMScopeMain, "cli", "user1", "", "main"},
		{"main scope with hint", DMScopeMain, "cli", "user1", "custom", "custom"},
		{"per-peer with sender", DMScopePerPeer, "cli", "user1", "", "direct:user1"},
		{"per-peer no sender", DMScopePerPeer, "cli", "", "", "main"},
		{"per-channel-peer", DMScopePerChannelPeer, "telegram", "alice", "", "telegram:direct:alice"},
		{"per-channel-peer no channel", DMScopePerChannelPeer, "", "alice", "", "direct:alice"},
		{"per-channel-peer no sender", DMScopePerChannelPeer, "telegram", "", "", "main"},
		{"hint overrides scope", DMScopePerChannelPeer, "telegram", "alice", "forced", "forced"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter(RouterConfig{DMScope: tt.scope}, NewManager(), nil)
			got := r.ResolveKey(tt.channel, tt.senderID, tt.sessionHint)
			if got != tt.want {
				t.Errorf("ResolveKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveCreatesSession(t *testing.T) {
	mgr := NewManager()
	r := NewRouter(RouterConfig{DMScope: DMScopeMain}, mgr, nil)

	ctx := context.Background()
	sess1, err := r.Resolve(ctx, "cli", "user1", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if sess1 == nil {
		t.Fatal("expected session, got nil")
	}

	// 同一路由键应返回同一 session
	sess2, err := r.Resolve(ctx, "cli", "user2", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if sess2.ID != sess1.ID {
		t.Errorf("expected same session ID %q, got %q", sess1.ID, sess2.ID)
	}
}

func TestResolvePerPeerIsolation(t *testing.T) {
	mgr := NewManager()
	r := NewRouter(RouterConfig{DMScope: DMScopePerPeer}, mgr, nil)

	ctx := context.Background()
	sess1, err := r.Resolve(ctx, "cli", "alice", "")
	if err != nil {
		t.Fatalf("Resolve alice: %v", err)
	}

	sess2, err := r.Resolve(ctx, "cli", "bob", "")
	if err != nil {
		t.Fatalf("Resolve bob: %v", err)
	}

	if sess1.ID == sess2.ID {
		t.Errorf("per-peer should create different sessions, got same ID %q", sess1.ID)
	}

	// Same peer should reuse session
	sess3, err := r.Resolve(ctx, "telegram", "alice", "")
	if err != nil {
		t.Fatalf("Resolve alice again: %v", err)
	}
	if sess3.ID != sess1.ID {
		t.Errorf("per-peer same user different channel should share session: %q != %q", sess3.ID, sess1.ID)
	}
}

func TestParseSessionKey(t *testing.T) {
	tests := []struct {
		key          string
		wantChannel  string
		wantSenderID string
	}{
		{"main", "", ""},
		{"direct:alice", "", "alice"},
		{"telegram:direct:bob", "telegram", "bob"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			ch, sid := ParseSessionKey(tt.key)
			if ch != tt.wantChannel || sid != tt.wantSenderID {
				t.Errorf("ParseSessionKey(%q) = (%q, %q), want (%q, %q)", tt.key, ch, sid, tt.wantChannel, tt.wantSenderID)
			}
		})
	}
}
