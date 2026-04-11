package kernel

import "github.com/mossagents/moss/kernel/session"

func streamAgentEvents(root Agent, invCtx *InvocationContext, yield func(*session.Event, error) bool) {
	if root == nil || invCtx == nil || invCtx.Agent() == nil {
		return
	}

	currentCtx := invCtx
	for {
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
		currentCtx = currentCtx.WithAgent(next).WithBranch(next.Name())
	}
}
