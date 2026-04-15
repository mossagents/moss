package runtime

import "github.com/mossagents/moss/extensions/capability"

type CapabilityStatus = capability.CapabilityStatus
type CapabilitySnapshot = capability.CapabilitySnapshot

func CapabilityStatusPath() string {
	return capability.CapabilityStatusPath()
}

func NewCapabilityReporter(path string, next CapabilityReporter) CapabilityReporter {
	return capability.NewCapabilityReporter(path, next)
}

func LoadCapabilitySnapshot(path string) (CapabilitySnapshot, error) {
	return capability.LoadCapabilitySnapshot(path)
}
