package sandbox

import (
	"github.com/mossagents/moss/kernel/workspace"
)

// 以下类型别名保持向后兼容：外部包无需更改 import。
// 接口与类型定义已移入 kernel/workspace，以消除 kernel → sandbox 的层次违反。

type (
	Sandbox         = workspace.Sandbox
	ResourceLimits  = workspace.ResourceLimits
	NetworkPolicy   = workspace.NetworkPolicy
	IsolationLevel  = workspace.SandboxIsolationLevel
	ResourceUsage   = workspace.ResourceUsage
	ResourceMonitor = workspace.ResourceMonitor
)

// Output 是命令执行的结果。
type Output = workspace.ExecOutput

const (
	IsolationNone      IsolationLevel = workspace.SandboxIsolationNone
	IsolationProcess   IsolationLevel = workspace.SandboxIsolationProcess
	IsolationContainer IsolationLevel = workspace.SandboxIsolationContainer
	IsolationVM        IsolationLevel = workspace.SandboxIsolationVM
)

// LimitsExceeded 检查资源使用是否超过配置限制。
func LimitsExceeded(limits ResourceLimits, usage ResourceUsage) []string {
	return workspace.LimitsExceeded(limits, usage)
}
