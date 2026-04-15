package builtintools

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/runtime"
)

// RegisteredToolNamesForKernel returns the runtime-owned builtin tools
// available for the current kernel ports.
func RegisteredToolNamesForKernel(k *kernel.Kernel) []string {
	return runtime.RegisteredBuiltinToolNamesForKernel(k)
}

// RegisterForKernel installs the runtime-owned builtin tools onto the provided
// registry using the kernel's existing ports.
func RegisterForKernel(k *kernel.Kernel, reg tool.Registry) error {
	return runtime.RegisterBuiltinToolsForKernel(k, reg)
}
