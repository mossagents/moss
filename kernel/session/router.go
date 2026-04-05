package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// DMScope 控制直接消息的会话隔离策略。
// 对标 OpenClaw session.dmScope。
type DMScope string

const (
	// DMScopeMain 所有 DM 共享主会话（默认，与 OpenClaw 一致）。
	DMScopeMain DMScope = "main"

	// DMScopePerPeer 按发送者隔离，跨通道共享。
	DMScopePerPeer DMScope = "per-peer"

	// DMScopePerChannelPeer 按通道+发送者隔离（推荐多用户场景）。
	DMScopePerChannelPeer DMScope = "per-channel-peer"
)

// RouterConfig 配置会话路由行为。
type RouterConfig struct {
	// DMScope 控制直接消息隔离策略，默认 "main"。
	DMScope DMScope `json:"dm_scope,omitempty" yaml:"dm_scope,omitempty"`

	// MainKey 主会话键名，默认 "main"。
	MainKey string `json:"main_key,omitempty" yaml:"main_key,omitempty"`

	// DefaultConfig 为自动创建的会话提供默认配置。
	DefaultConfig SessionConfig `json:"default_config,omitempty" yaml:"default_config,omitempty"`
}

// Router 根据入站消息元信息解析出应使用的 Session。
// 如果 Session 不存在则自动创建。
type Router struct {
	mu      sync.Mutex
	config  RouterConfig
	manager Manager
	store   SessionStore      // 可选，用于恢复持久化会话
	keys    map[string]string // routing key → session ID
}

// NewRouter 创建会话路由器。
func NewRouter(cfg RouterConfig, mgr Manager, store SessionStore) *Router {
	if cfg.MainKey == "" {
		cfg.MainKey = "main"
	}
	if cfg.DMScope == "" {
		cfg.DMScope = DMScopeMain
	}
	return &Router{
		config:  cfg,
		manager: mgr,
		store:   store,
		keys:    make(map[string]string),
	}
}

// ResolveKey 根据通道名和发送者 ID 生成会话路由键。
func (r *Router) ResolveKey(channel, senderID, sessionHint string) string {
	// 如果通道明确给出了 session hint，直接使用
	if sessionHint != "" {
		return sessionHint
	}

	switch r.config.DMScope {
	case DMScopePerPeer:
		if senderID != "" {
			return "direct:" + senderID
		}
		return r.config.MainKey

	case DMScopePerChannelPeer:
		if channel != "" && senderID != "" {
			return channel + ":direct:" + senderID
		}
		if senderID != "" {
			return "direct:" + senderID
		}
		return r.config.MainKey

	default: // DMScopeMain
		return r.config.MainKey
	}
}

// Resolve 根据路由键获取或创建 Session。
// 优先从内存 Manager 查找，其次从 SessionStore 恢复，最后新建。
func (r *Router) Resolve(ctx context.Context, channel, senderID, sessionHint string) (*Session, error) {
	key := r.ResolveKey(channel, senderID, sessionHint)

	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. 通过 key→ID 映射查内存
	if sessID, ok := r.keys[key]; ok {
		if sess, found := r.manager.Get(sessID); found {
			return sess, nil
		}
		// ID 失效（可能已被 Cancel），清理映射
		delete(r.keys, key)
	}

	// 2. 从持久化存储按 key 恢复
	if r.store != nil {
		if routed, ok := r.store.(RouteAwareSessionStore); ok {
			if sess, err := routed.LoadByRouteKey(ctx, key); err == nil && sess != nil {
				r.keys[key] = sess.ID
				return sess, nil
			}
		}
		if sess, err := r.store.Load(ctx, key); err == nil && sess != nil {
			r.keys[key] = sess.ID
			return sess, nil
		}
	}

	// 3. 创建新会话
	cfg := r.config.DefaultConfig
	if cfg.Goal == "" {
		cfg.Goal = "personal assistant"
	}
	cfg.Metadata = mergeMetadata(cfg.Metadata, map[string]any{
		"session_key": key,
		"channel":     channel,
		"sender_id":   senderID,
	})

	sess, err := r.manager.Create(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create session for key %q: %w", key, err)
	}
	r.keys[key] = sess.ID
	if routed, ok := r.store.(RouteAwareSessionStore); ok {
		if err := routed.SaveRouteKey(ctx, key, sess.ID); err != nil {
			return nil, fmt.Errorf("persist session route %q: %w", key, err)
		}
	}
	return sess, nil
}

// Config 返回当前路由配置。
func (r *Router) Config() RouterConfig {
	return r.config
}

func mergeMetadata(base, extra map[string]any) map[string]any {
	if base == nil {
		base = make(map[string]any)
	}
	for k, v := range extra {
		if _, exists := base[k]; !exists {
			base[k] = v
		}
	}
	return base
}

// ParseSessionKey 解析会话键格式，提取通道和发送者信息。
// 格式: "main", "direct:<peer>", "<channel>:direct:<peer>"
func ParseSessionKey(key string) (channel, senderID string) {
	// "<channel>:direct:<peer>"
	if idx := strings.Index(key, ":direct:"); idx >= 0 {
		return key[:idx], key[idx+len(":direct:"):]
	}
	// "direct:<peer>"
	if strings.HasPrefix(key, "direct:") {
		return "", key[len("direct:"):]
	}
	return "", ""
}
