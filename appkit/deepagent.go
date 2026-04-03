package appkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/appkit/runtime"
	config "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/sandbox"
)

// DeepAgentConfig 描述 deep-agent 风格装配的配置项。
// 零值通过 BuildDeepAgentKernel 的默认值补齐。
type DeepAgentConfig struct {
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
	AdditionalAppExtensions       []Extension
}

// ApplyOver returns a new DeepAgentConfig where every non-zero field in c
// overrides the corresponding field in base. Zero values in c are left
// untouched, preserving base's value. This implements "caller wins if set"
// overlay semantics and is the single authoritative merge path.
func (c DeepAgentConfig) ApplyOver(base DeepAgentConfig) DeepAgentConfig {
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
	if len(c.AdditionalAppExtensions) > 0 {
		base.AdditionalAppExtensions = c.AdditionalAppExtensions
	}
	return base
}
func DefaultDeepAgentConfig() DeepAgentConfig {
	return DeepAgentConfig{
		AppName:                       config.AppName(),
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

// BuildDeepAgentKernel 构建 deep-agent 风格的 Kernel 预设：
//  1. 默认扩展（内置工具、MCP、skills、agents）
//  2. 可选会话持久化（文件存储）
//  3. 可选 bootstrap 上下文加载
//  4. 自动注入 general-purpose 子 Agent（若未提供）
//  5. restricted 模式下默认审批高风险工具
func BuildDeepAgentKernel(ctx context.Context, flags *AppFlags, io port.UserIO, cfg *DeepAgentConfig) (*kernel.Kernel, error) {
	effective := DefaultDeepAgentConfig()
	if cfg != nil {
		effective = cfg.ApplyOver(effective)
	}

	var exts []Extension
	exts = append(exts, effective.AdditionalAppExtensions...)

	var stateCatalog *runtime.StateCatalog
	if appDir := config.AppDir(); appDir != "" {
		catalog, err := runtime.NewStateCatalog(filepath.Join(appDir, "state"), filepath.Join(appDir, "state", "events"), runtime.StateCatalogEnabledFromEnv())
		if err != nil {
			return nil, fmt.Errorf("state catalog: %w", err)
		}
		stateCatalog = catalog
		exts = append(exts, WithKernelOptions(runtime.WithStateCatalog(stateCatalog)))
	}

	if valueOrDefault(effective.EnableSessionStore, true) {
		storeDir := effective.SessionStoreDir
		if storeDir == "" {
			if appDir := config.AppDir(); appDir != "" {
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
		exts = append(exts, WithSessionStore(store))
		if valueOrDefault(effective.EnableContextOffload, true) {
			exts = append(exts, WithContextOffload(store))
			exts = append(exts, WithContextManagement(store))
		}
	}
	if valueOrDefault(effective.EnableCheckpointStore, true) {
		checkpointDir := effective.CheckpointStoreDir
		if checkpointDir == "" {
			if appDir := config.AppDir(); appDir != "" {
				checkpointDir = filepath.Join(appDir, "checkpoints")
			} else {
				checkpointDir = filepath.Join(flags.Workspace, "."+effective.AppName, "checkpoints")
			}
		}
		store, err := port.NewFileCheckpointStore(checkpointDir)
		if err != nil {
			return nil, fmt.Errorf("checkpoint store: %w", err)
		}
		checkpointStore := runtime.WrapCheckpointStore(store, stateCatalog)
		exts = append(exts, WithKernelOptions(kernel.WithCheckpoints(checkpointStore)))
	}
	if valueOrDefault(effective.EnableTaskRuntime, true) {
		taskDir := effective.TaskRuntimeDir
		if taskDir == "" {
			if appDir := config.AppDir(); appDir != "" {
				taskDir = filepath.Join(appDir, "tasks")
			} else {
				taskDir = filepath.Join(flags.Workspace, "."+effective.AppName, "tasks")
			}
		}
		taskRuntime, err := port.NewFileTaskRuntime(taskDir)
		if err != nil {
			return nil, fmt.Errorf("task runtime: %w", err)
		}
		exts = append(exts, WithKernelOptions(kernel.WithTaskRuntime(runtime.WrapTaskRuntime(taskRuntime, stateCatalog))))
	}

	if valueOrDefault(effective.EnablePersistentMemories, true) {
		memDir := effective.MemoryDir
		if memDir == "" {
			if appDir := config.AppDir(); appDir != "" {
				memDir = filepath.Join(appDir, "memories")
			} else {
				memDir = filepath.Join(flags.Workspace, "."+effective.AppName, "memories")
			}
		}
		exts = append(exts, WithPersistentMemories(memDir))
	}
	if valueOrDefault(effective.EnableWorkspaceIsolation, true) {
		isolationRoot := effective.IsolationRootDir
		if isolationRoot == "" {
			if appDir := config.AppDir(); appDir != "" {
				isolationRoot = filepath.Join(appDir, "workspaces")
			} else {
				isolationRoot = filepath.Join(flags.Workspace, "."+effective.AppName, "workspaces")
			}
		}
		if err := os.MkdirAll(isolationRoot, 0755); err != nil {
			return nil, fmt.Errorf("workspace isolation root: %w", err)
		}
		isolation, err := sandbox.NewLocalWorkspaceIsolation(isolationRoot)
		if err != nil {
			return nil, fmt.Errorf("workspace isolation: %w", err)
		}
		exts = append(exts, WithKernelOptions(kernel.WithWorkspaceIsolation(isolation)))
	}
	exts = append(exts, WithKernelOptions(kernel.WithRepoStateCapture(sandbox.NewGitRepoStateCapture(flags.Workspace))))
	exts = append(exts, WithKernelOptions(kernel.WithPatchApply(sandbox.NewGitPatchApply(flags.Workspace))))
	exts = append(exts, WithKernelOptions(kernel.WithPatchRevert(sandbox.NewGitPatchRevert(flags.Workspace))))
	exts = append(exts, WithKernelOptions(kernel.WithWorktreeSnapshots(sandbox.NewGitWorktreeSnapshotStore(flags.Workspace))))
	if strings.EqualFold(strings.TrimSpace(flags.Profile), "planning") {
		exts = append(exts, WithPlanning())
	}

	if valueOrDefault(effective.EnableBootstrapContext, true) {
		exts = append(exts, WithLoadedBootstrapContextWithTrust(flags.Workspace, effective.AppName, flags.Trust))
	}

	var defaultLLMRetry *retry.Config
	if valueOrDefault(effective.EnableDefaultLLMRetry, true) {
		defaultLLMRetry = effective.LLMRetryConfig
		if defaultLLMRetry == nil {
			defaultLLMRetry = &retry.Config{
				MaxRetries:   2,
				InitialDelay: 300 * time.Millisecond,
				MaxDelay:     2 * time.Second,
				Multiplier:   2.0,
			}
		}
	}
	if effective.LLMBreakerConfig != nil {
		exts = append(exts, WithKernelOptions(kernel.WithLLMBreaker(*effective.LLMBreakerConfig)))
	}

	k, err := BuildKernelWithConfig(ctx, flags, io, BuildConfig{
		DefaultLLMRetry:     defaultLLMRetry,
		DefaultSetupOptions: effective.DefaultSetupOptions,
		Extensions:          exts,
	})
	if err != nil {
		return nil, err
	}

	if valueOrDefault(effective.EnsureGeneralPurpose, true) {
		if err := ensureGeneralPurposeAgent(k, flags, effective); err != nil {
			return nil, err
		}
	}
	k.Middleware().Use(builtins.PatchToolCalls())

	if config.NormalizeTrustLevel(flags.Trust) == config.TrustRestricted && valueOrDefault(effective.EnableDefaultRestrictedPolicy, true) {
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

func ensureGeneralPurposeAgent(k *kernel.Kernel, flags *AppFlags, cfg DeepAgentConfig) error {
	reg := runtime.AgentRegistry(k)
	if _, ok := reg.Get(cfg.GeneralPurposeName); ok {
		return nil
	}

	toolSpecs := k.ToolRegistry().List()
	toolNames := make([]string, 0, len(toolSpecs))
	for _, spec := range toolSpecs {
		switch spec.Name {
		case "delegate_agent", "spawn_agent", "query_agent", "task", "list_tasks", "cancel_task", "update_task":
			continue
		default:
			toolNames = append(toolNames, spec.Name)
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
