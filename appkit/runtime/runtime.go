package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mossagents/moss/agent"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/logging"
	"github.com/mossagents/moss/mcp"
	"github.com/mossagents/moss/skill"
)

const (
	skillsStateKey kernel.ExtensionStateKey = "skills.state"
	agentsStateKey kernel.ExtensionStateKey = "agents.state"
)

type config struct {
	builtin          bool
	mcpServers       bool
	skills           bool
	progressive      bool
	agents           bool
	trust            string
	sessionStore     session.SessionStore
	sessionStoreSet  bool
	planning         bool
	capabilityReport CapabilityReporter
}

type Option func(*config)

func defaultConfig() config {
	return config{
		builtin:          true,
		mcpServers:       true,
		skills:           true,
		progressive:      false,
		agents:           true,
		trust:            appconfig.TrustTrusted,
		planning:         true,
		capabilityReport: noopCapabilityReporter{},
	}
}

func WithBuiltinTools(enabled bool) Option { return func(c *config) { c.builtin = enabled } }
func WithMCPServers(enabled bool) Option   { return func(c *config) { c.mcpServers = enabled } }
func WithSkills(enabled bool) Option       { return func(c *config) { c.skills = enabled } }
func WithProgressiveSkills(enabled bool) Option {
	return func(c *config) { c.progressive = enabled }
}
func WithAgents(enabled bool) Option   { return func(c *config) { c.agents = enabled } }
func WithPlanning(enabled bool) Option { return func(c *config) { c.planning = enabled } }
func WithWorkspaceTrust(trust string) Option {
	return func(c *config) { c.trust = trust }
}
func WithSessionStore(store session.SessionStore) Option {
	return func(c *config) {
		c.sessionStore = store
		c.sessionStoreSet = true
	}
}

func WithCapabilityReporter(r CapabilityReporter) Option {
	return func(c *config) {
		if r == nil {
			c.capabilityReport = noopCapabilityReporter{}
			return
		}
		c.capabilityReport = r
	}
}

type CapabilityReporter interface {
	Report(ctx context.Context, capability string, critical bool, state string, err error)
}

type noopCapabilityReporter struct{}

func (noopCapabilityReporter) Report(context.Context, string, bool, string, error) {}

func resolve(opts ...Option) (config, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if !cfg.skills && cfg.progressive {
		return cfg, fmt.Errorf("invalid runtime options: progressive skills require skills to be enabled")
	}
	if cfg.sessionStoreSet && cfg.sessionStore == nil {
		return cfg, fmt.Errorf("invalid runtime options: session store cannot be nil")
	}
	return cfg, nil
}

func Setup(ctx context.Context, k *kernel.Kernel, workspaceDir string, opts ...Option) error {
	cfg, err := resolve(opts...)
	if err != nil {
		return err
	}
	cfg.capabilityReport = NewCapabilityReporter(CapabilityStatusPath(), cfg.capabilityReport)
	return newRuntimeLifecycleManager().Run(ctx, k, workspaceDir, cfg)
}

func setupBuiltinTools(ctx context.Context, k *kernel.Kernel, _ config) error {
	return SkillsManager(k).Register(ctx, &builtinToolsProvider{}, Deps(k))
}

func setupMCPServers(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg config) error {
	logger := logging.GetLogger()
	globalCfg, _ := appconfig.LoadGlobalConfig()
	merged := appconfig.MergeConfigs(globalCfg)
	if appconfig.ProjectAssetsAllowed(cfg.trust) {
		projectCfg, _ := appconfig.LoadConfig(appconfig.DefaultProjectConfigPath(workspaceDir))
		merged = appconfig.MergeConfigs(globalCfg, projectCfg)
	}
	deps := Deps(k)
	ordered, err := orderSkillConfigs(merged.Skills)
	if err != nil {
		return err
	}
	for _, sc := range ordered {
		if !sc.IsEnabled() || !sc.IsMCP() {
			continue
		}
		if err := SkillsManager(k).Register(ctx, mcp.NewMCPServer(sc), deps); err != nil {
			cfg.capabilityReport.Report(ctx, "mcp:"+sc.Name, sc.IsRequired(), "failed", err)
			if sc.IsRequired() {
				return fmt.Errorf("required MCP server %q failed: %w", sc.Name, err)
			}
			logger.WarnContext(ctx, "failed to load MCP server",
				slog.String("server", sc.Name),
				slog.Any("error", err),
			)
			continue
		}
		cfg.capabilityReport.Report(ctx, "mcp:"+sc.Name, sc.IsRequired(), "ready", nil)
	}
	return nil
}

func setupSkills(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg config) error {
	logger := logging.GetLogger()
	manifests := skill.DiscoverSkillManifestsForTrust(workspaceDir, cfg.trust)
	ordered, err := orderSkillManifests(manifests)
	if err != nil {
		return err
	}
	if cfg.progressive {
		SetSkillManifests(k, ordered)
		EnableProgressiveSkills(k)
		for _, mf := range ordered {
			cfg.capabilityReport.Report(ctx, "skill-manifest:"+mf.Name, false, "discoverable", nil)
		}
		return RegisterProgressiveSkillTools(k)
	}
	deps := Deps(k)
	for _, mf := range ordered {
		ps, err := skill.ParseSkillMD(mf.Source)
		if err != nil {
			cfg.capabilityReport.Report(ctx, "skill-manifest:"+mf.Name, false, "degraded", err)
			logger.WarnContext(ctx, "failed to parse skill",
				slog.String("source", mf.Source),
				slog.Any("error", err),
			)
			continue
		}
		if err := SkillsManager(k).Register(ctx, ps, deps); err != nil {
			cfg.capabilityReport.Report(ctx, "skill:"+ps.Metadata().Name, false, "degraded", err)
			logger.WarnContext(ctx, "failed to load skill",
				slog.String("skill", ps.Metadata().Name),
				slog.Any("error", err),
			)
			continue
		}
		cfg.capabilityReport.Report(ctx, "skill:"+ps.Metadata().Name, false, "ready", nil)
	}
	return nil
}

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

type skillsState struct {
	manager                *skill.Manager
	manifests              []skill.Manifest
	progressive            bool
	progressiveToolsLoaded bool
}

func ensureSkillsState(k *kernel.Kernel) *skillsState {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(skillsStateKey, &skillsState{
		manager: skill.NewManager(),
	})
	st := actual.(*skillsState)
	if loaded {
		return st
	}
	bridge.OnShutdown(300, func(ctx context.Context, _ *kernel.Kernel) error {
		if st.manager == nil {
			return nil
		}
		return st.manager.ShutdownAll(ctx)
	})
	bridge.OnSystemPrompt(200, func(_ *kernel.Kernel) string {
		if st.manager == nil {
			return ""
		}
		additions := st.manager.SystemPromptAdditions()
		if !st.progressive || len(st.manifests) == 0 {
			return additions
		}
		names := make([]string, 0, len(st.manifests))
		for _, mf := range st.manifests {
			if _, loaded := st.manager.Get(mf.Name); loaded {
				continue
			}
			names = append(names, mf.Name)
		}
		if len(names) == 0 {
			return additions
		}
		hint := "Discovered skills are available on demand. Use `list_skills` to browse and `activate_skill` to load one when needed: " + strings.Join(names, ", ")
		if additions == "" {
			return hint
		}
		return additions + "\n\n" + hint
	})
	return st
}

func SkillsManager(k *kernel.Kernel) *skill.Manager {
	return ensureSkillsState(k).manager
}

func WithSkillManager(m *skill.Manager) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureSkillsState(k).manager = m
	}
}

func SkillManifests(k *kernel.Kernel) []skill.Manifest {
	st := ensureSkillsState(k)
	return append([]skill.Manifest(nil), st.manifests...)
}

func SetSkillManifests(k *kernel.Kernel, manifests []skill.Manifest) {
	st := ensureSkillsState(k)
	st.manifests = append([]skill.Manifest(nil), manifests...)
}

func EnableProgressiveSkills(k *kernel.Kernel) {
	ensureSkillsState(k).progressive = true
}

func RegisterProgressiveSkillTools(k *kernel.Kernel) error {
	st := ensureSkillsState(k)
	if st.progressiveToolsLoaded {
		return nil
	}
	if err := k.ToolRegistry().Register(tool.ToolSpec{
		Name:        "list_skills",
		Description: "List discovered SKILL.md skills and whether each one has been activated.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Risk:        tool.RiskLow,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		manifests := append([]skill.Manifest(nil), st.manifests...)
		resp := make([]map[string]any, 0, len(manifests))
		for _, mf := range manifests {
			_, loaded := st.manager.Get(mf.Name)
			resp = append(resp, map[string]any{
				"name":         mf.Name,
				"description":  mf.Description,
				"depends_on":   append([]string(nil), mf.DependsOn...),
				"required_env": append([]string(nil), mf.RequiredEnv...),
				"source":       mf.Source,
				"loaded":       loaded,
			})
		}
		return json.Marshal(resp)
	}); err != nil {
		return err
	}
	if err := k.ToolRegistry().Register(tool.ToolSpec{
		Name:        "activate_skill",
		Description: "Load one discovered SKILL.md into the active prompt context by skill name.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Skill name to activate"}},"required":["name"]}`),
		Risk:        tool.RiskLow,
	}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		if _, ok := st.manager.Get(name); ok {
			return json.Marshal(map[string]string{"status": "already_loaded", "name": name})
		}
		var found *skill.Manifest
		for i := range st.manifests {
			if st.manifests[i].Name == name {
				found = &st.manifests[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("skill %q not found in discovered manifests", name)
		}
		if err := activateManifestRecursive(ctx, st.manager, st.manifests, name, Deps(k), nil); err != nil {
			return nil, fmt.Errorf("activate skill %q: %w", name, err)
		}
		return json.Marshal(map[string]string{"status": "loaded", "name": name})
	}); err != nil {
		return err
	}
	st.progressiveToolsLoaded = true
	return nil
}

func Deps(k *kernel.Kernel) skill.Deps {
	return skill.Deps{
		Kernel:       k,
		ToolRegistry: k.ToolRegistry(),
		Middleware:   k.Middleware(),
		Sandbox:      k.Sandbox(),
		UserIO:       k.UserIO(),
		Workspace:    k.Workspace(),
		Executor:     k.Executor(),
		TaskRuntime:  k.TaskRuntime(),
		Mailbox:      k.Mailbox(),
		SessionStore: k.SessionStore(),
	}
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

func activateManifestRecursive(ctx context.Context, manager *skill.Manager, manifests []skill.Manifest, target string, deps skill.Deps, stack map[string]bool) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("skill name is required")
	}
	if _, ok := manager.Get(target); ok {
		return nil
	}
	if stack == nil {
		stack = make(map[string]bool)
	}
	if stack[target] {
		return fmt.Errorf("dependency cycle detected at %q", target)
	}
	var found *skill.Manifest
	for i := range manifests {
		if manifests[i].Name == target {
			found = &manifests[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("skill %q not found in discovered manifests", target)
	}
	stack[target] = true
	for _, dep := range found.DependsOn {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if _, ok := manager.Get(dep); ok {
			continue
		}
		if err := activateManifestRecursive(ctx, manager, manifests, dep, deps, stack); err != nil {
			return err
		}
	}
	delete(stack, target)
	ps, err := skill.ParseSkillMD(found.Source)
	if err != nil {
		return fmt.Errorf("load skill %q: %w", target, err)
	}
	return manager.Register(ctx, ps, deps)
}

type agentsState struct {
	registry  *agent.Registry
	tasks     *agent.TaskTracker
	runtime   port.TaskRuntime
	mailbox   port.Mailbox
	isolation port.WorkspaceIsolation
}

func ensureAgentsState(k *kernel.Kernel) *agentsState {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(agentsStateKey, &agentsState{
		registry: agent.NewRegistry(),
	})
	st := actual.(*agentsState)
	if loaded {
		return st
	}
	bridge.OnBoot(100, func(_ context.Context, k *kernel.Kernel) error {
		if st.registry == nil || len(st.registry.List()) == 0 {
			return nil
		}
		if st.runtime == nil {
			st.runtime = k.TaskRuntime()
		}
		if st.runtime == nil {
			st.runtime = port.NewMemoryTaskRuntime()
		}
		if st.mailbox == nil {
			st.mailbox = k.Mailbox()
		}
		if st.mailbox == nil {
			st.mailbox = port.NewMemoryMailbox()
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
			return kerrors.Wrap(kerrors.ErrInternal, "register agent delegation tools", err)
		}
		return nil
	})
	return st
}

func AgentRegistry(k *kernel.Kernel) *agent.Registry {
	return ensureAgentsState(k).registry
}

func AgentTaskTracker(k *kernel.Kernel) *agent.TaskTracker {
	return ensureAgentsState(k).tasks
}

func WithAgentRegistry(r *agent.Registry) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureAgentsState(k).registry = r
	}
}

func WithTaskRuntime(rt port.TaskRuntime) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureAgentsState(k).runtime = rt
	}
}

func WithMailbox(mb port.Mailbox) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureAgentsState(k).mailbox = mb
	}
}

func WithWorkspaceIsolation(isolation port.WorkspaceIsolation) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureAgentsState(k).isolation = isolation
	}
}

// builtinToolsProvider adapts runtime-owned builtin tools into the shared skill lifecycle.
// It exists so runtime can manage builtin tools, prompt skills, and MCP-backed providers uniformly,
// while keeping their ownership and behavior distinct:
//   - builtin tools: first-party tools implemented in appkit/runtime
//   - skills: provider abstraction and prompt injection model in package skill
//   - MCP: external tool servers bridged by package mcp
type builtinToolsProvider struct {
	toolNames []string
}

func (s *builtinToolsProvider) Metadata() skill.Metadata {
	return skill.Metadata{
		Name:        "builtin-tools",
		Version:     "0.3.0",
		Description: "Runtime-owned builtin tools for filesystem, command execution, HTTP requests, and user interaction",
		Tools:       s.toolNames,
		Prompts: []string{
			"You have access to built-in runtime tools: read_file, write_file, edit_file, glob, ls, grep, run_command, http_request, ask_user.",
		},
	}
}

func (s *builtinToolsProvider) Init(ctx context.Context, deps skill.Deps) error {
	s.toolNames = RegisteredBuiltinToolNames(deps.Sandbox, deps.Workspace, deps.Executor)
	return RegisterBuiltinToolsForKernel(deps.Kernel, deps.ToolRegistry, deps.Sandbox, deps.UserIO, deps.Workspace, deps.Executor)
}

func (s *builtinToolsProvider) Shutdown(_ context.Context) error { return nil }
