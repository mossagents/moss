package runtime

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mossagents/moss/agent"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/errors"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/logging"
)

const agentsStateKey kernel.ServiceKey = "agents.state"

func setupAgents(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg config) {
	logger := logging.GetLogger()
	registry := AgentRegistry(k)
	for _, dir := range collectAgentDirs(workspaceDir, cfg) {
		before := registry.List()
		if err := registry.LoadDir(dir); err != nil {
			cfg.capabilityReport.Report(ctx, "agents:"+dir, false, "degraded", err)
			logger.WarnContext(ctx, "failed to load agents",
				slog.String("dir", dir),
				slog.Any("error", err),
			)
			continue
		}
		cfg.capabilityReport.Report(ctx, "agents:"+dir, false, "ready", nil)
		known := make(map[string]struct{}, len(before))
		for _, item := range before {
			known[item.Name] = struct{}{}
		}
		for _, item := range registry.List() {
			if _, ok := known[item.Name]; ok {
				continue
			}
			cfg.capabilityReport.Report(ctx, "subagent:"+item.Name, false, "ready", nil)
		}
	}
}

// collectAgentDirs returns the ordered list of directories to scan for agent
// definitions based on trust level and user home directory.
func collectAgentDirs(workspaceDir string, cfg config) []string {
	var dirs []string
	if appconfig.ProjectAssetsAllowed(cfg.trust) {
		dirs = append(dirs, filepath.Join(workspaceDir, ".agents", "agents"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".moss", "agents"))
	}
	return dirs
}

type agentsState struct {
	registry  *agent.Registry
	tasks     *agent.TaskTracker
	runtime   taskrt.TaskRuntime
	mailbox   taskrt.Mailbox
	isolation workspace.WorkspaceIsolation
}

func ensureAgentsState(k *kernel.Kernel) *agentsState {
	actual, loaded := k.Services().LoadOrStore(agentsStateKey, &agentsState{
		registry: agent.NewRegistry(),
	})
	st := actual.(*agentsState)
	if loaded {
		return st
	}
	k.Stages().OnBoot(100, func(_ context.Context, k *kernel.Kernel) error {
		if st.registry == nil || len(st.registry.List()) == 0 {
			return nil
		}
		if st.runtime == nil {
			st.runtime = k.TaskRuntime()
		}
		if st.runtime == nil {
			st.runtime = taskrt.NewMemoryTaskRuntime()
		}
		if st.mailbox == nil {
			st.mailbox = k.Mailbox()
		}
		if st.mailbox == nil {
			st.mailbox = taskrt.NewMemoryMailbox()
		}
		if st.isolation == nil {
			st.isolation = k.WorkspaceIsolation()
		}
		if st.tasks == nil {
			st.tasks = agent.NewTaskTrackerWithRuntime(st.runtime)
		}
		if err := agent.RegisterToolsWithDeps(k.ToolRegistry(), st.registry, st.tasks, k, agent.RuntimeDeps{
			TaskRuntime: st.runtime,
			Mailbox:     st.mailbox,
			Isolation:   st.isolation,
		}); err != nil {
			return errors.Wrap(errors.ErrInternal, "register agent delegation tools", err)
		}
		return nil
	})
	return st
}

// AgentRegistry returns the low-level runtime-backed subagent catalog.
// Canonical public call sites should prefer harness.SubagentCatalogOf.
func AgentRegistry(k *kernel.Kernel) *agent.Registry {
	return ensureAgentsState(k).registry
}

// WithAgentRegistry injects the runtime-backed subagent catalog.
// Canonical feature composition should prefer harness.SubagentCatalogValue.
func WithAgentRegistry(r *agent.Registry) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureAgentsState(k).registry = r
	}
}

func WithTaskRuntime(rt taskrt.TaskRuntime) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureAgentsState(k).runtime = rt
	}
}

func WithMailbox(mb taskrt.Mailbox) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureAgentsState(k).mailbox = mb
	}
}

func WithWorkspaceIsolation(isolation workspace.WorkspaceIsolation) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureAgentsState(k).isolation = isolation
	}
}
