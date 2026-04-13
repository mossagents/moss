package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/knowledge"
	"github.com/mossagents/moss/runtime"
	"github.com/mossagents/moss/sandbox"
	"github.com/mossagents/moss/scheduler"
)

// RuntimeSetup returns a Feature that runs the standard runtime capability
// loading (builtin tools, MCP servers, skills, agents).
func RuntimeSetup(workspaceDir, trust string, opts ...runtime.Option) Feature {
	return FeatureFunc{
		FeatureName: "runtime-setup",
		MetadataValue: FeatureMetadata{
			Key:   "runtime-setup",
			Phase: FeaturePhaseRuntime,
		},
		InstallFunc: func(ctx context.Context, h *Harness) error {
			allOpts := make([]runtime.Option, 0, len(opts)+1)
			allOpts = append(allOpts, runtime.WithWorkspaceTrust(trust))
			allOpts = append(allOpts, opts...)
			return runtime.Setup(ctx, h.Kernel(), workspaceDir, allOpts...)
		},
	}
}

// ContextOption configures harness-owned context-management feature behavior.
type ContextOption interface {
	runtimeOption() runtime.ContextOption
}

type contextOption struct {
	option runtime.ContextOption
}

func (o contextOption) runtimeOption() runtime.ContextOption {
	return o.option
}

func WithTriggerDialogCount(n int) ContextOption {
	return contextOption{option: runtime.WithTriggerDialogCount(n)}
}

func WithKeepRecent(n int) ContextOption {
	return contextOption{option: runtime.WithKeepRecent(n)}
}

func WithContextTriggerTokens(n int) ContextOption {
	return contextOption{option: runtime.WithContextTriggerTokens(n)}
}

func WithContextPromptBudget(n int) ContextOption {
	return contextOption{option: runtime.WithContextPromptBudget(n)}
}

func WithContextStartupBudget(n int) ContextOption {
	return contextOption{option: runtime.WithContextStartupBudget(n)}
}

func runtimeContextOptions(opts []ContextOption) []runtime.ContextOption {
	out := make([]runtime.ContextOption, 0, len(opts))
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		out = append(out, opt.runtimeOption())
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
			h.Kernel().Apply(runtime.WithPlanningDefaults())
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
			h.Kernel().Apply(runtime.WithOffloadSessionStore(store))
			return runtime.RegisterOffloadTools(h.Kernel().ToolRegistry(), store, h.Kernel().SessionManager())
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
			kopts := []kernel.Option{runtime.WithContextSessionStore(store)}
			if rtOpts := runtimeContextOptions(opts); len(rtOpts) > 0 {
				kopts = append(kopts, runtime.ConfigureContext(rtOpts...))
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
