package gateway

import (
	"fmt"
	"strings"
)

type RouteScope string

const (
	RouteScopeMain                  RouteScope = "main"
	RouteScopePerPeer               RouteScope = "per-peer"
	RouteScopePerChannelPeer        RouteScope = "per-channel-peer"
	RouteScopePerAccountChannelPeer RouteScope = "per-account-channel-peer"
)

type RouteMeta struct {
	Channel   string
	AccountID string
	GuildID   string
	PeerID    string
}

type BindingRule struct {
	AgentID    string
	Tier       int
	MatchKey   string
	MatchValue string
	Priority   int
}

type BindingTable struct {
	rules []BindingRule
}

func NewBindingTable() *BindingTable {
	return &BindingTable{}
}

func (t *BindingTable) Add(rule BindingRule) {
	t.rules = append(t.rules, rule)
	// simple insertion sort semantics by tier asc, priority desc
	for i := len(t.rules) - 1; i > 0; i-- {
		prev, cur := t.rules[i-1], t.rules[i]
		if prev.Tier < cur.Tier {
			break
		}
		if prev.Tier == cur.Tier && prev.Priority >= cur.Priority {
			break
		}
		t.rules[i-1], t.rules[i] = t.rules[i], t.rules[i-1]
	}
}

func (t *BindingTable) Resolve(meta RouteMeta) (string, BindingRule, bool) {
	for _, r := range t.rules {
		switch r.MatchKey {
		case "peer":
			if r.MatchValue == meta.PeerID || r.MatchValue == fmt.Sprintf("%s:%s", meta.Channel, meta.PeerID) {
				return r.AgentID, r, true
			}
		case "guild":
			if r.MatchValue == meta.GuildID {
				return r.AgentID, r, true
			}
		case "account":
			if r.MatchValue == meta.AccountID {
				return r.AgentID, r, true
			}
		case "channel":
			if r.MatchValue == meta.Channel {
				return r.AgentID, r, true
			}
		case "default":
			return r.AgentID, r, true
		}
	}
	return "", BindingRule{}, false
}

func BuildSessionKey(agentID string, scope RouteScope, meta RouteMeta) string {
	aid := strings.ToLower(strings.TrimSpace(agentID))
	if aid == "" {
		aid = "main"
	}
	channel := strings.ToLower(strings.TrimSpace(meta.Channel))
	if channel == "" {
		channel = "unknown"
	}
	account := strings.ToLower(strings.TrimSpace(meta.AccountID))
	if account == "" {
		account = "default"
	}
	peer := strings.ToLower(strings.TrimSpace(meta.PeerID))

	switch scope {
	case RouteScopePerAccountChannelPeer:
		if peer != "" {
			return fmt.Sprintf("agent:%s:%s:%s:direct:%s", aid, channel, account, peer)
		}
	case RouteScopePerChannelPeer:
		if peer != "" {
			return fmt.Sprintf("agent:%s:%s:direct:%s", aid, channel, peer)
		}
	case RouteScopePerPeer:
		if peer != "" {
			return fmt.Sprintf("agent:%s:direct:%s", aid, peer)
		}
	}
	return fmt.Sprintf("agent:%s:main", aid)
}
