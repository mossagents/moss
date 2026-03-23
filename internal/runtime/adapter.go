package runtime

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/mossagi/moss/internal/approval"
	"github.com/mossagi/moss/internal/events"
	"github.com/mossagi/moss/internal/tools"
	"github.com/mossagi/moss/internal/workspace"
)

var eventCounter atomic.Uint64

func newEventID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), eventCounter.Add(1))
}

// RunRequest represents a request to start an agent run
type RunRequest struct {
	RunID        string
	Goal         string
	AgentName    string
	Instructions string
	Tools        []string
	Context      map[string]any
}

// RunResult represents the result of a run
type RunResult struct {
	RunID   string
	Success bool
	Output  string
	Error   string
	Steps   int
}

// Adapter is the interface for the runtime adapter
type Adapter interface {
	Execute(ctx context.Context, req RunRequest) (RunResult, error)
}

// SimpleAdapter is a basic adapter that doesn't require ADK Go
type SimpleAdapter struct {
	tools    *tools.Catalog
	ws       *workspace.Manager
	approval *approval.Service
	bus      *events.Bus
}

func NewSimpleAdapter(t *tools.Catalog, ws *workspace.Manager, svc *approval.Service, bus *events.Bus) *SimpleAdapter {
	return &SimpleAdapter{tools: t, ws: ws, approval: svc, bus: bus}
}

func (a *SimpleAdapter) Execute(ctx context.Context, req RunRequest) (RunResult, error) {
	a.bus.Publish(events.Event{
		EventID:   newEventID(),
		Type:      events.EventToolStarted,
		RunID:     req.RunID,
		Timestamp: time.Now(),
		Payload:   map[string]any{"agent": req.AgentName, "goal": req.Goal},
	})

	steps := 0
	var output string

	// Execute each tool listed in req.Tools with a basic input derived from goal
	for _, toolName := range req.Tools {
		t, ok := a.tools.Get(toolName)
		if !ok {
			continue
		}
		input := tools.ToolInput{"path": ".", "pattern": "*", "query": req.Goal}
		a.bus.Publish(events.Event{
			EventID:   newEventID(),
			Type:      events.EventToolStarted,
			RunID:     req.RunID,
			Timestamp: time.Now(),
			Payload:   map[string]any{"tool": toolName},
		})
		result, err := t.Execute(ctx, input)
		steps++
		if err != nil {
			a.bus.Publish(events.Event{
				EventID:   newEventID(),
				Type:      events.EventToolFailed,
				RunID:     req.RunID,
				Timestamp: time.Now(),
				Payload:   map[string]any{"tool": toolName, "error": err.Error()},
			})
			continue
		}
		a.bus.Publish(events.Event{
			EventID:   newEventID(),
			Type:      events.EventToolCompleted,
			RunID:     req.RunID,
			Timestamp: time.Now(),
			Payload:   map[string]any{"tool": toolName, "success": result.Success},
		})
		if result.Success {
			for k, v := range result.Data {
				output += fmt.Sprintf("%s=%v ", k, v)
			}
		}
	}

	return RunResult{
		RunID:   req.RunID,
		Success: true,
		Output:  output,
		Steps:   steps,
	}, nil
}
