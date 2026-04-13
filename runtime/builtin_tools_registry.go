package runtime

import (
	"github.com/mossagents/moss/kernel"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
	"strings"
)

// RegisteredBuiltinToolNames 返回 runtime 一方自带的 builtin tools 名称列表。
// 这些工具由 runtime 包直接提供，不是 prompt skills，也不是通过 MCP 桥接的外部工具。
// 当 Workspace 或 Sandbox 至少有一个可用时，注册文件系统工具。
// 当 Executor 或 Sandbox 至少有一个可用时，注册 run_command。
func RegisteredBuiltinToolNames(sb sandbox.Sandbox, ws workspace.Workspace, exec workspace.Executor) []string {
	surface := newExecutionSurface(sb, ws, exec)
	names := []string{}
	if surface.HasWorkspace() {
		names = append(names, "read_file", "write_file", "edit_file", "glob", "ls", "grep")
	}
	if surface.HasExecutor() {
		names = append(names, "run_command")
	}
	names = append(names, "http_request", "datetime", "ask_user")
	return names
}

// RegisterBuiltinTools 注册 runtime 自带的 builtin tools 到 registry。
// 优先使用 Workspace/Executor 接口；未提供时回退到 Sandbox。
// builtin tools 是 first-party runtime capability，不经过 skill prompt 解析，也不依赖 MCP transport。
func RegisterBuiltinTools(reg tool.Registry, sb sandbox.Sandbox, io kernio.UserIO, ws workspace.Workspace, exec workspace.Executor) error {
	return RegisterBuiltinToolsForKernel(nil, reg, sb, io, ws, exec)
}

func RegisterBuiltinToolsForKernel(k *kernel.Kernel, reg tool.Registry, sb sandbox.Sandbox, io kernio.UserIO, ws workspace.Workspace, exec workspace.Executor) error {
	type entry struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}

	var tools []entry
	surface := newExecutionSurface(sb, ws, exec)

	if ws != nil {
		tools = append(tools,
			entry{builtinToolSpec(readFileSpec, "runtime", true, false, false), readFileHandlerWS(ws)},
			entry{builtinToolSpec(writeFileSpec, "runtime", true, false, false), writeFileHandlerWS(ws)},
			entry{builtinToolSpec(editFileSpec, "runtime", true, false, false), editFileHandlerWS(ws)},
			entry{builtinToolSpec(globSpec, "runtime", true, false, false), globHandlerWS(ws)},
			entry{builtinToolSpec(listFilesSpec, "runtime", true, false, false), listFilesHandlerWS(ws)},
			entry{builtinToolSpec(grepSpec, "runtime", true, false, false), grepHandlerWS(ws)},
		)
	} else if surface.Sandbox() != nil {
		tools = append(tools,
			entry{builtinToolSpec(readFileSpec, "runtime", false, false, true), readFileHandler(surface.Sandbox())},
			entry{builtinToolSpec(writeFileSpec, "runtime", false, false, true), writeFileHandler(surface.Sandbox())},
			entry{builtinToolSpec(editFileSpec, "runtime", false, false, true), editFileHandler(surface.Sandbox())},
			entry{builtinToolSpec(globSpec, "runtime", false, false, true), globHandler(surface.Sandbox())},
			entry{builtinToolSpec(listFilesSpec, "runtime", false, false, true), listFilesHandler(surface.Sandbox())},
			entry{builtinToolSpec(grepSpec, "runtime", false, false, true), grepHandler(surface.Sandbox())},
		)
	}

	if exec != nil {
		tools = append(tools, entry{builtinToolSpec(runCommandSpec, "runtime", surface.WorkspacePort() != nil, true, false), runCommandHandlerExecWithPolicy(k, exec, surface.WorkspacePort())})
	} else if surface.Sandbox() != nil {
		tools = append(tools, entry{builtinToolSpec(runCommandSpec, "runtime", false, false, true), runCommandHandlerWithPolicy(k, surface.Sandbox())})
	}

	tools = append(tools, entry{builtinToolSpec(httpRequestSpec, "runtime", false, false, false), httpRequestHandlerWithPolicy(k)})
	tools = append(tools, entry{builtinToolSpec(datetimeSpec, "runtime", false, false, false), datetimeHandler()})
	tools = append(tools, entry{builtinToolSpec(askUserSpec, "runtime", false, false, false), askUserHandler(io)})

	for _, t := range tools {
		if err := reg.Register(tool.NewRawTool(t.spec, t.handler)); err != nil {
			return err
		}
	}
	return nil
}

func builtinToolSpec(spec tool.ToolSpec, owner string, requiresWorkspace, requiresExecutor, requiresSandbox bool) tool.ToolSpec {
	spec = applyRuntimeBuiltinExecutionMetadata(spec)
	spec.Source = "builtin"
	spec.Owner = strings.TrimSpace(owner)
	spec.RequiresWorkspace = requiresWorkspace
	spec.RequiresExecutor = requiresExecutor
	spec.RequiresSandbox = requiresSandbox
	return spec
}

func applyRuntimeBuiltinExecutionMetadata(spec tool.ToolSpec) tool.ToolSpec {
	switch spec.Name {
	case "datetime":
		spec.Effects = []tool.Effect{tool.EffectReadOnly}
		spec.SideEffectClass = tool.SideEffectNone
		spec.ApprovalClass = tool.ApprovalClassNone
		spec.PlannerVisibility = tool.PlannerVisibilityVisible
		spec.Idempotent = true
		spec.CommutativityClass = tool.CommutativityFullyCommutative
	case "read_file", "glob", "ls", "grep":
		spec.Effects = []tool.Effect{tool.EffectReadOnly}
		spec.ResourceScope = []string{"workspace:*"}
		spec.SideEffectClass = tool.SideEffectNone
		spec.ApprovalClass = tool.ApprovalClassNone
		spec.PlannerVisibility = tool.PlannerVisibilityVisible
		spec.Idempotent = true
		spec.CommutativityClass = tool.CommutativityFullyCommutative
	case "write_file", "edit_file":
		spec.Effects = []tool.Effect{tool.EffectWritesWorkspace}
		spec.ResourceScope = []string{"workspace:*"}
		spec.LockScope = []string{"workspace:*"}
		spec.SideEffectClass = tool.SideEffectWorkspace
		spec.ApprovalClass = tool.ApprovalClassExplicitUser
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	case "run_command":
		spec.Effects = []tool.Effect{tool.EffectExternalSideEffect}
		spec.ResourceScope = []string{"workspace:*", "process:command"}
		spec.LockScope = []string{"process:command"}
		spec.SideEffectClass = tool.SideEffectProcess
		spec.ApprovalClass = tool.ApprovalClassExplicitUser
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	case "http_request":
		spec.Effects = []tool.Effect{tool.EffectExternalSideEffect}
		spec.ResourceScope = []string{"network:http"}
		spec.LockScope = []string{"network:http"}
		spec.SideEffectClass = tool.SideEffectNetwork
		spec.ApprovalClass = tool.ApprovalClassExplicitUser
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	}
	return spec
}
