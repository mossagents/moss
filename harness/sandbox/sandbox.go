package sandbox

import (
	"github.com/mossagents/moss/kernel/workspace"
)

// 以下类型别名保持 harness/sandbox 包内部一致性。

type (
	ResourceLimits  = workspace.ResourceLimits
	NetworkPolicy   = workspace.NetworkPolicy
	IsolationLevel  = workspace.SandboxIsolationLevel
	ResourceUsage   = workspace.ResourceUsage
	ResourceMonitor = workspace.ResourceMonitor
)

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
