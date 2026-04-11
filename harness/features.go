package harness

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/bootstrap"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/hooks/builtins"
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

// Installer is a function called after a Kernel is constructed.
// It provides post-construction setup, such as registering tools or hooks.
type Installer func(context.Context, *kernel.Kernel) error

// InstallerFeature returns a Feature that runs the given Installer during
// installation. This bridges legacy Installer functions into the Feature model.
func InstallerFeature(name string, installer Installer) Feature {
	return FeatureFunc{
		FeatureName: name,
		InstallFunc: func(ctx context.Context, h *Harness) error {
			if installer == nil {
				return nil
			}
			return installer(ctx, h.Kernel())
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
			bridge := kernel.Extensions(h.Kernel())
			bridge.OnSystemPrompt(100, func(_ *kernel.Kernel) string {
				return bctx.SystemPromptSection()
			})
			return nil
		},
	}
}

// SessionPersistence returns a Feature that enables persistent session storage.
func SessionPersistence(store session.SessionStore) Feature {
	return FeatureFunc{
		FeatureName: "session-persistence",
		MetadataValue: FeatureMetadata{
			Key:   "session-store",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			if store == nil {
				return fmt.Errorf("session store must not be nil")
			}
			h.Kernel().Apply(kernel.WithSessionStore(store))
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

// ExecutionPolicy returns a Feature that configures tool-level access
// control policies (deny, require-approval, allow).
func ExecutionPolicy(rules ...builtins.PolicyRule) Feature {
	return FeatureFunc{
		FeatureName: "execution-policy",
		MetadataValue: FeatureMetadata{
			Key:   "execution-policy",
			Phase: FeaturePhasePostRuntime,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			h.Kernel().WithPolicy(rules...)
			return nil
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
