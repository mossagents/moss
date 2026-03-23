package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mossagi/moss/internal/agents/manager"
	"github.com/mossagi/moss/internal/approval"
	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/events"
	"github.com/mossagi/moss/internal/policy"
	"github.com/mossagi/moss/internal/tools"
	"github.com/mossagi/moss/internal/tools/builtins"
	"github.com/mossagi/moss/internal/transcript"
	"github.com/mossagi/moss/internal/workspace"
)

// AgentRunner is the interface for agent execution
type AgentRunner interface {
	Run(ctx context.Context, task *domain.Task, catalog *tools.Catalog) (*domain.TaskResult, error)
}

// RunRequest specifies what to run
type RunRequest struct {
	Goal      string
	Mode      domain.RunMode
	Workspace string
	Trust     workspace.TrustLevel
}

// Service orchestrates runs
type Service struct {
	runManager *RunManager
	agents     map[string]AgentRunner
	catalog    *tools.Catalog
}

// NewService creates a new app service with all dependencies wired up
func NewService(wsRoot string, trust workspace.TrustLevel, reader io.Reader, writer io.Writer) (*Service, error) {
	if reader == nil {
		reader = os.Stdin
	}
	if writer == nil {
		writer = os.Stdout
	}

	ws := workspace.New(wsRoot, trust)
	bus := events.NewBus()

	// Set up transcript store
	tsPath := wsRoot + "/.moss/transcript.jsonl"
	if err := os.MkdirAll(wsRoot+"/.moss", 0755); err != nil {
		return nil, fmt.Errorf("creating .moss dir: %w", err)
	}
	ts, err := transcript.NewTranscriptStore(tsPath)
	if err != nil {
		return nil, fmt.Errorf("creating transcript store: %w", err)
	}

	// Subscribe transcript to bus events
	bus.Subscribe(func(e events.Event) {
		_ = ts.Write(e)
	})

	pol := policy.New()
	appSvc := approval.New(bus, reader, writer)

	// Create tool catalog with builtins
	cat := tools.NewCatalog()
	cat.Register(builtins.NewListFilesTool(ws))
	cat.Register(builtins.NewReadFileTool(ws))
	cat.Register(builtins.NewSearchTextTool(ws))
	cat.Register(builtins.NewRunCommandTool(ws))
	cat.Register(builtins.NewWriteFileTool(ws))
	cat.Register(builtins.NewAskUserTool(reader, writer))

	rm := NewRunManager(ws, pol, appSvc, cat, bus, ts)

	managerAgent := manager.New(cat)

	return &Service{
		runManager: rm,
		catalog:    cat,
		agents:     map[string]AgentRunner{"manager": managerAgent},
	}, nil
}

// Execute performs a complete run with the manager agent
func (s *Service) Execute(ctx context.Context, req RunRequest) (*domain.Run, error) {
	run, err := s.runManager.StartRun(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("starting run: %w", err)
	}

	taskID := fmt.Sprintf("task-%d", time.Now().UnixNano())
	task := &domain.Task{
		TaskID:        taskID,
		RunID:         run.RunID,
		AssignedAgent: "manager",
		Goal:          req.Goal,
		Status:        domain.TaskStatusRunning,
	}

	agentRunner, ok := s.agents["manager"]
	if !ok {
		_ = s.runManager.FailRun(run.RunID, "manager agent not found")
		return run, fmt.Errorf("manager agent not found")
	}

	result, err := agentRunner.Run(ctx, task, s.catalog)
	if err != nil {
		_ = s.runManager.FailRun(run.RunID, err.Error())
		return run, err
	}

	if result.Success {
		_ = s.runManager.CompleteRun(run.RunID, result.Summary)
	} else {
		_ = s.runManager.FailRun(run.RunID, result.Error)
	}

	return run, nil
}
