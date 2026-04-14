package capability

import "github.com/mossagents/moss/kernel"

// KernelStateServiceKey stores runtime capability state on a kernel service registry.
const KernelStateServiceKey kernel.ServiceKey = "capabilities.state"

type managerStateAccessor interface {
	CapabilityManager() *Manager
}

// LookupManager returns the current capability manager if capability state has
// already been assembled on the kernel.
func LookupManager(k *kernel.Kernel) (*Manager, bool) {
	if k == nil {
		return nil, false
	}
	actual, ok := k.Services().Load(KernelStateServiceKey)
	if !ok || actual == nil {
		return nil, false
	}
	state, ok := actual.(managerStateAccessor)
	if !ok || state == nil {
		return nil, false
	}
	manager := state.CapabilityManager()
	if manager == nil {
		return nil, false
	}
	return manager, true
}
