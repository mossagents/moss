package appkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// DefaultDeepAgentConfig 返回 deep-agent 装配默认配置。
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
		if cfg.AppName != "" {
			effective.AppName = cfg.AppName
		}
		if cfg.EnableSessionStore != nil {
			effective.EnableSessionStore = cfg.EnableSessionStore
		}
		if cfg.SessionStoreDir != "" {
			effective.SessionStoreDir = cfg.SessionStoreDir
		}
		if cfg.EnableCheckpointStore != nil {
			effective.EnableCheckpointStore = cfg.EnableCheckpointStore
		}
		if cfg.CheckpointStoreDir != "" {
			effective.CheckpointStoreDir = cfg.CheckpointStoreDir
		}
		if cfg.EnableTaskRuntime != nil {
			effective.EnableTaskRuntime = cfg.EnableTaskRuntime
		}
		if cfg.TaskRuntimeDir != "" {
			effective.TaskRuntimeDir = cfg.TaskRuntimeDir
		}
		if cfg.EnablePersistentMemories != nil {
			effective.EnablePersistentMemories = cfg.EnablePersistentMemories
		}
		if cfg.MemoryDir != "" {
			effective.MemoryDir = cfg.MemoryDir
		}
		if cfg.EnableContextOffload != nil {
			effective.EnableContextOffload = cfg.EnableContextOffload
		}
		if cfg.EnableBootstrapContext != nil {
			effective.EnableBootstrapContext = cfg.EnableBootstrapContext
		}
		if cfg.EnsureGeneralPurpose != nil {
			effective.EnsureGeneralPurpose = cfg.EnsureGeneralPurpose
		}
		if cfg.GeneralPurposeName != "" {
			effective.GeneralPurposeName = cfg.GeneralPurposeName
		}
		if cfg.GeneralPurposePrompt != "" {
			effective.GeneralPurposePrompt = cfg.GeneralPurposePrompt
		}
		if cfg.GeneralPurposeDesc != "" {
			effective.GeneralPurposeDesc = cfg.GeneralPurposeDesc
		}
		if cfg.GeneralPurposeMaxSteps > 0 {
			effective.GeneralPurposeMaxSteps = cfg.GeneralPurposeMaxSteps
		}
		if cfg.EnableWorkspaceIsolation != nil {
			effective.EnableWorkspaceIsolation = cfg.EnableWorkspaceIsolation
		}
		if cfg.IsolationRootDir != "" {
			effective.IsolationRootDir = cfg.IsolationRootDir
		}
		if cfg.EnableDefaultRestrictedPolicy != nil {
			effective.EnableDefaultRestrictedPolicy = cfg.EnableDefaultRestrictedPolicy
		}
		if cfg.EnableDefaultLLMRetry != nil {
			effective.EnableDefaultLLMRetry = cfg.EnableDefaultLLMRetry
		}
		if cfg.LLMRetryConfig != nil {
			effective.LLMRetryConfig = cfg.LLMRetryConfig
		}
		if cfg.LLMBreakerConfig != nil {
			effective.LLMBreakerConfig = cfg.LLMBreakerConfig
		}
		if len(cfg.DefaultSetupOptions) > 0 {
			effective.DefaultSetupOptions = cfg.DefaultSetupOptions
		}
		if len(cfg.AdditionalAppExtensions) > 0 {
			effective.AdditionalAppExtensions = cfg.AdditionalAppExtensions
		}
	}

	var exts []Extension
	exts = append(exts, effective.AdditionalAppExtensions...)

	if valueOrDefault(effective.EnableSessionStore, true) {
		storeDir := effective.SessionStoreDir
		if storeDir == "" {
			if appDir := config.AppDir(); appDir != "" {
				storeDir = filepath.Join(appDir, "sessions")
			} else {
				storeDir = filepath.Join(flags.Workspace, "."+effective.AppName, "sessions")
			}
		}
		store, err := session.NewFileStore(storeDir)
		if err != nil {
			return nil, fmt.Errorf("session store: %w", err)
		}
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
		exts = append(exts, WithKernelOptions(kernel.WithCheckpoints(store)))
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
		exts = append(exts, WithKernelOptions(kernel.WithTaskRuntime(taskRuntime)))
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
	exts = append(exts, WithPlanning())

	if valueOrDefault(effective.EnableBootstrapContext, true) {
		exts = append(exts, WithLoadedBootstrapContext(flags.Workspace, effective.AppName))
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

	if flags.Trust == "restricted" && valueOrDefault(effective.EnableDefaultRestrictedPolicy, true) {
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
