package harness

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/harness/bootstrap"
	"github.com/mossagents/moss/harness/extensions/capability"
	"github.com/mossagents/moss/harness/runtime"
	"github.com/mossagents/moss/harness/runtime/execution"
	runtimepolicy "github.com/mossagents/moss/harness/runtime/policy"
	rprobe "github.com/mossagents/moss/harness/runtime/probe"
	rstate "github.com/mossagents/moss/harness/runtime/state"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	kplugin "github.com/mossagents/moss/kernel/plugin"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
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
			if err := h.Kernel().Prompts().Add(100, func(_ *kernel.Kernel) string {
				return bctx.SystemPromptSection()
			}); err != nil {
				return fmt.Errorf("register bootstrap prompt: %w", err)
			}
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
			if err := h.Kernel().Prompts().Add(100, func(_ *kernel.Kernel) string {
				return ctx.SystemPromptSection()
			}); err != nil {
				return fmt.Errorf("register bootstrap prompt: %w", err)
			}
			return nil
		},
	}
}

// LoadedBootstrapContext returns a Feature that loads bootstrap context using
// bootstrap.LoadWithAppName trusted semantics.
func LoadedBootstrapContext(workspace, appName string) Feature {
	return BootstrapContextValue(bootstrap.LoadWithAppName(workspace, appName))
}

// Plugins returns a Feature that installs one or more lifecycle plugins.
func Plugins(plugins ...kernel.Plugin) Feature {
	return FeatureFunc{
		FeatureName: "plugins",
		MetadataValue: FeatureMetadata{
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			for _, plugin := range plugins {
				if err := h.Kernel().InstallPlugin(plugin); err != nil {
					return err
				}
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
func StateCatalog(catalog *rstate.StateCatalog) Feature {
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
			return execution.Install(h.Kernel(), workspaceRoot, isolationRoot, isolationEnabled)
		},
	}
}

// ExecutionCapabilityReport returns a Feature that reports execution capability
// readiness after runtime assembly.
func ExecutionCapabilityReport(workspace, isolationRoot string, isolationEnabled bool, reporters ...capability.CapabilityReporter) Feature {
	return FeatureFunc{
		FeatureName: "execution-capability-report",
		MetadataValue: FeatureMetadata{
			Key:      "execution-capability-report",
			Phase:    FeaturePhasePostRuntime,
			Requires: []string{"execution-services"},
		},
		InstallFunc: func(ctx context.Context, h *Harness) error {
			reporter := capability.NewCapabilityReporter(capability.CapabilityStatusPath(), nil)
			if len(reporters) > 0 && reporters[0] != nil {
				reporter = reporters[0]
			}
			rprobe.ReportExecutionProbe(
				ctx,
				reporter,
				rprobe.ExecutionProbeFromKernel(h.Kernel(), workspace, isolationRoot, isolationEnabled),
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
func ToolPolicy(policy runtimepolicy.ToolPolicy) Feature {
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
			return h.Kernel().InstallPlugin(kplugin.BeforeLLMHook("patch-tool-calls", 0, builtins.PatchToolCalls()))
		},
	}
}
