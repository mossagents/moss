package agent

import (
	"context"
)

type contextKey struct{}
type sessionContextKey struct{}
type maxDepthContextKey struct{}

// DefaultMaxDelegationDepth 是委派调用的默认最大递归深度。
const DefaultMaxDelegationDepth = 3

// MaxDelegationDepth 是委派调用的最大递归深度。
// Deprecated: 使用 WithMaxDepth/MaxDepth 进行运行时配置。
const MaxDelegationDepth = DefaultMaxDelegationDepth

// WithDepth 在 context 中设置当前委派深度。
func WithDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, contextKey{}, depth)
}

// Depth 从 context 中获取当前委派深度，未设置时返回 0。
func Depth(ctx context.Context) int {
	v, _ := ctx.Value(contextKey{}).(int)
	return v
}

// WithMaxDepth 在 context 中设置最大委派深度限制。
func WithMaxDepth(ctx context.Context, maxDepth int) context.Context {
	return context.WithValue(ctx, maxDepthContextKey{}, maxDepth)
}

// MaxDepth 从 context 中获取最大委派深度，未设置时返回 DefaultMaxDelegationDepth。
func MaxDepth(ctx context.Context) int {
	v, ok := ctx.Value(maxDepthContextKey{}).(int)
	if !ok || v <= 0 {
		return DefaultMaxDelegationDepth
	}
	return v
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, sessionID)
}

func SessionID(ctx context.Context) string {
	v, _ := ctx.Value(sessionContextKey{}).(string)
	return v
}
