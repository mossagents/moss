package appkit

import (
	"context"
	"sort"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/runtime"
)

// DeepAgentConfig describes the configuration for a deep-agent style
// preset. Zero values are filled from DeepAgentDefaults.
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
	AdditionalFeatures            []harness.Feature
}

// ApplyOver returns a new config where every non-zero field in c overrides
// the corresponding field in base.
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
	if len(c.AdditionalFeatures) > 0 {
		base.AdditionalFeatures = c.AdditionalFeatures
	}
	return base
}

// DeepAgentDefaults returns the default deep-agent configuration.
func DeepAgentDefaults() DeepAgentConfig {
	return DeepAgentConfig{
		AppName:                       appconfig.AppName(),
		EnableSessionStore:            deepAgentBoolPtr(true),
		EnableCheckpointStore:         deepAgentBoolPtr(true),
		EnableTaskRuntime:             deepAgentBoolPtr(true),
		EnablePersistentMemories:      deepAgentBoolPtr(true),
		EnableContextOffload:          deepAgentBoolPtr(true),
		EnableBootstrapContext:        deepAgentBoolPtr(true),
		EnsureGeneralPurpose:          deepAgentBoolPtr(true),
		GeneralPurposeName:            "general-purpose",
		GeneralPurposePrompt:          "You are a general-purpose delegated assistant. Complete delegated tasks thoroughly and return concise results.",
		GeneralPurposeDesc:            "General-purpose agent for delegated tasks that need context isolation.",
		GeneralPurposeMaxSteps:        50,
		EnableWorkspaceIsolation:      deepAgentBoolPtr(true),
		EnableDefaultRestrictedPolicy: deepAgentBoolPtr(true),
		EnableDefaultLLMRetry:         deepAgentBoolPtr(true),
	}
}

// BuildDeepAgent builds a deep-agent style kernel by composing declarative
// preset packs and delegating final assembly to BuildKernelWithFeatures.
func BuildDeepAgent(ctx context.Context, flags *AppFlags, uio io.UserIO, cfg *DeepAgentConfig) (*kernel.Kernel, error) {
	effective := DeepAgentDefaults()
	if cfg != nil {
		effective = cfg.ApplyOver(effective)
	}
	features, err := buildDeepAgentFeatures(flags, effective)
	if err != nil {
		return nil, err
	}
	return BuildKernelWithFeatures(ctx, flags, uio, features...)
}

func ensureGeneralPurposeAgent(k *kernel.Kernel, flags *AppFlags, cfg DeepAgentConfig) error {
	agReg := harness.SubagentCatalogOf(k)
	name := cfg.GeneralPurposeName
	if _, exists := agReg.Get(name); exists {
		return nil
	}
	maxSteps := cfg.GeneralPurposeMaxSteps
	if maxSteps <= 0 {
		maxSteps = 50
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

	prompt := cfg.GeneralPurposePrompt
	prompt = appconfig.RenderSystemPromptForTrust(flags.Workspace, flags.Trust, prompt, appconfig.DefaultTemplateContext(flags.Workspace))
	return agReg.Register(harness.SubagentConfig{
		Name:         name,
		Description:  cfg.GeneralPurposeDesc,
		SystemPrompt: prompt,
		Tools:        toolNames,
		MaxSteps:     maxSteps,
		TrustLevel:   trust,
	})
}

func deepAgentBoolPtr(v bool) *bool { return &v }

func deepAgentValueOrDefault(p *bool, def bool) bool {
	if p != nil {
		return *p
	}
	return def
}
