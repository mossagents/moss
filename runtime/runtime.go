package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/capability"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/logging"
	"github.com/mossagents/moss/mcp"
	"github.com/mossagents/moss/skill"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	capabilitiesStateKey kernel.ServiceKey = "capabilities.state"
	agentsStateKey       kernel.ServiceKey = "agents.state"
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
		trust:            appconfig.TrustRestricted,
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
	SetExecutionPolicy(k, ResolveExecutionPolicyForKernel(k, cfg.trust, "confirm"))
	return newRuntimeLifecycleManager().Run(ctx, k, workspaceDir, cfg)
}

func setupBuiltinTools(ctx context.Context, k *kernel.Kernel, _ config) error {
	return CapabilityManager(k).Register(ctx, &builtinToolsProvider{}, CapabilityDeps(k))
}

func setupMCPServers(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg config) error {
	logger := logging.GetLogger()
	globalCfg, err := appconfig.LoadGlobalConfig()
	if err != nil {
		cfg.capabilityReport.Report(ctx, "mcp:global-config", true, "failed", err)
		return fmt.Errorf("load global config: %w", err)
	}
	deps := CapabilityDeps(k)
	allSkills := append([]appconfig.SkillConfig(nil), globalCfg.Skills...)
	projectCfg, err := appconfig.LoadProjectConfigForTrust(workspaceDir, cfg.trust)
	if err != nil {
		cfg.capabilityReport.Report(ctx, "mcp:project-config", true, "failed", err)
		return fmt.Errorf("load project config: %w", err)
	}
	if !appconfig.ProjectAssetsAllowed(cfg.trust) {
		return registerMCPServers(ctx, cfg, deps, allSkills)
	}
	approved := make([]appconfig.SkillConfig, 0, len(projectCfg.Skills))
	for _, sc := range projectCfg.Skills {
		if !sc.IsEnabled() || !sc.IsMCP() {
			continue
		}
		allow, err := approveProjectMCPServer(ctx, deps.UserIO, workspaceDir, sc)
		if err != nil {
			cfg.capabilityReport.Report(ctx, "mcp:"+sc.Name, sc.IsRequired(), "failed", err)
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
			cfg.capabilityReport.Report(ctx, "mcp:"+sc.Name, sc.IsRequired(), "skipped", err)
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

func registerMCPServers(ctx context.Context, cfg config, deps capability.Deps, skills []appconfig.SkillConfig) error {
	logger := logging.GetLogger()
	ordered, err := orderSkillConfigs(skills)
	if err != nil {
		return err
	}
	for _, sc := range ordered {
		if !sc.IsEnabled() || !sc.IsMCP() {
			continue
		}
		if err := CapabilityManager(deps.Kernel).Register(ctx, mcp.NewMCPServer(sc), deps); err != nil {
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

func approveProjectMCPServer(ctx context.Context, userIO io.UserIO, workspaceDir string, sc appconfig.SkillConfig) (bool, error) {
	if userIO == nil {
		userIO = &io.NoOpIO{}
	}
	target := strings.TrimSpace(sc.URL)
	if target == "" {
		target = strings.TrimSpace(sc.Command)
	}
	resp, err := userIO.Ask(ctx, io.InputRequest{
		Type:         io.InputConfirm,
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

func setupPromptSkills(ctx context.Context, k *kernel.Kernel, workspaceDir string, cfg config) error {
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
	deps := CapabilityDeps(k)
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
		if err := CapabilityManager(k).Register(ctx, ps, deps); err != nil {
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

type capabilitiesState struct {
	manager                *capability.Manager
	manifests              []skill.Manifest
	progressive            bool
	progressiveToolsLoaded bool
}

func ensureCapabilitiesState(k *kernel.Kernel) *capabilitiesState {
	actual, loaded := k.Services().LoadOrStore(capabilitiesStateKey, &capabilitiesState{
		manager: capability.NewManager(),
	})
	st := actual.(*capabilitiesState)
	if loaded {
		return st
	}
	k.Stages().OnShutdown(300, func(ctx context.Context, _ *kernel.Kernel) error {
		if st.manager == nil {
			return nil
		}
		return st.manager.ShutdownAll(ctx)
	})
	k.Prompts().Add(200, func(_ *kernel.Kernel) string {
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

func CapabilityManager(k *kernel.Kernel) *capability.Manager {
	return ensureCapabilitiesState(k).manager
}

func WithCapabilityManager(m *capability.Manager) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureCapabilitiesState(k).manager = m
	}
}

func SkillManifests(k *kernel.Kernel) []skill.Manifest {
	st := ensureCapabilitiesState(k)
	return append([]skill.Manifest(nil), st.manifests...)
}

func SetSkillManifests(k *kernel.Kernel, manifests []skill.Manifest) {
	st := ensureCapabilitiesState(k)
	st.manifests = append([]skill.Manifest(nil), manifests...)
}

func EnableProgressiveSkills(k *kernel.Kernel) {
	ensureCapabilitiesState(k).progressive = true
}

func RegisterProgressiveSkillTools(k *kernel.Kernel) error {
	st := ensureCapabilitiesState(k)
	if st.progressiveToolsLoaded {
		return nil
	}
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
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
	})); err != nil {
		return err
	}
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
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
		if err := activateManifestRecursive(ctx, st.manager, st.manifests, name, CapabilityDeps(k), nil); err != nil {
			return nil, fmt.Errorf("activate skill %q: %w", name, err)
		}
		return json.Marshal(map[string]string{"status": "loaded", "name": name})
	})); err != nil {
		return err
	}
	st.progressiveToolsLoaded = true
	return nil
}

func CapabilityDeps(k *kernel.Kernel) capability.Deps {
	return capability.Deps{
		Kernel:       k,
		ToolRegistry: k.ToolRegistry(),
		Hooks:        k.Hooks(),
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

func activateManifestRecursive(ctx context.Context, manager *capability.Manager, manifests []skill.Manifest, target string, deps capability.Deps, stack map[string]bool) error {
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

func AgentTaskTracker(k *kernel.Kernel) *agent.TaskTracker {
	return ensureAgentsState(k).tasks
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

// builtinToolsProvider adapts runtime-owned builtin tools into the shared capability lifecycle.
// It exists so runtime can manage builtin tools, prompt skills, and MCP-backed providers uniformly,
// while keeping their ownership and behavior distinct:
//   - builtin tools: first-party tools implemented in package runtime
//   - prompt skills: SKILL.md prompt additions implemented in package skill
//   - MCP providers: external tool servers bridged by package mcp
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
	s.toolNames = RegisteredBuiltinToolNames(deps.Sandbox, deps.Workspace, deps.Executor)
	return RegisterBuiltinToolsForKernel(deps.Kernel, deps.ToolRegistry, deps.Sandbox, deps.UserIO, deps.Workspace, deps.Executor)
}

func (s *builtinToolsProvider) Shutdown(_ context.Context) error { return nil }
