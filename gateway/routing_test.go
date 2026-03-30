package gateway

import "testing"

func TestBindingTable_ResolveByTier(t *testing.T) {
	tb := NewBindingTable()
	tb.Add(BindingRule{AgentID: "def", Tier: 5, MatchKey: "default", MatchValue: "*", Priority: 0})
	tb.Add(BindingRule{AgentID: "ch", Tier: 4, MatchKey: "channel", MatchValue: "telegram", Priority: 0})
	tb.Add(BindingRule{AgentID: "acct", Tier: 3, MatchKey: "account", MatchValue: "bot-1", Priority: 0})
	tb.Add(BindingRule{AgentID: "guild", Tier: 2, MatchKey: "guild", MatchValue: "g-1", Priority: 0})
	tb.Add(BindingRule{AgentID: "peer", Tier: 1, MatchKey: "peer", MatchValue: "telegram:u-1", Priority: 0})

	meta := RouteMeta{Channel: "telegram", AccountID: "bot-1", GuildID: "g-1", PeerID: "u-1"}
	agent, _, ok := tb.Resolve(meta)
	if !ok {
		t.Fatal("expected rule match")
	}
	if agent != "peer" {
		t.Fatalf("agent=%s want peer", agent)
	}
}

func TestBindingTable_PriorityWithinTier(t *testing.T) {
	tb := NewBindingTable()
	tb.Add(BindingRule{AgentID: "low", Tier: 4, MatchKey: "channel", MatchValue: "discord", Priority: 1})
	tb.Add(BindingRule{AgentID: "high", Tier: 4, MatchKey: "channel", MatchValue: "discord", Priority: 9})

	agent, _, ok := tb.Resolve(RouteMeta{Channel: "discord"})
	if !ok {
		t.Fatal("expected match")
	}
	if agent != "high" {
		t.Fatalf("agent=%s want high", agent)
	}
}

func TestBuildSessionKeyScopes(t *testing.T) {
	meta := RouteMeta{Channel: "telegram", AccountID: "bot", PeerID: "u1"}

	if got := BuildSessionKey("agentX", RouteScopePerPeer, meta); got != "agent:agentx:direct:u1" {
		t.Fatalf("per-peer key=%q", got)
	}
	if got := BuildSessionKey("agentX", RouteScopePerChannelPeer, meta); got != "agent:agentx:telegram:direct:u1" {
		t.Fatalf("per-channel-peer key=%q", got)
	}
	if got := BuildSessionKey("agentX", RouteScopePerAccountChannelPeer, meta); got != "agent:agentx:telegram:bot:direct:u1" {
		t.Fatalf("per-account-channel-peer key=%q", got)
	}
	if got := BuildSessionKey("agentX", RouteScopeMain, RouteMeta{}); got != "agent:agentx:main" {
		t.Fatalf("main key=%q", got)
	}
}
