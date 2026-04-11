package appkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/bootstrap"
	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/knowledge"
	"github.com/mossagents/moss/sandbox"
	"github.com/mossagents/moss/scheduler"
)

// Extension is an alias for harness.Feature.
// All appkit Feature constructors return this type.
type Extension = harness.Feature

// SessionLifecycleRegistration 描述一个 Session 生命周期 hook 注册项。
type SessionLifecycleRegistration struct {
	Order int
	Hook  session.LifecycleHook
}

// ToolLifecycleRegistration 描述一个工具调用生命周期 hook 注册项。
type ToolLifecycleRegistration struct {
	Order int
	Hook  session.ToolLifecycleHook
}

// RuntimeSetup returns a Feature that runs the standard runtime capability
// loading (builtin tools, MCP servers, skills, agents).
func RuntimeSetup(workspaceDir, trust string, opts ...runtime.Option) Extension {
	return harness.FeatureFunc{
		FeatureName: "runtime-setup",
		MetadataValue: harness.FeatureMetadata{
			Key:   "runtime-setup",
			Phase: harness.FeaturePhaseRuntime,
		},
		InstallFunc: func(ctx context.Context, h *harness.Harness) error {
			allOpts := make([]runtime.Option, 0, len(opts)+1)
			allOpts = append(allOpts, runtime.WithWorkspaceTrust(trust))
			allOpts = append(allOpts, opts...)
			return runtime.Setup(ctx, h.Kernel(), workspaceDir, allOpts...)
		},
	}
}

// WithKernelOptions wraps raw kernel.Option values into a Feature.
func WithKernelOptions(opts ...kernel.Option) Extension {
	return harness.KernelOptions(opts...)
}

// AfterBuild wraps a post-construction installer function into a Feature.
func AfterBuild(installer harness.Installer) Extension {
	return harness.FeatureFunc{
		FeatureName: "after-build",
		MetadataValue: harness.FeatureMetadata{
			Phase: harness.FeaturePhasePostRuntime,
		},
		InstallFunc: func(ctx context.Context, h *harness.Harness) error {
			if installer == nil {
				return nil
			}
			return installer(ctx, h.Kernel())
		},
	}
}

// WithSessionLifecycleHooks returns a Feature that registers Session lifecycle hooks.
func WithSessionLifecycleHooks(hooks ...SessionLifecycleRegistration) Extension {
	return harness.FeatureFunc{
		FeatureName: "session-lifecycle-hooks",
		MetadataValue: harness.FeatureMetadata{
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			bridge := kernel.Extensions(h.Kernel())
			for _, hook := range hooks {
				if hook.Hook == nil {
					continue
				}
				bridge.OnSessionLifecycle(hook.Order, hook.Hook)
			}
			return nil
		},
	}
}

// WithToolLifecycleHooks returns a Feature that registers tool lifecycle hooks.
func WithToolLifecycleHooks(hooks ...ToolLifecycleRegistration) Extension {
	return harness.FeatureFunc{
		FeatureName: "tool-lifecycle-hooks",
		MetadataValue: harness.FeatureMetadata{
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			bridge := kernel.Extensions(h.Kernel())
			for _, hook := range hooks {
				if hook.Hook == nil {
					continue
				}
				bridge.OnToolLifecycle(hook.Order, hook.Hook)
			}
			return nil
		},
	}
}

// WithSessionStore returns a Feature that installs a SessionStore.
func WithSessionStore(store session.SessionStore) Extension {
	return harness.FeatureFunc{
		FeatureName: "session-store",
		MetadataValue: harness.FeatureMetadata{
			Key:   "session-store",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			h.Kernel().Apply(runtime.WithKernelSessionStore(store))
			return nil
		},
	}
}

// WithPlanning returns a Feature that installs the write_todos planning tool.
func WithPlanning() Extension {
	return harness.FeatureFunc{
		FeatureName: "planning",
		MetadataValue: harness.FeatureMetadata{
			Key:   "planning",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			h.Kernel().Apply(runtime.WithPlanningDefaults())
			return nil
		},
	}
}

// WithContextOffload returns a Feature that installs context offload (compression) tools.
func WithContextOffload(store session.SessionStore) Extension {
	return harness.FeatureFunc{
		FeatureName: "context-offload",
		MetadataValue: harness.FeatureMetadata{
			Key:   "context-offload",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			h.Kernel().Apply(runtime.WithOffloadSessionStore(store))
			return runtime.RegisterOffloadTools(h.Kernel().ToolRegistry(), store, h.Kernel().SessionManager())
		},
	}
}

// WithContextManagement returns a Feature that installs auto context compression
// and the compact_conversation tool.
func WithContextManagement(store session.SessionStore, opts ...runtime.ContextOption) Extension {
	return harness.FeatureFunc{
		FeatureName: "context-management",
		MetadataValue: harness.FeatureMetadata{
			Key:   "context-management",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			kopts := []kernel.Option{runtime.WithContextSessionStore(store)}
			if len(opts) > 0 {
				kopts = append(kopts, runtime.ConfigureContext(opts...))
			}
			h.Kernel().Apply(kopts...)
			return nil
		},
	}
}

// WithBootstrapContext returns a Feature that installs a pre-loaded bootstrap context.
func WithBootstrapContext(ctx *bootstrap.Context) Extension {
	return harness.FeatureFunc{
		FeatureName: "bootstrap-context",
		MetadataValue: harness.FeatureMetadata{
			Key:   "bootstrap-context",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			h.Kernel().Apply(runtime.WithBootstrapContext(ctx))
			return nil
		},
	}
}

// WithLoadedBootstrapContext returns a Feature that loads bootstrap context from workspace.
func WithLoadedBootstrapContext(workspace, appName string) Extension {
	return harness.FeatureFunc{
		FeatureName: "bootstrap-context",
		MetadataValue: harness.FeatureMetadata{
			Key:   "bootstrap-context",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			h.Kernel().Apply(runtime.WithLoadedBootstrapContext(workspace, appName))
			return nil
		},
	}
}

// WithLoadedBootstrapContextWithTrust returns a Feature that loads bootstrap context
// with the given trust level.
func WithLoadedBootstrapContextWithTrust(workspace, appName, trust string) Extension {
	return harness.FeatureFunc{
		FeatureName: "bootstrap-context",
		MetadataValue: harness.FeatureMetadata{
			Key:   "bootstrap-context",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			h.Kernel().Apply(runtime.WithLoadedBootstrapContextWithTrust(workspace, appName, trust))
			return nil
		},
	}
}

// WithScheduling returns a Feature that installs a scheduler and registers scheduler tools.
func WithScheduling(s *scheduler.Scheduler) Extension {
	return harness.FeatureFunc{
		FeatureName: "scheduling",
		MetadataValue: harness.FeatureMetadata{
			Key:   "scheduling",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			h.Kernel().Apply(runtime.WithScheduler(s))
			return runtime.RegisterSchedulerTools(h.Kernel(), s)
		},
	}
}

// WithKnowledge returns a Feature that registers knowledge base tools.
func WithKnowledge(store knowledge.Store, embedder model.Embedder) Extension {
	return harness.FeatureFunc{
		FeatureName: "knowledge",
		MetadataValue: harness.FeatureMetadata{
			Key:   "knowledge",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			return runtime.RegisterKnowledgeTools(h.Kernel(), store, embedder)
		},
	}
}

// WithPersistentMemories returns a Feature that installs persistent memory tools.
func WithPersistentMemories(memoriesDir string) Extension {
	return harness.FeatureFunc{
		FeatureName: "persistent-memories",
		MetadataValue: harness.FeatureMetadata{
			Key:   "persistent-memories",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			if memoriesDir == "" {
				return fmt.Errorf("memories dir is empty")
			}
			absDir, err := filepath.Abs(memoriesDir)
			if err != nil {
				return fmt.Errorf("resolve memories dir: %w", err)
			}
			if err := os.MkdirAll(absDir, 0755); err != nil {
				return fmt.Errorf("create memories dir: %w", err)
			}
			sb, err := sandbox.NewLocal(absDir)
			if err != nil {
				return fmt.Errorf("memory sandbox: %w", err)
			}
			ws := sandbox.NewLocalWorkspace(sb)
			runtime.WithMemoryWorkspace(ws)(h.Kernel())
			sqlitePath := filepath.Join(absDir, ".moss", "memory.db")
			store, err := runtime.NewSQLiteMemoryStore(sqlitePath)
			if err != nil {
				return fmt.Errorf("memory sqlite store: %w", err)
			}
			runtime.WithMemoryStore(store)(h.Kernel())
			return runtime.RegisterMemoryToolsWithRuntime(h.Kernel().ToolRegistry(), ws, store, h.Kernel().TaskRuntime())
		},
	}
}

// WithPersistentMemoriesSQLite returns a Feature with explicit SQLite path for memory store.
func WithPersistentMemoriesSQLite(memoriesDir string, sqlitePath string) Extension {
	return harness.FeatureFunc{
		FeatureName: "persistent-memories-sqlite",
		MetadataValue: harness.FeatureMetadata{
			Key:   "persistent-memories",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			if strings.TrimSpace(memoriesDir) == "" {
				return fmt.Errorf("memories dir is empty")
			}
			absDir, err := filepath.Abs(memoriesDir)
			if err != nil {
				return fmt.Errorf("resolve memories dir: %w", err)
			}
			if err := os.MkdirAll(absDir, 0755); err != nil {
				return fmt.Errorf("create memories dir: %w", err)
			}
			sb, err := sandbox.NewLocal(absDir)
			if err != nil {
				return fmt.Errorf("memory sandbox: %w", err)
			}
			ws := sandbox.NewLocalWorkspace(sb)
			runtime.WithMemoryWorkspace(ws)(h.Kernel())
			if strings.TrimSpace(sqlitePath) == "" {
				sqlitePath = filepath.Join(absDir, ".moss", "memory.db")
			}
			store, err := runtime.NewSQLiteMemoryStore(sqlitePath)
			if err != nil {
				return fmt.Errorf("memory sqlite store: %w", err)
			}
			runtime.WithMemoryStore(store)(h.Kernel())
			return runtime.RegisterMemoryToolsWithRuntime(h.Kernel().ToolRegistry(), ws, store, h.Kernel().TaskRuntime())
		},
	}
}
