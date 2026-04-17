package builtintools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

// RegisteredBuiltinToolNamesForKernel 返回 runtime 一方自带的 builtin tools 名称列表。
func RegisteredBuiltinToolNamesForKernel(k *kernel.Kernel) []string {
	return registeredBuiltinToolNames(k)
}

func registeredBuiltinToolNames(k *kernel.Kernel) []string {
	names := []string{}
	var ws workspace.Workspace
	if k != nil {
		ws = k.Workspace()
	}
	if ws != nil {
		names = append(names, "read_file", "write_file", "edit_file", "glob", "ls", "list_files", "grep")
		if k != nil && k.PatchApply() != nil {
			names = append(names, "apply_patch")
		}
		names = append(names, "run_command")
	}
	names = append(names, "http_request", "datetime", "ask_user")
	return names
}

// RegisterBuiltinToolsForKernel 注册 runtime 自带的 builtin tools 到 registry。
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
	io := k.UserIO()
	root := workspaceRoot(ws)
	patchApply := k.PatchApply()

	if ws != nil {
		tools = append(tools,
			entry{builtinToolSpec(readFileSpec, "runtime", true), readFileHandlerWS(ws)},
			entry{builtinToolSpec(writeFileSpec, "runtime", true), writeFileHandlerWS(ws)},
			entry{builtinToolSpec(editFileSpec, "runtime", true), editFileHandlerWS(ws)},
			entry{builtinToolSpec(globSpec, "runtime", true), globHandlerPort(ws, root)},
			entry{builtinToolSpec(listFilesSpec, "runtime", true), listFilesHandlerPort(ws, root)},
			entry{builtinToolSpec(listFilesAliasSpec, "runtime", true), listFilesHandlerPort(ws, root)},
			entry{builtinToolSpec(grepSpec, "runtime", true), grepHandlerPort(ws, root)},
		)
		if patchApply != nil {
			tools = append(tools, entry{builtinToolSpec(applyPatchSpec, "runtime", true), applyPatchHandlerPort(patchApply)})
		}

		tools = append(tools, entry{builtinToolSpec(runCommandSpec, "runtime", true), runCommandHandler(k, ws, root)})
	}

	tools = append(tools, entry{builtinToolSpec(httpRequestSpec, "runtime", false), httpRequestHandlerWithPolicy(k)})
	tools = append(tools, entry{builtinToolSpec(datetimeSpec, "runtime", false), datetimeHandler()})
	tools = append(tools, entry{builtinToolSpec(askUserSpec, "runtime", false), askUserHandler(io)})

	for _, t := range tools {
		if err := reg.Register(tool.NewRawTool(t.spec, t.handler)); err != nil {
			return err
		}
	}
	return nil
}

func builtinToolSpec(spec tool.ToolSpec, owner string, requiresWorkspace bool) tool.ToolSpec {
	spec = applyRuntimeBuiltinExecutionMetadata(spec)
	spec.Source = "builtin"
	spec.Owner = strings.TrimSpace(owner)
	spec.RequiresWorkspace = requiresWorkspace
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
	case "read_file", "glob", "ls", "list_files", "grep":
		spec.Effects = []tool.Effect{tool.EffectReadOnly}
		spec.ResourceScope = []string{"workspace:*"}
		spec.SideEffectClass = tool.SideEffectNone
		spec.ApprovalClass = tool.ApprovalClassNone
		spec.PlannerVisibility = tool.PlannerVisibilityVisible
		spec.Idempotent = true
		spec.CommutativityClass = tool.CommutativityFullyCommutative
	case "write_file", "edit_file", "apply_patch":
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

func workspaceRoot(ws workspace.Workspace) string {
	if ws == nil {
		return ""
	}
	root, err := ws.ResolvePath(".")
	if err != nil {
		return ""
	}
	return filepath.Clean(root)
}
