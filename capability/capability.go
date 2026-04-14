package capability

import (
	"context"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
)

// Provider is a runtime-loadable capability unit.
type Provider interface {
	Metadata() Metadata
	Init(ctx context.Context, deps Deps) error
	Shutdown(ctx context.Context) error
}

// Dependency describes a version-constrained dependency on another provider.
type Dependency struct {
	Name       string `json:"name" yaml:"name"`
	MinVersion string `json:"min_version,omitempty" yaml:"min_version,omitempty"`
	MaxVersion string `json:"max_version,omitempty" yaml:"max_version,omitempty"`
}

// Metadata describes provider metadata.
type Metadata struct {
	Name        string       `json:"name" yaml:"name"`
	Version     string       `json:"version" yaml:"version"`
	Description string       `json:"description" yaml:"description"`
	Tools       []string     `json:"tools,omitempty" yaml:"tools,omitempty"`
	Prompts     []string     `json:"prompts,omitempty" yaml:"prompts,omitempty"`
	DependsOn   []string     `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Requires    []Dependency `json:"requires,omitempty" yaml:"requires,omitempty"`
	RequiredEnv []string     `json:"required_env,omitempty" yaml:"required_env,omitempty"`
}

// Deps are the runtime dependencies available when a provider initializes.
type Deps struct {
	Kernel       *kernel.Kernel
	ToolRegistry tool.Registry
	Sandbox      sandbox.Sandbox
	UserIO       io.UserIO
	Workspace    workspace.Workspace
	Executor     workspace.Executor
	TaskRuntime  taskrt.TaskRuntime
	Mailbox      taskrt.Mailbox
	SessionStore session.SessionStore
}
