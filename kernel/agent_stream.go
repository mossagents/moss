package kernel

import (
	"fmt"

	"github.com/mossagents/moss/kernel/session"
)

// maxTransferDepth limits the number of consecutive agent transfers to prevent
// infinite A→B→A loops.
const maxTransferDepth = 20

func streamAgentEvents(root Agent, invCtx *InvocationContext, yield func(*session.Event, error) bool) {
	if root == nil || invCtx == nil || invCtx.Agent() == nil {
		return
	}

	visited := make(map[string]int) // agent name → transfer count
	currentCtx := invCtx
	for transfers := 0; ; transfers++ {
		if transfers >= maxTransferDepth {
			yield(nil, fmt.Errorf("agent transfer depth limit reached (%d): possible cycle", maxTransferDepth))
			return
		}

		var next Agent
		for event, err := range currentCtx.Agent().Run(currentCtx) {
			if err != nil {
				yield(nil, err)
				return
			}
			if event == nil {
				continue
			}
			session.MaterializeEvent(currentCtx.Session(), event)
			if !yield(event, nil) {
				return
			}
			if event.Actions.TransferToAgent != "" {
				next = FindAgentInTree(root, event.Actions.TransferToAgent)
			}
			if next != nil {
				break
			}
		}

		if next == nil {
			return
		}
		visited[next.Name()]++
		if visited[next.Name()] > 2 {
			yield(nil, fmt.Errorf("agent transfer cycle detected: agent %q visited %d times", next.Name(), visited[next.Name()]))
			return
		}
		currentCtx = currentCtx.WithAgent(next).WithBranch(next.Name())
	}
}
