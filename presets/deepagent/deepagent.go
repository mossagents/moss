package deepagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/runtime"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
)

// Config 描述 deep-agent 风格装配的配置项。
// 零值通过 DefaultConfig 的默认值补齐。
type Config struct {
	AppName                       string
	EnableSessionStore            *bool
	SessionStoreDir               string
	EnableCheckpointStore         *bool
	CheckpointStoreDir            string
	EnableTaskRuntime             *bool
	TaskRuntimeDir                string
	EnablePersistentMemories      *bool
	MemoryDir                     string
	EnableContextOffload          *bool
	EnableBootstrapContext        *bool
	EnsureGeneralPurpose          *bool
	GeneralPurposeName            string
	GeneralPurposePrompt          string
	GeneralPurposeDesc            string
	GeneralPurposeMaxSteps        int
	EnableWorkspaceIsolation      *bool
	IsolationRootDir              string
	EnableDefaultRestrictedPolicy *bool
	EnableDefaultLLMRetry         *bool
	LLMRetryConfig                *retry.Config
	LLMBreakerConfig              *retry.BreakerConfig
	DefaultSetupOptions           []runtime.Option
	AdditionalFeatures            []harness.Feature
}

// ApplyOver returns a new Config where every non-zero field in c overrides the
// corresponding field in base. Zero values in c are left untouched.
func (c Config) ApplyOver(base Config) Config {
	if c.AppName != "" {
		base.AppName = c.AppName
	}
	if c.EnableSessionStore != nil {
		base.EnableSessionStore = c.EnableSessionStore
	}
	if c.SessionStoreDir != "" {
		base.SessionStoreDir = c.SessionStoreDir
	}
	if c.EnableCheckpointStore != nil {
		base.EnableCheckpointStore = c.EnableCheckpointStore
	}
	if c.CheckpointStoreDir != "" {
		base.CheckpointStoreDir = c.CheckpointStoreDir
	}
	if c.EnableTaskRuntime != nil {
		base.EnableTaskRuntime = c.EnableTaskRuntime
	}
	if c.TaskRuntimeDir != "" {
		base.TaskRuntimeDir = c.TaskRuntimeDir
	}
	if c.EnablePersistentMemories != nil {
		base.EnablePersistentMemories = c.EnablePersistentMemories
	}
	if c.MemoryDir != "" {
		base.MemoryDir = c.MemoryDir
	}
	if c.EnableContextOffload != nil {
		base.EnableContextOffload = c.EnableContextOffload
	}
	if c.EnableBootstrapContext != nil {
		base.EnableBootstrapContext = c.EnableBootstrapContext
	}
	if c.EnsureGeneralPurpose != nil {
		base.EnsureGeneralPurpose = c.EnsureGeneralPurpose
	}
	if c.GeneralPurposeName != "" {
		base.GeneralPurposeName = c.GeneralPurposeName
	}
	if c.GeneralPurposePrompt != "" {
		base.GeneralPurposePrompt = c.GeneralPurposePrompt
	}
	if c.GeneralPurposeDesc != "" {
		base.GeneralPurposeDesc = c.GeneralPurposeDesc
	}
	if c.GeneralPurposeMaxSteps > 0 {
		base.GeneralPurposeMaxSteps = c.GeneralPurposeMaxSteps
	}
	if c.EnableWorkspaceIsolation != nil {
		base.EnableWorkspaceIsolation = c.EnableWorkspaceIsolation
	}
	if c.IsolationRootDir != "" {
		base.IsolationRootDir = c.IsolationRootDir
	}
	if c.EnableDefaultRestrictedPolicy != nil {
		base.EnableDefaultRestrictedPolicy = c.EnableDefaultRestrictedPolicy
	}
	if c.EnableDefaultLLMRetry != nil {
		base.EnableDefaultLLMRetry = c.EnableDefaultLLMRetry
	}
	if c.LLMRetryConfig != nil {
		base.LLMRetryConfig = c.LLMRetryConfig
	}
	if c.LLMBreakerConfig != nil {
		base.LLMBreakerConfig = c.LLMBreakerConfig
	}
	if len(c.DefaultSetupOptions) > 0 {
		base.DefaultSetupOptions = c.DefaultSetupOptions
	}
	if len(c.AdditionalFeatures) > 0 {
		base.AdditionalFeatures = c.AdditionalFeatures
	}
	return base
}

// DefaultConfig returns the default deep preset configuration.
func DefaultConfig() Config {
	return Config{
		AppName:                       appconfig.AppName(),
		EnableSessionStore:            boolPtr(true),
		EnableCheckpointStore:         boolPtr(true),
		EnableTaskRuntime:             boolPtr(true),
		EnablePersistentMemories:      boolPtr(true),
		EnableContextOffload:          boolPtr(true),
		EnableBootstrapContext:        boolPtr(true),
		EnsureGeneralPurpose:          boolPtr(true),
		GeneralPurposeName:            "general-purpose",
		GeneralPurposePrompt:          "You are a general-purpose delegated assistant. Complete delegated tasks thoroughly and return concise results.",
		GeneralPurposeDesc:            "General-purpose agent for delegated tasks that need context isolation.",
		GeneralPurposeMaxSteps:        50,
		EnableWorkspaceIsolation:      boolPtr(true),
		EnableDefaultRestrictedPolicy: boolPtr(true),
		EnableDefaultLLMRetry:         boolPtr(true),
	}
}

// BuildKernel builds a deep-agent style kernel preset.
func BuildKernel(ctx context.Context, flags *appkit.AppFlags, io io.UserIO, cfg *Config) (*kernel.Kernel, error) {
	effective := DefaultConfig()
	if cfg != nil {
		effective = cfg.ApplyOver(effective)
	}

	var features []harness.Feature
	features = append(features, effective.AdditionalFeatures...)

	var stateCatalog *runtime.StateCatalog
	if appDir := appconfig.AppDir(); appDir != "" {
		catalog, err := runtime.NewStateCatalog(filepath.Join(appDir, "state"), filepath.Join(appDir, "state", "events"), runtime.StateCatalogEnabledFromEnv())
		if err != nil {
			return nil, fmt.Errorf("state catalog: %w", err)
		}
		stateCatalog = catalog
		features = append(features, harness.KernelOptions(runtime.WithStateCatalog(stateCatalog)))
	}

	if valueOrDefault(effective.EnableSessionStore, true) {
		storeDir := effective.SessionStoreDir
		if storeDir == "" {
			if appDir := appconfig.AppDir(); appDir != "" {
				storeDir = filepath.Join(appDir, "sessions")
			} else {
				storeDir = filepath.Join(flags.Workspace, "."+effective.AppName, "sessions")
			}
		}
		rawStore, err := session.NewFileStore(storeDir)
		if err != nil {
			return nil, fmt.Errorf("session store: %w", err)
		}
		var store session.SessionStore = rawStore
		store = runtime.WrapSessionStore(store, stateCatalog)
		features = append(features, appkit.WithSessionStore(store))
		if valueOrDefault(effective.EnableContextOffload, true) {
			features = append(features, appkit.WithContextOffload(store))
			features = append(features, appkit.WithContextManagement(store))
		}
	}
	if valueOrDefault(effective.EnableCheckpointStore, true) {
		checkpointDir := effective.CheckpointStoreDir
		if checkpointDir == "" {
			if appDir := appconfig.AppDir(); appDir != "" {
				checkpointDir = filepath.Join(appDir, "checkpoints")
			} else {
				checkpointDir = filepath.Join(flags.Workspace, "."+effective.AppName, "checkpoints")
			}
		}
		store, err := checkpoint.NewFileCheckpointStore(checkpointDir)
		if err != nil {
			return nil, fmt.Errorf("checkpoint store: %w", err)
		}
		checkpointStore := runtime.WrapCheckpointStore(store, stateCatalog)
		features = append(features, harness.Checkpointing(checkpointStore))
	}
	if valueOrDefault(effective.EnableTaskRuntime, true) {
		taskDir := effective.TaskRuntimeDir
		if taskDir == "" {
			if appDir := appconfig.AppDir(); appDir != "" {
				taskDir = filepath.Join(appDir, "tasks")
			} else {
				taskDir = filepath.Join(flags.Workspace, "."+effective.AppName, "tasks")
			}
		}
		taskRuntime, err := taskrt.NewFileTaskRuntime(taskDir)
		if err != nil {
			return nil, fmt.Errorf("task runtime: %w", err)
		}
		features = append(features, harness.TaskDelegation(runtime.WrapTaskRuntime(taskRuntime, stateCatalog)))
	}

	if valueOrDefault(effective.EnablePersistentMemories, true) {
		memDir := effective.MemoryDir
		if memDir == "" {
			if appDir := appconfig.AppDir(); appDir != "" {
				memDir = filepath.Join(appDir, "memories")
			} else {
				memDir = filepath.Join(flags.Workspace, "."+effective.AppName, "memories")
			}
		}
		features = append(features, appkit.WithPersistentMemories(memDir))
	}
	isolationEnabled := valueOrDefault(effective.EnableWorkspaceIsolation, true)
	isolationRoot := effective.IsolationRootDir
	if isolationRoot == "" {
		if appDir := appconfig.AppDir(); appDir != "" {
			isolationRoot = filepath.Join(appDir, "workspaces")
		} else {
			isolationRoot = filepath.Join(flags.Workspace, "."+effective.AppName, "workspaces")
		}
	}
	if isolationEnabled {
		if err := os.MkdirAll(isolationRoot, 0o755); err != nil {
			return nil, fmt.Errorf("workspace isolation root: %w", err)
		}
	}
	executionSurface := runtime.NewExecutionSurface(flags.Workspace, isolationRoot, isolationEnabled)
	if err := executionSurface.Error(runtime.CapabilityExecutionIsolation); err != nil {
		return nil, fmt.Errorf("workspace isolation: %w", err)
	}
	features = append(features, harness.KernelOptions(executionSurface.KernelOptions()...))
	if strings.EqualFold(strings.TrimSpace(flags.Profile), "planning") {
		features = append(features, appkit.WithPlanning())
	}

	if valueOrDefault(effective.EnableBootstrapContext, true) {
		features = append(features, appkit.WithLoadedBootstrapContextWithTrust(flags.Workspace, effective.AppName, flags.Trust))
	}

	if valueOrDefault(effective.EnableDefaultLLMRetry, true) {
		llmRetryCfg := effective.LLMRetryConfig
		if llmRetryCfg == nil {
			llmRetryCfg = &retry.Config{
				MaxRetries:   2,
				InitialDelay: 300 * time.Millisecond,
				MaxDelay:     2 * time.Second,
				Multiplier:   2.0,
			}
		}
		features = append(features, harness.KernelOptions(kernel.WithLLMRetry(*llmRetryCfg)))
	}
	if effective.LLMBreakerConfig != nil {
		features = append(features, harness.KernelOptions(kernel.WithLLMBreaker(*effective.LLMBreakerConfig)))
	}

	// RuntimeSetup: load builtin tools, MCP servers, skills, agents
	features = append(features, appkit.RuntimeSetup(flags.Workspace, flags.Trust, effective.DefaultSetupOptions...))

	k, err := appkit.BuildKernelWithFeatures(ctx, flags, io, features...)
	if err != nil {
		return nil, err
	}
	runtime.ReportExecutionSurface(ctx, runtime.NewCapabilityReporter(runtime.CapabilityStatusPath(), nil), runtime.ExecutionSurfaceFromKernel(k, flags.Workspace, isolationRoot, isolationEnabled))

	if valueOrDefault(effective.EnsureGeneralPurpose, true) {
		if err := ensureGeneralPurposeAgent(k, flags, effective); err != nil {
			return nil, err
		}
	}
	k.InstallPlugin(kernel.Plugin{
		Name:      "patch-tool-calls",
		BeforeLLM: builtins.PatchToolCalls(),
	})

	if appconfig.NormalizeTrustLevel(flags.Trust) == appconfig.TrustRestricted && valueOrDefault(effective.EnableDefaultRestrictedPolicy, true) {
		k.WithPolicy(
			builtins.DenyCommandContaining("rm -rf /", "format c:", "del /f /q c:\\"),
			builtins.RequireApprovalForPathPrefix(".git", ".moss"),
			builtins.RequireApprovalFor(
				"write_file", "edit_file", "run_command", "spawn_agent", "task",
				"cancel_task", "update_task",
				"write_memory", "delete_memory", "offload_context",
				"acquire_workspace", "release_workspace",
			),
			builtins.DefaultAllow(),
		)
	}

	return k, nil
}

func ensureGeneralPurposeAgent(k *kernel.Kernel, flags *appkit.AppFlags, cfg Config) error {
	reg := runtime.AgentRegistry(k)
	if _, ok := reg.Get(cfg.GeneralPurposeName); ok {
		return nil
	}

	toolList := k.ToolRegistry().List()
	toolNames := make([]string, 0, len(toolList))
	for _, t := range toolList {
		switch t.Name() {
		case "delegate_agent", "spawn_agent", "query_agent", "task", "list_tasks", "cancel_task", "update_task":
			continue
		default:
			toolNames = append(toolNames, t.Name())
		}
	}
	sort.Strings(toolNames)

	trust := flags.Trust
	if trust == "" {
		trust = "restricted"
	}

	return reg.Register(agent.AgentConfig{
		Name:         cfg.GeneralPurposeName,
		Description:  cfg.GeneralPurposeDesc,
		SystemPrompt: cfg.GeneralPurposePrompt,
		Tools:        toolNames,
		MaxSteps:     cfg.GeneralPurposeMaxSteps,
		TrustLevel:   trust,
	})
}

func boolPtr(v bool) *bool { return &v }

func valueOrDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}
