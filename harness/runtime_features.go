package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mossagents/moss/extensions/knowledge"
	"github.com/mossagents/moss/internal/runtime/assembly"
	runtimectx "github.com/mossagents/moss/internal/runtime/context"
	"github.com/mossagents/moss/internal/runtime/planning"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/runtime"
	"github.com/mossagents/moss/sandbox"
	"github.com/mossagents/moss/scheduler"
)

type runtimeSetupConfig struct {
	builtinTools      bool
	mcpServers        bool
	skills            bool
	progressiveSkills bool
	agents            bool
	reporter          runtime.CapabilityReporter
}

// RuntimeSetupOption configures harness-owned runtime capability assembly.
type RuntimeSetupOption func(*runtimeSetupConfig)

func defaultRuntimeSetupConfig() runtimeSetupConfig {
	return runtimeSetupConfig{
		builtinTools:      true,
		mcpServers:        true,
		skills:            true,
		progressiveSkills: false,
		agents:            true,
	}
}

func resolveRuntimeSetupConfig(opts []RuntimeSetupOption) runtimeSetupConfig {
	cfg := defaultRuntimeSetupConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

func WithBuiltinTools(enabled bool) RuntimeSetupOption {
	return func(cfg *runtimeSetupConfig) { cfg.builtinTools = enabled }
}

func WithMCPServers(enabled bool) RuntimeSetupOption {
	return func(cfg *runtimeSetupConfig) { cfg.mcpServers = enabled }
}

func WithSkills(enabled bool) RuntimeSetupOption {
	return func(cfg *runtimeSetupConfig) { cfg.skills = enabled }
}

func WithProgressiveSkills(enabled bool) RuntimeSetupOption {
	return func(cfg *runtimeSetupConfig) { cfg.progressiveSkills = enabled }
}

func WithAgents(enabled bool) RuntimeSetupOption {
	return func(cfg *runtimeSetupConfig) { cfg.agents = enabled }
}

func WithCapabilityReporter(r runtime.CapabilityReporter) RuntimeSetupOption {
	return func(cfg *runtimeSetupConfig) { cfg.reporter = r }
}

// RuntimeSetup returns a Feature that runs the standard runtime capability
// loading (builtin tools, MCP servers, skills, agents).
func RuntimeSetup(workspaceDir, trust string, opts ...RuntimeSetupOption) Feature {
	return FeatureFunc{
		FeatureName: "runtime-setup",
		MetadataValue: FeatureMetadata{
			Key:   "runtime-setup",
			Phase: FeaturePhaseRuntime,
		},
		InstallFunc: func(ctx context.Context, h *Harness) error {
			cfg := resolveRuntimeSetupConfig(opts)
			return assembly.Install(ctx, h.Kernel(), workspaceDir, assembly.Config{
				BuiltinTools:       cfg.builtinTools,
				MCPServers:         cfg.mcpServers,
				Skills:             cfg.skills,
				ProgressiveSkills:  cfg.progressiveSkills,
				Agents:             cfg.agents,
				Trust:              trust,
				CapabilityReporter: cfg.reporter,
			})
		},
	}
}

// ContextOption configures harness-owned context-management feature behavior.
type ContextOption func(*contextFeatureConfig)

type contextFeatureConfig struct {
	triggerDialog *int
	keepRecent    *int
	triggerTokens *int
	promptBudget  *int
	startupBudget *int
}

func WithTriggerDialogCount(n int) ContextOption {
	return func(cfg *contextFeatureConfig) {
		if n > 0 {
			cfg.triggerDialog = &n
		}
	}
}

func WithKeepRecent(n int) ContextOption {
	return func(cfg *contextFeatureConfig) {
		if n > 0 {
			cfg.keepRecent = &n
		}
	}
}

func WithContextTriggerTokens(n int) ContextOption {
	return func(cfg *contextFeatureConfig) {
		if n > 0 {
			cfg.triggerTokens = &n
		}
	}
}

func WithContextPromptBudget(n int) ContextOption {
	return func(cfg *contextFeatureConfig) {
		if n > 0 {
			cfg.promptBudget = &n
		}
	}
}

func WithContextStartupBudget(n int) ContextOption {
	return func(cfg *contextFeatureConfig) {
		if n >= 0 {
			cfg.startupBudget = &n
		}
	}
}

func runtimeContextOptions(opts []ContextOption) []runtimectx.ContextOption {
	var cfg contextFeatureConfig
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}
	out := make([]runtimectx.ContextOption, 0, 5)
	if cfg.triggerDialog != nil {
		out = append(out, runtimectx.WithTriggerDialogCount(*cfg.triggerDialog))
	}
	if cfg.keepRecent != nil {
		out = append(out, runtimectx.WithKeepRecent(*cfg.keepRecent))
	}
	if cfg.triggerTokens != nil {
		out = append(out, runtimectx.WithContextTriggerTokens(*cfg.triggerTokens))
	}
	if cfg.promptBudget != nil {
		out = append(out, runtimectx.WithContextPromptBudget(*cfg.promptBudget))
	}
	if cfg.startupBudget != nil {
		out = append(out, runtimectx.WithContextStartupBudget(*cfg.startupBudget))
	}
	return out
}

// Planning returns a Feature that installs the write_todos planning tool.
func Planning() Feature {
	return FeatureFunc{
		FeatureName: "planning",
		MetadataValue: FeatureMetadata{
			Key:   "planning",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			h.Kernel().Apply(planning.WithPlanningDefaults())
			return nil
		},
	}
}

// ContextOffload returns a Feature that installs context offload tools.
func ContextOffload(store session.SessionStore) Feature {
	return FeatureFunc{
		FeatureName: "context-offload",
		MetadataValue: FeatureMetadata{
			Key:   "context-offload",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			h.Kernel().Apply(runtimectx.WithOffloadSessionStore(store))
			return runtimectx.RegisterOffloadTools(
				h.Kernel().ToolRegistry(),
				store,
				h.Kernel().SessionManager(),
				runtime.NewContextMemoryService(h.Kernel()),
			)
		},
	}
}

// ContextManagement returns a Feature that installs auto context compression
// and the compact_conversation tool.
func ContextManagement(store session.SessionStore, opts ...ContextOption) Feature {
	return FeatureFunc{
		FeatureName: "context-management",
		MetadataValue: FeatureMetadata{
			Key:   "context-management",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			kopts := []kernel.Option{runtimectx.WithContextSessionStore(store)}
			if rtOpts := runtimeContextOptions(opts); len(rtOpts) > 0 {
				kopts = append(kopts, runtimectx.ConfigureContext(rtOpts...))
			}
			h.Kernel().Apply(kopts...)
			return nil
		},
	}
}

// Scheduling returns a Feature that installs a scheduler and registers
// scheduler tools.
func Scheduling(s *scheduler.Scheduler) Feature {
	return FeatureFunc{
		FeatureName: "scheduling",
		MetadataValue: FeatureMetadata{
			Key:   "scheduling",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			h.Kernel().Apply(runtime.WithScheduler(s))
			return runtime.RegisterSchedulerTools(h.Kernel(), s)
		},
	}
}

// Knowledge returns a Feature that registers knowledge-base tools.
func Knowledge(store knowledge.Store, embedder model.Embedder) Feature {
	return FeatureFunc{
		FeatureName: "knowledge",
		MetadataValue: FeatureMetadata{
			Key:   "knowledge",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			return runtime.RegisterKnowledgeTools(h.Kernel(), store, embedder)
		},
	}
}

// PersistentMemories returns a Feature that installs persistent memory tools.
func PersistentMemories(memoriesDir string) Feature {
	return persistentMemoriesFeature("persistent-memories", memoriesDir, "")
}

// PersistentMemoriesSQLite returns a Feature with an explicit SQLite path for
// the persistent memory store.
func PersistentMemoriesSQLite(memoriesDir, sqlitePath string) Feature {
	return persistentMemoriesFeature("persistent-memories-sqlite", memoriesDir, sqlitePath)
}

func persistentMemoriesFeature(name, memoriesDir, sqlitePath string) Feature {
	return FeatureFunc{
		FeatureName: name,
		MetadataValue: FeatureMetadata{
			Key:   "persistent-memories",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			return installPersistentMemories(h.Kernel(), memoriesDir, sqlitePath)
		},
	}
}

func installPersistentMemories(k *kernel.Kernel, memoriesDir, sqlitePath string) error {
	if strings.TrimSpace(memoriesDir) == "" {
		return fmt.Errorf("memories dir is empty")
	}
	absDir, err := filepath.Abs(memoriesDir)
	if err != nil {
		return fmt.Errorf("resolve memories dir: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return fmt.Errorf("create memories dir: %w", err)
	}
	sb, err := sandbox.NewLocal(absDir)
	if err != nil {
		return fmt.Errorf("memory sandbox: %w", err)
	}
	ws := sandbox.NewLocalWorkspace(sb)
	if strings.TrimSpace(sqlitePath) == "" {
		sqlitePath = filepath.Join(absDir, ".moss", "memory.db")
	}
	store, err := runtime.NewSQLiteMemoryStore(sqlitePath)
	if err != nil {
		return fmt.Errorf("memory sqlite store: %w", err)
	}
	k.Apply(runtime.WithMemoryWorkspace(ws), runtime.WithMemoryStore(store))
	return nil
}
