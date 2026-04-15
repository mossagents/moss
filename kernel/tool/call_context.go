package tool

import (
	"context"
)

type toolContextKey struct{}

// ToolCallContext 描述一次工具调用的运行时上下文。
type ToolCallContext struct {
	SessionID string
	ToolName  string
	CallID    string
}

// WithToolCallContext 将工具调用上下文附加到 context。
func WithToolCallContext(ctx context.Context, meta ToolCallContext) context.Context {
	return context.WithValue(ctx, toolContextKey{}, meta)
}

// ToolCallContextFromContext 读取 context 中的工具调用上下文。
func ToolCallContextFromContext(ctx context.Context) (ToolCallContext, bool) {
	meta, ok := ctx.Value(toolContextKey{}).(ToolCallContext)
	return meta, ok
}
