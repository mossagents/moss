package runtime

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mossagents/moss/agent"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/logging"
)

func setupAgents(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg config) {
	logger := logging.GetLogger()
	if err := agent.EnsureKernelDelegation(k); err != nil {
		cfg.capabilityReport.Report(ctx, "agents:delegation", false, "failed", err)
		logger.WarnContext(ctx, "failed to prepare agent delegation substrate", slog.Any("error", err))
		return
	}
	registry := agent.KernelRegistry(k)
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
