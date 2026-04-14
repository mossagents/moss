package harness

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/bootstrap"
	"github.com/mossagents/moss/internal/runtimeexecution"
	"github.com/mossagents/moss/internal/runtimepolicy"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/runtime"
)

// KernelOptions returns a Feature that applies raw kernel.Option values to
// the Kernel during installation. This is the escape hatch for options that
// don't have a dedicated Feature constructor.
func KernelOptions(opts ...kernel.Option) Feature {
	return FeatureFunc{
		FeatureName: "kernel-options",
		InstallFunc: func(_ context.Context, h *Harness) error {
			h.Kernel().Apply(opts...)
			return nil
		},
	}
}

// BootstrapContext returns a Feature that loads AGENTS.md / SOUL.md /
// workspace-level instructions and injects them into the system prompt.
func BootstrapContext(workspace, appName, trust string) Feature {
	return FeatureFunc{
		FeatureName: "bootstrap-context",
		MetadataValue: FeatureMetadata{
			Key:   "bootstrap-context",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			bctx := bootstrap.LoadWithAppNameAndTrust(workspace, appName, trust)
			h.Kernel().Prompts().Add(100, func(_ *kernel.Kernel) string {
				return bctx.SystemPromptSection()
			})
			return nil
		},
	}
}

// BootstrapContextValue returns a Feature that injects a pre-loaded bootstrap context.
func BootstrapContextValue(ctx *bootstrap.Context) Feature {
	return FeatureFunc{
		FeatureName: "bootstrap-context",
		MetadataValue: FeatureMetadata{
			Key:   "bootstrap-context",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			if ctx == nil {
				return nil
			}
			h.Kernel().Prompts().Add(100, func(_ *kernel.Kernel) string {
				return ctx.SystemPromptSection()
			})
			return nil
		},
	}
}

// LoadedBootstrapContext returns a Feature that loads bootstrap context using
// bootstrap.LoadWithAppName trusted semantics.
func LoadedBootstrapContext(workspace, appName string) Feature {
	return BootstrapContextValue(bootstrap.LoadWithAppName(workspace, appName))
}

// SessionLifecycleRegistration describes a session lifecycle hook registration.
type SessionLifecycleRegistration struct {
	Order int
	Hook  session.LifecycleHook
}

// ToolLifecycleRegistration describes a tool lifecycle hook registration.
type ToolLifecycleRegistration struct {
	Order int
	Hook  hooks.ToolHook
}

// SessionLifecycleHooks returns a Feature that registers session lifecycle hooks.
func SessionLifecycleHooks(registrations ...SessionLifecycleRegistration) Feature {
	return FeatureFunc{
		FeatureName: "session-lifecycle-hooks",
		MetadataValue: FeatureMetadata{
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			for _, registration := range registrations {
				registration := registration
				if registration.Hook == nil {
					continue
				}
				h.Kernel().Hooks().OnSessionLifecycle.AddHook("", func(ctx context.Context, ev *session.LifecycleEvent) error {
					if ev == nil {
						return nil
					}
					registration.Hook(ctx, *ev)
					return nil
				}, registration.Order)
			}
			return nil
		},
	}
}

// ToolLifecycleHooks returns a Feature that registers tool lifecycle hooks.
func ToolLifecycleHooks(registrations ...ToolLifecycleRegistration) Feature {
	return FeatureFunc{
		FeatureName: "tool-lifecycle-hooks",
		MetadataValue: FeatureMetadata{
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			for _, registration := range registrations {
				registration := registration
				if registration.Hook == nil {
					continue
				}
				h.Kernel().Hooks().OnToolLifecycle.AddHook("", func(ctx context.Context, ev *hooks.ToolEvent) error {
					if ev == nil {
						return nil
					}
					registration.Hook(ctx, *ev)
					return nil
				}, registration.Order)
			}
			return nil
		},
	}
}

// SessionPersistence returns a Feature that enables persistent session storage
// and kernel-managed lifecycle persistence.
func SessionPersistence(store session.SessionStore) Feature {
	return FeatureFunc{
		FeatureName: "session-store",
		MetadataValue: FeatureMetadata{
			Key:   "session-store",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			if store == nil {
				return fmt.Errorf("session store must not be nil")
			}
			h.Kernel().Apply(kernel.WithPersistentSessionStore(store))
			return nil
		},
	}
}

// Checkpointing returns a Feature that enables session checkpoints
// (fork / replay / worktree snapshots).
func Checkpointing(store checkpoint.CheckpointStore) Feature {
	return FeatureFunc{
		FeatureName: "checkpointing",
		MetadataValue: FeatureMetadata{
			Key:   "checkpoint-store",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			if store == nil {
				return fmt.Errorf("checkpoint store must not be nil")
			}
			h.Kernel().Apply(kernel.WithCheckpoints(store))
			return nil
		},
	}
}

// TaskDelegation returns a Feature that enables async task delegation
// with a Mailbox for sub-agent communication.
func TaskDelegation(rt taskrt.TaskRuntime) Feature {
	return FeatureFunc{
		FeatureName: "task-delegation",
		MetadataValue: FeatureMetadata{
			Key:   "task-runtime",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			if rt == nil {
				return fmt.Errorf("task runtime must not be nil")
			}
			h.Kernel().Apply(kernel.WithTaskRuntime(rt))
			return nil
		},
	}
}

// StateCatalog returns a Feature that installs a runtime state catalog.
func StateCatalog(catalog *runtime.StateCatalog) Feature {
	return FeatureFunc{
		FeatureName: "state-catalog",
		MetadataValue: FeatureMetadata{
			Key:   "state-catalog",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			if catalog == nil {
				return fmt.Errorf("state catalog must not be nil")
			}
			h.Kernel().Apply(runtime.WithStateCatalog(catalog))
			return nil
		},
	}
}

// ExecutionServices returns a Feature that installs auxiliary execution
// services around the active backend-owned workspace/executor ports.
func ExecutionServices(workspaceRoot, isolationRoot string, isolationEnabled bool) Feature {
	return FeatureFunc{
		FeatureName: "execution-services",
		MetadataValue: FeatureMetadata{
			Key:   "execution-services",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			return runtimeexecution.Install(h.Kernel(), workspaceRoot, isolationRoot, isolationEnabled)
		},
	}
}

// ExecutionCapabilityReport returns a Feature that reports execution capability
// readiness after runtime assembly.
func ExecutionCapabilityReport(workspace, isolationRoot string, isolationEnabled bool, reporters ...runtime.CapabilityReporter) Feature {
	return FeatureFunc{
		FeatureName: "execution-capability-report",
		MetadataValue: FeatureMetadata{
			Key:      "execution-capability-report",
			Phase:    FeaturePhasePostRuntime,
			Requires: []string{"execution-services"},
		},
		InstallFunc: func(ctx context.Context, h *Harness) error {
			reporter := runtime.NewCapabilityReporter(runtime.CapabilityStatusPath(), nil)
			if len(reporters) > 0 && reporters[0] != nil {
				reporter = reporters[0]
			}
			runtime.ReportExecutionProbe(
				ctx,
				reporter,
				runtime.ExecutionProbeFromKernel(h.Kernel(), workspace, isolationRoot, isolationEnabled),
			)
			return nil
		},
	}
}

// LLMResilience returns a Feature that configures LLM call retry and
// optional circuit-breaker policies.
func LLMResilience(retryCfg *retry.Config, breakerCfg *retry.BreakerConfig) Feature {
	return FeatureFunc{
		FeatureName: "llm-resilience",
		MetadataValue: FeatureMetadata{
			Key:   "llm-resilience",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			var opts []kernel.Option
			if retryCfg != nil {
				opts = append(opts, kernel.WithLLMRetry(*retryCfg))
			}
			if breakerCfg != nil {
				opts = append(opts, kernel.WithLLMBreaker(*breakerCfg))
			}
			h.Kernel().Apply(opts...)
			return nil
		},
	}
}

// ToolPolicy returns a Feature that installs the canonical structured tool policy.
func ToolPolicy(policy runtime.ToolPolicy) Feature {
	return FeatureFunc{
		FeatureName: "tool-policy",
		MetadataValue: FeatureMetadata{
			Key:   "tool-policy",
			Phase: FeaturePhasePostRuntime,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			return runtimepolicy.Apply(h.Kernel(), policy)
		},
	}
}

// PatchToolCalls returns a Feature that installs the PatchToolCalls
// hook which normalises LLM tool-call formatting before processing.
func PatchToolCalls() Feature {
	return FeatureFunc{
		FeatureName: "patch-tool-calls",
		MetadataValue: FeatureMetadata{
			Key:   "patch-tool-calls",
			Phase: FeaturePhasePostRuntime,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			h.Kernel().InstallPlugin(kernel.Plugin{
				Name:      "patch-tool-calls",
				BeforeLLM: builtins.PatchToolCalls(),
			})
			return nil
		},
	}
}
