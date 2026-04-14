package assembly

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mossagents/moss/capability"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/extensions/agent"
	"github.com/mossagents/moss/extensions/mcp"
	"github.com/mossagents/moss/extensions/skill"
	runtimecapa "github.com/mossagents/moss/internal/runtime/capability"
	"github.com/mossagents/moss/internal/runtime/policy"
	"github.com/mossagents/moss/kernel"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/logging"
	appruntime "github.com/mossagents/moss/runtime"
)

// Config controls runtime capability assembly through the non-public setup
// pipeline used by harness.
type Config struct {
	BuiltinTools       bool
	MCPServers         bool
	Skills             bool
	ProgressiveSkills  bool
	Agents             bool
	Trust              string
	CapabilityReporter capability.CapabilityReporter
}

// DefaultConfig returns the canonical runtime capability defaults.
func DefaultConfig() Config {
	return Config{
		BuiltinTools:      true,
		MCPServers:        true,
		Skills:            true,
		ProgressiveSkills: false,
		Agents:            true,
		Trust:             appconfig.TrustRestricted,
	}
}

// ResolveConfig validates and normalizes a runtime assembly config.
func ResolveConfig(cfg Config) (Config, error) {
	cfg.Trust = appconfig.NormalizeTrustLevel(strings.TrimSpace(cfg.Trust))
	if cfg.Trust == "" {
		cfg.Trust = appconfig.TrustRestricted
	}
	if !cfg.Skills && cfg.ProgressiveSkills {
		return cfg, fmt.Errorf("invalid runtime options: progressive skills require skills to be enabled")
	}
	return cfg, nil
}

// Install assembles runtime capabilities onto the kernel using the resolved
// non-public setup pipeline.
func Install(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg Config) error {
	cfg, err := ResolveConfig(cfg)
	if err != nil {
		return err
	}
	cfg.CapabilityReporter = capability.NewCapabilityReporter(capability.CapabilityStatusPath(), cfg.CapabilityReporter)
	if err := policy.ApplyResolved(k, workspaceDir, cfg.Trust, "confirm"); err != nil {
		return err
	}
	return newLifecycleManager().Run(ctx, k, workspaceDir, cfg)
}

type capabilityInstaller interface {
	Name() string
	Critical() bool
	Enabled(Config) bool
	Register(context.Context, *kernel.Kernel, string, Config) error
	Validate(context.Context, *kernel.Kernel, string, Config) error
	Activate(context.Context, *kernel.Kernel, string, Config) error
}

type lifecycleManager struct {
	capabilities []capabilityInstaller
}

func newLifecycleManager() lifecycleManager {
	return lifecycleManager{
		capabilities: []capabilityInstaller{
			builtinToolsCapability{},
			mcpCapability{},
			promptSkillsCapability{},
			agentsCapability{},
		},
	}
}

func (m lifecycleManager) Run(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg Config) error {
	for _, cap := range m.capabilities {
		if !cap.Enabled(cfg) {
			report(cfg.CapabilityReporter, ctx, cap.Name(), cap.Critical(), "disabled", nil)
			continue
		}
		if err := cap.Register(ctx, k, workspaceDir, cfg); err != nil {
			report(cfg.CapabilityReporter, ctx, cap.Name(), cap.Critical(), "failed", err)
			if cap.Critical() {
				return err
			}
			continue
		}
		report(cfg.CapabilityReporter, ctx, cap.Name(), cap.Critical(), "ready", nil)
	}

	for _, cap := range m.capabilities {
		if !cap.Enabled(cfg) {
			continue
		}
		if err := cap.Validate(ctx, k, workspaceDir, cfg); err != nil {
			report(cfg.CapabilityReporter, ctx, "runtime-validate", true, "failed", err)
			return err
		}
	}
	report(cfg.CapabilityReporter, ctx, "runtime-validate", true, "ready", nil)

	for _, cap := range m.capabilities {
		if !cap.Enabled(cfg) {
			continue
		}
		if err := cap.Activate(ctx, k, workspaceDir, cfg); err != nil {
			report(cfg.CapabilityReporter, ctx, cap.Name(), cap.Critical(), "failed", err)
			if cap.Critical() {
				return err
			}
		}
	}
	report(cfg.CapabilityReporter, ctx, "runtime-activate", true, "ready", nil)
	return nil
}

type builtinToolsCapability struct{}

func (builtinToolsCapability) Name() string            { return "builtin-tools" }
func (builtinToolsCapability) Critical() bool          { return true }
func (builtinToolsCapability) Enabled(cfg Config) bool { return cfg.BuiltinTools }
func (builtinToolsCapability) Register(ctx context.Context, k *kernel.Kernel, _ string, _ Config) error {
	return runtimecapa.Manager(k).Register(ctx, &builtinToolsProvider{}, runtimecapa.CapabilityDeps(k))
}
func (builtinToolsCapability) Validate(_ context.Context, k *kernel.Kernel, _ string, _ Config) error {
	manager, ok := runtimecapa.LookupManager(k)
	if !ok || manager == nil {
		return fmt.Errorf("runtime validation failed: capability manager missing")
	}
	if _, ok := manager.Get("builtin-tools"); !ok {
		return fmt.Errorf("runtime validation failed: builtin-tools provider missing")
	}
	return nil
}
func (builtinToolsCapability) Activate(context.Context, *kernel.Kernel, string, Config) error {
	return nil
}

type mcpCapability struct{}

func (mcpCapability) Name() string            { return "mcp" }
func (mcpCapability) Critical() bool          { return false }
func (mcpCapability) Enabled(cfg Config) bool { return cfg.MCPServers }
func (mcpCapability) Register(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg Config) error {
	logger := logging.GetLogger()
	globalCfg, err := appconfig.LoadGlobalConfig()
	if err != nil {
		report(cfg.CapabilityReporter, ctx, "mcp:global-config", true, "failed", err)
		return fmt.Errorf("load global config: %w", err)
	}
	deps := runtimecapa.CapabilityDeps(k)
	allSkills := append([]appconfig.SkillConfig(nil), globalCfg.Skills...)
	projectCfg, err := appconfig.LoadProjectConfigForTrust(workspaceDir, cfg.Trust)
	if err != nil {
		report(cfg.CapabilityReporter, ctx, "mcp:project-config", true, "failed", err)
		return fmt.Errorf("load project config: %w", err)
	}
	if !appconfig.ProjectAssetsAllowed(cfg.Trust) {
		return registerMCPServers(ctx, cfg, deps, allSkills)
	}
	approved := make([]appconfig.SkillConfig, 0, len(projectCfg.Skills))
	for _, sc := range projectCfg.Skills {
		if !sc.IsEnabled() || !sc.IsMCP() {
			continue
		}
		allow, err := approveProjectMCPServer(ctx, deps.UserIO, workspaceDir, sc)
		if err != nil {
			report(cfg.CapabilityReporter, ctx, "mcp:"+sc.Name, sc.IsRequired(), "failed", err)
			if sc.IsRequired() {
				return fmt.Errorf("required MCP server %q approval failed: %w", sc.Name, err)
			}
			logger.WarnContext(ctx, "failed to approve project MCP server",
				slog.String("server", sc.Name),
				slog.Any("error", err),
			)
			continue
		}
		if !allow {
			err := fmt.Errorf("project MCP server %q was not approved", sc.Name)
			report(cfg.CapabilityReporter, ctx, "mcp:"+sc.Name, sc.IsRequired(), "skipped", err)
			if sc.IsRequired() {
				return err
			}
			logger.WarnContext(ctx, "skipping unapproved project MCP server", slog.String("server", sc.Name))
			continue
		}
		approved = append(approved, sc)
	}
	merged := appconfig.MergeConfigs(&appconfig.Config{Skills: allSkills}, &appconfig.Config{Skills: approved})
	return registerMCPServers(ctx, cfg, deps, merged.Skills)
}
func (mcpCapability) Validate(context.Context, *kernel.Kernel, string, Config) error { return nil }
func (mcpCapability) Activate(context.Context, *kernel.Kernel, string, Config) error { return nil }

type promptSkillsCapability struct{}

func (promptSkillsCapability) Name() string            { return "skills" }
func (promptSkillsCapability) Critical() bool          { return true }
func (promptSkillsCapability) Enabled(cfg Config) bool { return cfg.Skills }
func (promptSkillsCapability) Register(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg Config) error {
	logger := logging.GetLogger()
	manifests := skill.DiscoverSkillManifestsForTrust(workspaceDir, cfg.Trust)
	ordered, err := orderSkillManifests(manifests)
	if err != nil {
		return err
	}
	if cfg.ProgressiveSkills {
		runtimecapa.SetSkillManifests(k, ordered)
		runtimecapa.EnableProgressiveSkills(k)
		for _, mf := range ordered {
			report(cfg.CapabilityReporter, ctx, "skill-manifest:"+mf.Name, false, "discoverable", nil)
		}
		return runtimecapa.RegisterProgressiveSkillTools(k)
	}
	deps := runtimecapa.CapabilityDeps(k)
	for _, mf := range ordered {
		ps, err := skill.ParseSkillMD(mf.Source)
		if err != nil {
			report(cfg.CapabilityReporter, ctx, "skill-manifest:"+mf.Name, false, "degraded", err)
			logger.WarnContext(ctx, "failed to parse skill",
				slog.String("source", mf.Source),
				slog.Any("error", err),
			)
			continue
		}
		if err := runtimecapa.Manager(k).Register(ctx, ps, deps); err != nil {
			report(cfg.CapabilityReporter, ctx, "skill:"+ps.Metadata().Name, false, "degraded", err)
			logger.WarnContext(ctx, "failed to load skill",
				slog.String("skill", ps.Metadata().Name),
				slog.Any("error", err),
			)
			continue
		}
		report(cfg.CapabilityReporter, ctx, "skill:"+ps.Metadata().Name, false, "ready", nil)
	}
	return nil
}
func (promptSkillsCapability) Validate(context.Context, *kernel.Kernel, string, Config) error {
	return nil
}
func (promptSkillsCapability) Activate(context.Context, *kernel.Kernel, string, Config) error {
	return nil
}

type agentsCapability struct{}

func (agentsCapability) Name() string            { return "agents" }
func (agentsCapability) Critical() bool          { return false }
func (agentsCapability) Enabled(cfg Config) bool { return cfg.Agents }
func (agentsCapability) Register(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg Config) error {
	logger := logging.GetLogger()
	if err := agent.EnsureKernelDelegation(k); err != nil {
		report(cfg.CapabilityReporter, ctx, "agents:delegation", false, "failed", err)
		logger.WarnContext(ctx, "failed to prepare agent delegation substrate", slog.Any("error", err))
		return nil
	}
	registry := agent.KernelRegistry(k)
	for _, dir := range collectAgentDirs(workspaceDir, cfg) {
		before := registry.List()
		if err := registry.LoadDir(dir); err != nil {
			report(cfg.CapabilityReporter, ctx, "agents:"+dir, false, "degraded", err)
			logger.WarnContext(ctx, "failed to load agents",
				slog.String("dir", dir),
				slog.Any("error", err),
			)
			continue
		}
		report(cfg.CapabilityReporter, ctx, "agents:"+dir, false, "ready", nil)
		known := make(map[string]struct{}, len(before))
		for _, item := range before {
			known[item.Name] = struct{}{}
		}
		for _, item := range registry.List() {
			if _, ok := known[item.Name]; ok {
				continue
			}
			report(cfg.CapabilityReporter, ctx, "subagent:"+item.Name, false, "ready", nil)
		}
	}
	return nil
}
func (agentsCapability) Validate(context.Context, *kernel.Kernel, string, Config) error { return nil }
func (agentsCapability) Activate(context.Context, *kernel.Kernel, string, Config) error { return nil }

func report(reporter capability.CapabilityReporter, ctx context.Context, capability string, critical bool, state string, err error) {
	if reporter != nil {
		reporter.Report(ctx, capability, critical, state, err)
	}
}

func registerMCPServers(ctx context.Context, cfg Config, deps capability.Deps, skills []appconfig.SkillConfig) error {
	logger := logging.GetLogger()
	ordered, err := orderSkillConfigs(skills)
	if err != nil {
		return err
	}
	for _, sc := range ordered {
		if !sc.IsEnabled() || !sc.IsMCP() {
			continue
		}
		if err := runtimecapa.Manager(deps.Kernel).Register(ctx, mcp.NewMCPServer(sc), deps); err != nil {
			report(cfg.CapabilityReporter, ctx, "mcp:"+sc.Name, sc.IsRequired(), "failed", err)
			if sc.IsRequired() {
				return fmt.Errorf("required MCP server %q failed: %w", sc.Name, err)
			}
			logger.WarnContext(ctx, "failed to load MCP server",
				slog.String("server", sc.Name),
				slog.Any("error", err),
			)
			continue
		}
		report(cfg.CapabilityReporter, ctx, "mcp:"+sc.Name, sc.IsRequired(), "ready", nil)
	}
	return nil
}

func approveProjectMCPServer(ctx context.Context, userIO kernio.UserIO, workspaceDir string, sc appconfig.SkillConfig) (bool, error) {
	if userIO == nil {
		userIO = &kernio.NoOpIO{}
	}
	target := strings.TrimSpace(sc.URL)
	if target == "" {
		target = strings.TrimSpace(sc.Command)
	}
	resp, err := userIO.Ask(ctx, kernio.InputRequest{
		Type:         kernio.InputConfirm,
		Prompt:       fmt.Sprintf("Start project MCP server %q from %s?", sc.Name, appconfig.DefaultProjectConfigPath(workspaceDir)),
		ConfirmLabel: "Start MCP server",
		Meta: map[string]any{
			"workspace": workspaceDir,
			"target":    target,
			"transport": sc.Transport,
			"source":    appconfig.DefaultProjectConfigPath(workspaceDir),
		},
	})
	if err != nil {
		return false, err
	}
	return resp.Approved, nil
}

func orderSkillConfigs(items []appconfig.SkillConfig) ([]appconfig.SkillConfig, error) {
	indexed := make(map[string]appconfig.SkillConfig, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		indexed[item.Name] = item
	}
	orderedNames, err := topoOrderNames(indexed, func(item appconfig.SkillConfig) []string { return item.DependsOn })
	if err != nil {
		return nil, err
	}
	out := make([]appconfig.SkillConfig, 0, len(orderedNames))
	for _, name := range orderedNames {
		out = append(out, indexed[name])
	}
	return out, nil
}

func orderSkillManifests(items []skill.Manifest) ([]skill.Manifest, error) {
	indexed := make(map[string]skill.Manifest, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		indexed[item.Name] = item
	}
	orderedNames, err := topoOrderNames(indexed, func(item skill.Manifest) []string { return item.DependsOn })
	if err != nil {
		return nil, err
	}
	out := make([]skill.Manifest, 0, len(orderedNames))
	for _, name := range orderedNames {
		out = append(out, indexed[name])
	}
	return out, nil
}

func topoOrderNames[T any](items map[string]T, deps func(T) []string) ([]string, error) {
	ordered := make([]string, 0, len(items))
	visiting := make(map[string]bool, len(items))
	visited := make(map[string]bool, len(items))
	var visit func(string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("dependency cycle detected at %q", name)
		}
		item, ok := items[name]
		if !ok {
			return nil
		}
		visiting[name] = true
		for _, dep := range deps(item) {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if _, ok := items[dep]; !ok {
				continue
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[name] = false
		visited[name] = true
		ordered = append(ordered, name)
		return nil
	}
	for name := range items {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

type builtinToolsProvider struct {
	toolNames []string
}

func (s *builtinToolsProvider) Metadata() capability.Metadata {
	return capability.Metadata{
		Name:        "builtin-tools",
		Version:     "0.3.0",
		Description: "Runtime-owned builtin tools for filesystem, command execution, HTTP requests, and user interaction",
		Tools:       s.toolNames,
		Prompts: []string{
			"You have access to built-in runtime tools: read_file, write_file, edit_file, glob, ls, grep, run_command, http_request, ask_user.",
		},
	}
}

func (s *builtinToolsProvider) Init(ctx context.Context, deps capability.Deps) error {
	s.toolNames = appruntime.RegisteredBuiltinToolNamesForKernel(deps.Kernel)
	return appruntime.RegisterBuiltinToolsForKernel(deps.Kernel, deps.ToolRegistry)
}

func (s *builtinToolsProvider) Shutdown(_ context.Context) error { return nil }

var _ capability.Provider = (*builtinToolsProvider)(nil)

func collectAgentDirs(workspaceDir string, cfg Config) []string {
	var dirs []string
	if appconfig.ProjectAssetsAllowed(cfg.Trust) {
		dirs = append(dirs, filepath.Join(workspaceDir, ".agents", "agents"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".moss", "agents"))
	}
	return dirs
}
