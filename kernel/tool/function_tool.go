package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// NewFunctionTool 创建类型安全的工具。
// TArgs 必须是可 JSON 反序列化的类型（通常是 struct），
// TResult 必须是可 JSON 序列化的类型。
// 如果 spec.InputSchema 为空，将自动从 TArgs 类型生成 JSON Schema。
func NewFunctionTool[TArgs any, TResult any](
	spec ToolSpec,
	fn func(ctx context.Context, args TArgs) (TResult, error),
) Tool {
	if len(spec.InputSchema) == 0 {
		spec.InputSchema = SchemaFor[TArgs]()
	}
	return &functionTool[TArgs, TResult]{spec: spec, fn: fn}
}

// NewRawTool 从原始 JSON 处理函数创建 Tool。
// 这是迁移旧 ToolHandler 风格工具的便捷方式。
func NewRawTool(spec ToolSpec, handler ToolHandler) Tool {
	return &rawTool{spec: spec, handler: handler}
}

// functionTool 是泛型的类型安全工具实现。
type functionTool[TArgs any, TResult any] struct {
	spec ToolSpec
	fn   func(ctx context.Context, args TArgs) (TResult, error)
}

func (t *functionTool[TArgs, TResult]) Name() string        { return t.spec.Name }
func (t *functionTool[TArgs, TResult]) Description() string { return t.spec.Description }
func (t *functionTool[TArgs, TResult]) Spec() ToolSpec       { return t.spec }

func (t *functionTool[TArgs, TResult]) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var input TArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("tool %q: unmarshal args: %w", t.spec.Name, err)
		}
	}
	result, err := t.fn(ctx, input)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// rawTool 包装原始 JSON 处理函数为 Tool 接口。
type rawTool struct {
	spec    ToolSpec
	handler ToolHandler
}

func (t *rawTool) Name() string        { return t.spec.Name }
func (t *rawTool) Description() string { return t.spec.Description }
func (t *rawTool) Spec() ToolSpec       { return t.spec }

func (t *rawTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	return t.handler(ctx, args)
}
