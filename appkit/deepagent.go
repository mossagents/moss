package appkit

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/mossagents/moss/agent"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/extensions/agentsx"
	"github.com/mossagents/moss/extensions/defaults"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

// DeepAgentConfig 描述 deep-agent 风格装配的配置项。
// 零值通过 BuildDeepAgentKernel 的默认值补齐。
type DeepAgentConfig struct {
	AppName                  string
	EnableSessionStore       *bool
	SessionStoreDir          string
	EnablePersistentMemories *bool
	MemoryDir                string
	EnableContextOffload     *bool
	EnableBootstrapContext   *bool
	EnsureGeneralPurpose     *bool
	GeneralPurposeName       string
	GeneralPurposePrompt     string
	GeneralPurposeDesc       string
	GeneralPurposeMaxSteps   int
	DefaultSetupOptions      []defaults.Option
	AdditionalAppExtensions  []Extension
}

// DefaultDeepAgentConfig 返回 deep-agent 装配默认配置。
func DefaultDeepAgentConfig() DeepAgentConfig {
	return DeepAgentConfig{
		AppName:                  appconfig.AppName(),
		EnableSessionStore:       boolPtr(true),
		EnablePersistentMemories: boolPtr(true),
		EnableContextOffload:     boolPtr(true),
		EnableBootstrapContext:   boolPtr(true),
		EnsureGeneralPurpose:     boolPtr(true),
		GeneralPurposeName:       "general-purpose",
		GeneralPurposePrompt:     "You are a general-purpose delegated assistant. Complete delegated tasks thoroughly and return concise results.",
		GeneralPurposeDesc:       "General-purpose agent for delegated tasks that need context isolation.",
		GeneralPurposeMaxSteps:   50,
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
			if appDir := appconfig.AppDir(); appDir != "" {
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
		}
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
		exts = append(exts, WithPersistentMemories(memDir))
	}

	if valueOrDefault(effective.EnableBootstrapContext, true) {
		exts = append(exts, WithLoadedBootstrapContext(flags.Workspace, effective.AppName))
	}

	k, err := BuildKernelWithConfig(ctx, flags, io, BuildConfig{
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

	if flags.Trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor(
				"write_file", "edit_file", "run_command", "spawn_agent", "task",
				"write_memory", "delete_memory", "offload_context",
			),
			builtins.DefaultAllow(),
		)
	}

	return k, nil
}

func ensureGeneralPurposeAgent(k *kernel.Kernel, flags *AppFlags, cfg DeepAgentConfig) error {
	reg := agentsx.Registry(k)
	if _, ok := reg.Get(cfg.GeneralPurposeName); ok {
		return nil
	}

	toolSpecs := k.ToolRegistry().List()
	toolNames := make([]string, 0, len(toolSpecs))
	for _, spec := range toolSpecs {
		switch spec.Name {
		case "delegate_agent", "spawn_agent", "query_agent", "task":
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
