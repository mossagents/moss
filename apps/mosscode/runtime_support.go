package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/harness/appkit"
	"github.com/mossagents/moss/harness/appkit/product"
	"github.com/mossagents/moss/harness/appkit/product/changes"
	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/logging"
	providers "github.com/mossagents/moss/harness/providers"
	rsessionspec "github.com/mossagents/moss/harness/runtime/sessionspec"
	"github.com/mossagents/moss/harness/sandbox"
	"github.com/mossagents/moss/harness/userio/prompting"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

//go:embed templates/system_prompt.tmpl
var defaultPromptInstructionsTemplate string

func initializeCommandRuntime(cfg *config) (func(), error) {
	auditObserver, auditCloser, err := product.OpenAuditObserver()
	if err != nil {
		return nil, fmt.Errorf("initialize audit log: %w", err)
	}
	pricingCatalog, _, err := product.OpenPricingCatalog(cfg.flags.Workspace, cfg.governance.PricingCatalogPath)
	if err != nil {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return nil, fmt.Errorf("load pricing catalog: %w", err)
	}
	cfg.pricingCatalog = pricingCatalog
	cfg.observer = product.NewPricingObserver(pricingCatalog, auditObserver)
	return func() {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
	}, nil
}

func buildCheckpointKernel(ctx context.Context, cfg *config) (*kernel.Kernel, error) {
	invocation, err := resolveRuntimeInvocation(cfg, "interactive")
	if err != nil {
		return nil, err
	}
	return buildKernel(ctx, cloneAppFlags(invocation.CompatFlags), &io.NoOpIO{}, invocation.ApprovalMode, cfg.governance, cfg.observer)
}

func buildChangeRuntime(ctx context.Context, cfg *config, sessionID string) (changes.ChangeRuntime, func(), error) {
	rt := changes.ChangeRuntime{
		Workspace:        cfg.flags.Workspace,
		RepoStateCapture: sandbox.NewGitRepoStateCapture(cfg.flags.Workspace),
		PatchApply:       sandbox.NewGitPatchApply(cfg.flags.Workspace),
		PatchRevert:      sandbox.NewGitPatchRevert(cfg.flags.Workspace),
	}
	if strings.TrimSpace(sessionID) == "" {
		return rt, func() {}, nil
	}
	k, err := buildCheckpointKernel(ctx, cfg)
	if err != nil {
		return changes.ChangeRuntime{}, nil, err
	}
	if err := k.Boot(ctx); err != nil {
		return changes.ChangeRuntime{}, nil, err
	}
	return changes.ChangeRuntimeFromKernel(cfg.flags.Workspace, k), func() {
		_ = k.Shutdown(ctx)
	}, nil
}

func buildKernel(ctx context.Context, flags *appkit.AppFlags, io io.UserIO, approvalMode string, governance product.GovernanceConfig, observer observe.Observer) (*kernel.Kernel, error) {
	logging.GetLogger().DebugContext(ctx, "build kernel requested",
		"workspace", flags.Workspace,
		"profile", flags.Profile,
		"trust", flags.Trust,
		"approval_mode", approvalMode,
	)
	resolved, err := resolveProfileForFlags(flags, approvalMode)
	if err != nil {
		return nil, err
	}
	flags.Trust = resolved.Trust
	flags.Profile = resolved.Name
	disableDefaultPolicy := false
	router, _, err := product.OpenModelRouter(flags.Workspace, governance.RouterConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load model router: %w", err)
	}
	failoverCfg, failoverEnabled := governance.FailoverConfig()
	useFailover := failoverEnabled && router != nil && len(router.Models()) > 1
	retryCfg, retryEnabled := governance.RetryConfig()
	breakerCfg := governance.BreakerConfig()
	if useFailover {
		disabled := false
		retryEnabled = &disabled
		retryCfg = nil
		breakerCfg = nil
	}
	k, err := appkit.BuildDeepAgent(ctx, flags, io, &appkit.DeepAgentConfig{
		AppName:                       appName,
		EnableDefaultRestrictedPolicy: &disableDefaultPolicy,
		EnableDefaultLLMRetry:         retryEnabled,
		LLMRetryConfig:                retryCfg,
		LLMBreakerConfig:              breakerCfg,
		AdditionalFeatures:            []harness.Feature{},
	})
	if err != nil {
		return nil, err
	}
	if err := configureContextPolicy(k, flags); err != nil {
		return nil, err
	}
	if err := k.Apply(kernel.WithParallelToolCalls()); err != nil {
		return nil, err
	}
	logging.GetLogger().DebugContext(ctx, "kernel built",
		"profile", resolved.Name,
		"trust", resolved.Trust,
		"workspace", flags.Workspace,
		"router_enabled", router != nil,
		"failover_enabled", useFailover,
	)
	if router != nil {
		var llm model.LLM = router
		if useFailover {
			failoverLLM, err := providers.NewFailoverLLM(router, failoverCfg)
			if err != nil {
				return nil, fmt.Errorf("build failover llm: %w", err)
			}
			llm = failoverLLM
		}
		k.SetLLM(llm)
	}
	k.SetObserver(product.ComposeStateObserver(k, observer))
	if err := product.ApplyToolPolicy(k, resolved.ToolPolicy); err != nil {
		return nil, err
	}
	return k, nil
}

func resolveProfileForFlags(flags *appkit.AppFlags, approvalMode string) (rsessionspec.LegacyRuntimeSelection, error) {
	return rsessionspec.ResolveLegacyRuntimeSelection(rsessionspec.LegacyResolveOptions{
		Workspace:        flags.Workspace,
		RequestedProfile: flags.Profile,
		Trust:            flags.Trust,
		ApprovalMode:     approvalMode,
	})
}

func resolveProfileForConfig(cfg *config) (rsessionspec.LegacyRuntimeSelection, error) {
	trust := ""
	if hasExplicitFlag(cfg.explicitFlags, "trust") || envConfigured("MOSSCODE_TRUST", "MOSS_TRUST") {
		trust = cfg.flags.Trust
	}
	approval := ""
	if hasExplicitFlag(cfg.explicitFlags, "approval") || envConfigured("MOSSCODE_APPROVAL_MODE", "MOSS_APPROVAL_MODE") {
		approval = cfg.approvalMode
	}
	return rsessionspec.ResolveLegacyRuntimeSelection(rsessionspec.LegacyResolveOptions{
		Workspace:        cfg.flags.Workspace,
		RequestedProfile: cfg.flags.Profile,
		Trust:            trust,
		ApprovalMode:     approval,
	})
}

func hasExplicitFlag(explicit []string, name string) bool {
	for _, item := range explicit {
		if item == name {
			return true
		}
	}
	return false
}

func envConfigured(keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func buildProductPromptInstructions(workspace, trust string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	if prefs, err := product.LoadTUIConfig(); err == nil {
		ctx["Personality"] = product.NormalizePersonality(prefs.Personality)
		ctx["FastMode"] = prefs.FastMode != nil && *prefs.FastMode
	}
	return appconfig.RenderSystemPromptForTrust(workspace, trust, defaultPromptInstructionsTemplate, ctx)
}

func composeProductSystemPrompt(workspace, trust string, k *kernel.Kernel, cfg session.SessionConfig) (string, map[string]any, error) {
	promptMetadata := make(map[string]any, len(cfg.Metadata)+2)
	for key, value := range cfg.Metadata {
		promptMetadata[key] = value
	}
	promptCfg := cfg
	promptCfg.Metadata = promptMetadata
	if promptCfg.Profile == "" {
		promptCfg.Profile = strings.TrimSpace(cfg.Profile)
	}
	if profile := strings.TrimSpace(promptCfg.Profile); profile != "" {
		promptMetadata[prompting.MetadataProfileNameKey] = profile
	}
	if taskMode := firstNonEmptyTrimmed(metadataString(promptMetadata, session.MetadataTaskMode), sessionConfigCollaborationMode(cfg), strings.TrimSpace(promptCfg.Profile)); taskMode != "" {
		promptMetadata[session.MetadataTaskMode] = taskMode
	}
	systemPrompt, promptMetadata, err := prompting.ComposeSystemPromptForConfig(
		workspace,
		trust,
		k,
		buildProductPromptInstructions(workspace, trust),
		"",
		promptCfg,
	)
	if err != nil {
		return "", nil, err
	}
	return systemPrompt, promptMetadata, nil
}

func buildLegacyProjectedSessionConfig(base session.SessionConfig, flags *appkit.AppFlags, trust, approvalMode, profile, promptPack string) session.SessionConfig {
	selection, err := rsessionspec.ResolveLegacyRuntimeSelection(rsessionspec.LegacyResolveOptions{
		Workspace:        flags.Workspace,
		RequestedProfile: profile,
		Trust:            trust,
		ApprovalMode:     approvalMode,
	})
	if err != nil {
		selection = rsessionspec.DefaultLegacyRuntimeSelection(flags.Workspace, profile, trust, approvalMode)
	}
	cfg := applyContextPolicy(base, flags)
	projected, err := rsessionspec.ApplyLegacyRuntimeSelection(cfg, selection, rsessionspec.LegacyProjectionInput{
		PromptPack: firstNonEmptyTrimmed(promptPack, "coding"),
		Provider:   flags.Provider,
		ModelName:  flags.Model,
	})
	if err != nil {
		return cfg
	}
	return projected
}

func sessionConfigCollaborationMode(cfg session.SessionConfig) string {
	if cfg.ResolvedSessionSpec != nil && strings.TrimSpace(cfg.ResolvedSessionSpec.Intent.CollaborationMode) != "" {
		return strings.TrimSpace(cfg.ResolvedSessionSpec.Intent.CollaborationMode)
	}
	if cfg.SessionSpec != nil && strings.TrimSpace(cfg.SessionSpec.Intent.CollaborationMode) != "" {
		return strings.TrimSpace(cfg.SessionSpec.Intent.CollaborationMode)
	}
	return ""
}

func metadataString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func effectiveFlags() *appkit.AppFlags {
	f := &appkit.AppFlags{}
	_ = appkit.InitializeApp(appName, f, "MOSSCODE", "MOSS")
	return f
}

func applyGovernanceEnv(cfg *product.GovernanceConfig, explicitFlags []string) {
	explicit := make(map[string]struct{}, len(explicitFlags))
	for _, name := range explicitFlags {
		explicit[name] = struct{}{}
	}
	if _, ok := explicit["router-config"]; !ok {
		cfg.RouterConfigPath = firstEnv(cfg.RouterConfigPath, "MOSSCODE_ROUTER_CONFIG", "MOSS_ROUTER_CONFIG")
	}
	if _, ok := explicit["llm-retries"]; !ok {
		cfg.LLMRetries = firstEnvInt(cfg.LLMRetries, "MOSSCODE_LLM_RETRIES", "MOSS_LLM_RETRIES")
	}
	if _, ok := explicit["llm-retry-initial"]; !ok {
		cfg.LLMRetryInitial = firstEnvDuration(cfg.LLMRetryInitial, "MOSSCODE_LLM_RETRY_INITIAL", "MOSS_LLM_RETRY_INITIAL")
	}
	if _, ok := explicit["llm-retry-max-delay"]; !ok {
		cfg.LLMRetryMaxDelay = firstEnvDuration(cfg.LLMRetryMaxDelay, "MOSSCODE_LLM_RETRY_MAX_DELAY", "MOSS_LLM_RETRY_MAX_DELAY")
	}
	if _, ok := explicit["llm-breaker-failures"]; !ok {
		cfg.LLMBreakerFailures = firstEnvInt(cfg.LLMBreakerFailures, "MOSSCODE_LLM_BREAKER_FAILURES", "MOSS_LLM_BREAKER_FAILURES")
	}
	if _, ok := explicit["llm-breaker-reset"]; !ok {
		cfg.LLMBreakerReset = firstEnvDuration(cfg.LLMBreakerReset, "MOSSCODE_LLM_BREAKER_RESET", "MOSS_LLM_BREAKER_RESET")
	}
	if _, ok := explicit["llm-failover"]; !ok {
		cfg.LLMFailoverEnabled = firstEnvBool(cfg.LLMFailoverEnabled, "MOSSCODE_LLM_FAILOVER", "MOSS_LLM_FAILOVER")
	}
	if _, ok := explicit["llm-failover-max-candidates"]; !ok {
		cfg.LLMFailoverMaxCandidates = firstEnvInt(cfg.LLMFailoverMaxCandidates, "MOSSCODE_LLM_FAILOVER_MAX_CANDIDATES", "MOSS_LLM_FAILOVER_MAX_CANDIDATES")
	}
	if _, ok := explicit["llm-failover-retries"]; !ok {
		cfg.LLMFailoverPerCandidateRetries = firstEnvInt(cfg.LLMFailoverPerCandidateRetries, "MOSSCODE_LLM_FAILOVER_RETRIES", "MOSS_LLM_FAILOVER_RETRIES")
	}
	if _, ok := explicit["llm-failover-on-breaker-open"]; !ok {
		cfg.LLMFailoverOnBreakerOpen = firstEnvBool(cfg.LLMFailoverOnBreakerOpen, "MOSSCODE_LLM_FAILOVER_ON_BREAKER_OPEN", "MOSS_LLM_FAILOVER_ON_BREAKER_OPEN")
	}
}

func firstEnv(def string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return def
}

func firstEnvInt(def int, keys ...string) int {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
		fmt.Fprintf(os.Stderr, "warning: ignore invalid %s=%q\n", key, value)
	}
	return def
}

func firstEnvDuration(def time.Duration, keys ...string) time.Duration {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parsed, err := time.ParseDuration(value)
		if err == nil {
			return parsed
		}
		fmt.Fprintf(os.Stderr, "warning: ignore invalid %s=%q\n", key, value)
	}
	return def
}

func firstEnvBool(def bool, keys ...string) bool {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
		fmt.Fprintf(os.Stderr, "warning: ignore invalid %s=%q\n", key, value)
	}
	return def
}
