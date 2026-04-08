package agent

import (
	"context"
)

type contextKey struct{}
type sessionContextKey struct{}

// MaxDelegationDepth 是委派调用的最大递归深度。
const MaxDelegationDepth = 3

// WithDepth 在 context 中设置当前委派深度。
func WithDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, contextKey{}, depth)
}

// Depth 从 context 中获取当前委派深度，未设置时返回 0。
func Depth(ctx context.Context) int {
	v, _ := ctx.Value(contextKey{}).(int)
	return v
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, sessionID)
}

func SessionID(ctx context.Context) string {
	v, _ := ctx.Value(sessionContextKey{}).(string)
	return v
}
