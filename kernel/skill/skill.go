package skill

import (
	"context"

	"github.com/mossagi/moss/kernel/middleware"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/tool"
)

// Skill 是可加载的能力单元：一组工具 + 系统提示词 + 可选中间件。
type Skill interface {
	// Metadata 返回 skill 的元信息。
	Metadata() Metadata

	// Init 初始化 skill，注册工具和中间件。
	Init(ctx context.Context, deps Deps) error

	// Shutdown 清理 skill 资源（如关闭 MCP 连接）。
	Shutdown(ctx context.Context) error
}

// Metadata 描述 skill 的元信息。
type Metadata struct {
	Name        string   `json:"name" yaml:"name"`
	Version     string   `json:"version" yaml:"version"`
	Description string   `json:"description" yaml:"description"`
	Tools       []string `json:"tools,omitempty" yaml:"tools,omitempty"`     // 提供的工具名列表
	Prompts     []string `json:"prompts,omitempty" yaml:"prompts,omitempty"` // 注入到 system prompt 的片段
}

// Deps 是 skill 初始化时可用的依赖。
type Deps struct {
	ToolRegistry tool.Registry
	Middleware   *middleware.Chain
	Sandbox      sandbox.Sandbox
	UserIO       port.UserIO
}
