package middleware

import (
	"context"
)

// Chain 管理有序的 middleware 列表，以洋葱模型执行。
type Chain struct {
	middlewares []Middleware
}

// NewChain 创建空 middleware 链。
func NewChain() *Chain {
	return &Chain{}
}

// Use 追加一个 middleware 到链尾。
func (c *Chain) Use(mw Middleware) {
	c.middlewares = append(c.middlewares, mw)
}

// Run 以洋葱模型执行所有 middleware。
// 仅针对指定 phase 执行；如果 middleware 不关心当前 phase，应直接调用 next。
func (c *Chain) Run(ctx context.Context, phase Phase, mc *Context) error {
	mc.Phase = phase
	return c.execute(ctx, mc, 0)
}

func (c *Chain) execute(ctx context.Context, mc *Context, index int) error {
	if index >= len(c.middlewares) {
		return nil
	}
	return c.middlewares[index](ctx, mc, func(ctx context.Context) error {
		return c.execute(ctx, mc, index+1)
	})
}
