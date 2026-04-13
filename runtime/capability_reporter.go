package runtime

import "context"

// CapabilityReporter receives capability state changes during runtime assembly
// and capability status reporting.
type CapabilityReporter interface {
	Report(ctx context.Context, capability string, critical bool, state string, err error)
}
