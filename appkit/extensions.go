package appkit

import (
	"context"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/bootstrap"
	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/knowledge"
	"github.com/mossagents/moss/scheduler"
)

// Extension is an alias for harness.Feature.
// All appkit Feature constructors return this type.
type Extension = harness.Feature

// SessionLifecycleRegistration 描述一个 Session 生命周期 hook 注册项。
type SessionLifecycleRegistration = harness.SessionLifecycleRegistration

// ToolLifecycleRegistration 描述一个工具调用生命周期 hook 注册项。
type ToolLifecycleRegistration = harness.ToolLifecycleRegistration

// ContextOption 描述上下文管理行为配置。
type ContextOption = harness.ContextOption

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

// WithSessionLifecycleHooks is a backward-compatible wrapper around harness.SessionLifecycleHooks.
func WithSessionLifecycleHooks(hooks ...SessionLifecycleRegistration) Extension {
	return harness.SessionLifecycleHooks(hooks...)
}

// WithToolLifecycleHooks is a backward-compatible wrapper around harness.ToolLifecycleHooks.
func WithToolLifecycleHooks(hooks ...ToolLifecycleRegistration) Extension {
	return harness.ToolLifecycleHooks(hooks...)
}

// WithSessionStore is a backward-compatible wrapper around harness.SessionPersistence.
func WithSessionStore(store session.SessionStore) Extension {
	return harness.SessionPersistence(store)
}

// WithPlanning returns a Feature that installs the write_todos planning tool.
func WithPlanning() Extension {
	return harness.Planning()
}

// WithContextOffload returns a Feature that installs context offload (compression) tools.
func WithContextOffload(store session.SessionStore) Extension {
	return harness.ContextOffload(store)
}

// WithContextManagement returns a Feature that installs auto context compression
// and the compact_conversation tool.
func WithContextManagement(store session.SessionStore, opts ...ContextOption) Extension {
	return harness.ContextManagement(store, opts...)
}

// WithBootstrapContext is a backward-compatible wrapper around harness.BootstrapContextValue.
func WithBootstrapContext(ctx *bootstrap.Context) Extension {
	return harness.BootstrapContextValue(ctx)
}

// WithLoadedBootstrapContext is a backward-compatible wrapper around harness.LoadedBootstrapContext.
func WithLoadedBootstrapContext(workspace, appName string) Extension {
	return harness.LoadedBootstrapContext(workspace, appName)
}

// WithLoadedBootstrapContextWithTrust is a backward-compatible wrapper around harness.BootstrapContext.
func WithLoadedBootstrapContextWithTrust(workspace, appName, trust string) Extension {
	return harness.BootstrapContext(workspace, appName, trust)
}

// WithScheduling returns a Feature that installs a scheduler and registers scheduler tools.
func WithScheduling(s *scheduler.Scheduler) Extension {
	return harness.Scheduling(s)
}

// WithKnowledge returns a Feature that registers knowledge base tools.
func WithKnowledge(store knowledge.Store, embedder model.Embedder) Extension {
	return harness.Knowledge(store, embedder)
}

// WithPersistentMemories returns a Feature that installs persistent memory tools.
func WithPersistentMemories(memoriesDir string) Extension {
	return harness.PersistentMemories(memoriesDir)
}

// WithPersistentMemoriesSQLite returns a Feature with explicit SQLite path for memory store.
func WithPersistentMemoriesSQLite(memoriesDir string, sqlitePath string) Extension {
	return harness.PersistentMemoriesSQLite(memoriesDir, sqlitePath)
}
