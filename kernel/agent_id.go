package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
)

var agentEventSeq atomic.Uint64

// generateEventID produces a short unique event identifier.
func generateEventID() string {
	seq := agentEventSeq.Add(1)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("evt-%d-%s", seq, hex.EncodeToString(b))
}

// generateInvocationID produces a unique invocation identifier.
func generateInvocationID() string {
	seq := agentEventSeq.Add(1)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("inv-%d-%s", seq, hex.EncodeToString(b))
}
