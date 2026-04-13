package runtime

import (
	"fmt"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
	"strings"
)

// RegisteredBuiltinToolNames 返回 runtime 一方自带的 builtin tools 名称列表。
// 这些工具由 runtime 包直接提供，不是 prompt skills，也不是通过 MCP 桥接的外部工具。
// 当 Workspace 可用时，注册文件系统工具。
// 当 Executor 可用时，注册 run_command。
func RegisteredBuiltinToolNamesForKernel(k *kernel.Kernel) []string {
	if k == nil {
		return registeredBuiltinToolNames(nil, nil)
	}
	return registeredBuiltinToolNames(k.Workspace(), k.Executor())
}

func registeredBuiltinToolNames(ws workspace.Workspace, exec workspace.Executor) []string {
	names := []string{}
	if ws != nil {
		names = append(names, "read_file", "write_file", "edit_file", "glob", "ls", "grep")
	}
	if exec != nil {
		names = append(names, "run_command")
	}
	names = append(names, "http_request", "datetime", "ask_user")
	return names
}

// RegisterBuiltinToolsForKernel 注册 runtime 自带的 builtin tools 到 registry。
// builtin tools 只消费已经安装到 Kernel 上的端口，不再自行重建 execution bridge。
func RegisterBuiltinToolsForKernel(k *kernel.Kernel, reg tool.Registry) error {
	if k == nil {
		return fmt.Errorf("kernel is nil")
	}
	type entry struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}

	var tools []entry
	ws := k.Workspace()
	exec := k.Executor()
	io := k.UserIO()
	root := sandboxRoot(k.Sandbox())

	if ws != nil {
		tools = append(tools,
			entry{builtinToolSpec(readFileSpec, "runtime", true, false, false), readFileHandlerWS(ws)},
			entry{builtinToolSpec(writeFileSpec, "runtime", true, false, false), writeFileHandlerWS(ws)},
			entry{builtinToolSpec(editFileSpec, "runtime", true, false, false), editFileHandlerWS(ws)},
			entry{builtinToolSpec(globSpec, "runtime", true, false, false), globHandlerPort(ws, root)},
			entry{builtinToolSpec(listFilesSpec, "runtime", true, false, false), listFilesHandlerPort(ws, root)},
			entry{builtinToolSpec(grepSpec, "runtime", true, false, false), grepHandlerPort(ws, root)},
		)
	}

	if exec != nil {
		tools = append(tools, entry{builtinToolSpec(runCommandSpec, "runtime", ws != nil, true, false), runCommandHandlerWithExecutor(k, exec, ws, root)})
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
