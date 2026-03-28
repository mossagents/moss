package skill

import (
	"context"

	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/sandbox"
)

// Provider 是 runtime 可加载的能力单元抽象。
// 它统一了三类来源不同的能力：
//   - runtime 自带的 builtin tools provider
//   - 通过 SKILL.md 注入提示词的 prompt skill
//   - 通过 MCP 连接外部工具服务的 provider
// Provider 是统一生命周期接口，不意味着这些能力在来源、权限边界或实现方式上等价。
type Provider interface {
	// Metadata 返回 skill 的元信息。
	Metadata() Metadata

	// Init 初始化 skill，注册工具和中间件。
	Init(ctx context.Context, deps Deps) error

	// Shutdown 清理 skill 资源（如关闭 MCP 连接）。
	Shutdown(ctx context.Context) error
}

// Metadata 描述 provider 的元信息。
type Metadata struct {
	Name        string   `json:"name" yaml:"name"`
	Version     string   `json:"version" yaml:"version"`
	Description string   `json:"description" yaml:"description"`
	Tools       []string `json:"tools,omitempty" yaml:"tools,omitempty"`     // 提供的工具名列表
	Prompts     []string `json:"prompts,omitempty" yaml:"prompts,omitempty"` // 注入到 system prompt 的片段
}

// Deps 是 provider 初始化时可用的依赖。
type Deps struct {
	ToolRegistry tool.Registry
	Middleware   *middleware.Chain
	Sandbox      sandbox.Sandbox
	UserIO       port.UserIO
	Workspace    port.Workspace
	Executor     port.Executor
}
