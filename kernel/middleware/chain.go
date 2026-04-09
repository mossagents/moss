package middleware

import (
	"context"
	"fmt"
)

// NamedMiddleware 将 Middleware 与一个唯一名称和可选的前置依赖关联。
// 用于 Chain 的顺序验证。
type NamedMiddleware struct {
	Name       string
	Middleware Middleware
	// DependsOn 声明必须在此 middleware 之前注册的 middleware 名称。
	DependsOn []string
}

// Chain 管理有序的 middleware 列表，以洋葱模型执行。
type Chain struct {
	middlewares []Middleware
	names       []string // 与 middlewares 一一对应
}

// NewChain 创建空 middleware 链。
func NewChain() *Chain {
	return &Chain{}
}

// Use 追加一个 middleware 到链尾。
func (c *Chain) Use(mw Middleware) {
	c.middlewares = append(c.middlewares, mw)
	c.names = append(c.names, "")
}

// UseNamed 追加一个命名 middleware 到链尾，并验证其声明的依赖是否满足。
// 若依赖未找到（即依赖的 middleware 尚未注册），返回 error。
func (c *Chain) UseNamed(nm NamedMiddleware) error {
	for _, dep := range nm.DependsOn {
		found := false
		for _, name := range c.names {
			if name == dep {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("middleware %q depends on %q, which is not registered or comes after it", nm.Name, dep)
		}
	}
	c.middlewares = append(c.middlewares, nm.Middleware)
	c.names = append(c.names, nm.Name)
	return nil
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
