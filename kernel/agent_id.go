package kernel

import (
	"github.com/mossagents/moss/kernel/ids"
)

// generateEventID produces a short unique event identifier.
func generateEventID() string {
	return ids.NewPrefixed("evt")
}

// generateInvocationID produces a unique invocation identifier.
func generateInvocationID() string {
	return ids.NewPrefixed("inv")
}
