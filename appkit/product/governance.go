package product

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mossagents/moss/adapters"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/retry"
)

const defaultLLMBreakerReset = 30 * time.Second

// GovernanceConfig 描述产品层暴露的模型治理配置。
type GovernanceConfig struct {
	RouterConfigPath   string
	PricingCatalogPath string
	LLMRetries         int
	LLMRetryInitial    time.Duration
	LLMRetryMaxDelay   time.Duration
	LLMBreakerFailures int
	LLMBreakerReset    time.Duration
}

type GovernanceReport struct {
	RouterConfig       string `json:"router_config,omitempty"`
	RouterEnabled      bool   `json:"router_enabled"`
	RouterExists       bool   `json:"router_exists"`
	RouterDefaultModel string `json:"router_default_model,omitempty"`
	RouterModels       int    `json:"router_models"`
	PricingCatalog     string `json:"pricing_catalog,omitempty"`
	PricingExists      bool   `json:"pricing_exists"`
	PricingModels      int    `json:"pricing_models"`
	PricingError       string `json:"pricing_error,omitempty"`
	RetryEnabled       bool   `json:"retry_enabled"`
	RetryMaxRetries    int    `json:"retry_max_retries"`
	RetryInitialDelay  string `json:"retry_initial_delay,omitempty"`
	RetryMaxDelay      string `json:"retry_max_delay,omitempty"`
	BreakerEnabled     bool   `json:"breaker_enabled"`
	BreakerMaxFailures int    `json:"breaker_max_failures"`
	BreakerResetAfter  string `json:"breaker_reset_after,omitempty"`
	Error              string `json:"error,omitempty"`
}

func DefaultGovernanceConfig() GovernanceConfig {
	return GovernanceConfig{
		LLMRetries:       2,
		LLMRetryInitial:  300 * time.Millisecond,
		LLMRetryMaxDelay: 2 * time.Second,
	}
}

func (c GovernanceConfig) RetryConfig() (*retry.Config, *bool) {
	defaults := DefaultGovernanceConfig()
	if c.LLMRetries < 0 {
		return nil, nil
	}
	if c.LLMRetries == 0 {
		disabled := false
		return nil, &disabled
	}
	enabled := true
	return &retry.Config{
		MaxRetries:   c.LLMRetries,
		InitialDelay: durationOrDefault(c.LLMRetryInitial, defaults.LLMRetryInitial),
		MaxDelay:     durationOrDefault(c.LLMRetryMaxDelay, defaults.LLMRetryMaxDelay),
		Multiplier:   2.0,
	}, &enabled
}

func (c GovernanceConfig) BreakerConfig() *retry.BreakerConfig {
	if c.LLMBreakerFailures <= 0 {
		return nil
	}
	resetAfter := c.LLMBreakerReset
	if resetAfter <= 0 {
		resetAfter = defaultLLMBreakerReset
	}
	return &retry.BreakerConfig{
		MaxFailures: c.LLMBreakerFailures,
		ResetAfter:  resetAfter,
	}
}

func ResolveRouterConfigPath(workspace, explicit string) string {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit
	}
	for _, candidate := range routerConfigCandidates(workspace) {
		if pathExists(candidate) {
			return candidate
		}
	}
	return ""
}

func OpenModelRouter(workspace, explicit string) (*adapters.ModelRouter, string, error) {
	path := ResolveRouterConfigPath(workspace, explicit)
	if strings.TrimSpace(path) == "" {
		return nil, "", nil
	}
	router, err := adapters.NewModelRouterFromFile(path)
	if err != nil {
		return nil, path, err
	}
	return router, path, nil
}

func BuildGovernanceReport(workspace string, cfg GovernanceConfig) GovernanceReport {
	report := GovernanceReport{
		RetryEnabled:    cfg.LLMRetries > 0,
		RetryMaxRetries: cfg.LLMRetries,
		RetryInitialDelay: func() string {
			if cfg.LLMRetries <= 0 {
				return ""
			}
			value := durationOrDefault(cfg.LLMRetryInitial, DefaultGovernanceConfig().LLMRetryInitial)
			if value <= 0 {
				return ""
			}
			return value.String()
		}(),
		RetryMaxDelay: func() string {
			if cfg.LLMRetries <= 0 {
				return ""
			}
			value := durationOrDefault(cfg.LLMRetryMaxDelay, DefaultGovernanceConfig().LLMRetryMaxDelay)
			if value <= 0 {
				return ""
			}
			return value.String()
		}(),
		BreakerEnabled:     cfg.LLMBreakerFailures > 0,
		BreakerMaxFailures: cfg.LLMBreakerFailures,
	}
	if cfg.LLMBreakerReset > 0 {
		report.BreakerResetAfter = cfg.LLMBreakerReset.String()
	} else if report.BreakerEnabled {
		report.BreakerResetAfter = defaultLLMBreakerReset.String()
	}

	pricingCatalog, pricingPath, pricingErr := OpenPricingCatalog(workspace, cfg.PricingCatalogPath)
	if pricingPath != "" {
		report.PricingCatalog = pricingPath
		report.PricingExists = pathExists(pricingPath)
	}
	if pricingErr != nil {
		report.PricingError = pricingErr.Error()
	} else if pricingCatalog != nil {
		report.PricingModels = len(pricingCatalog.Models)
	}

	path := ResolveRouterConfigPath(workspace, cfg.RouterConfigPath)
	if path == "" {
		return report
	}
	report.RouterConfig = path
	report.RouterExists = pathExists(path)
	if !report.RouterExists {
		report.Error = fmt.Sprintf("router config not found: %s", path)
		return report
	}

	router, err := adapters.NewModelRouterFromFile(path)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.RouterEnabled = true
	report.RouterDefaultModel = router.DefaultModel()
	report.RouterModels = len(router.Models())
	return report
}

func routerConfigCandidates(workspace string) []string {
	candidates := make([]string, 0, 3)
	workspace = strings.TrimSpace(workspace)
	if workspace != "" {
		candidates = append(candidates,
			filepath.Join(workspace, ".mosscode", "models.yaml"),
			filepath.Join(workspace, ".moss", "models.yaml"),
		)
	}
	candidates = append(candidates, filepath.Join(appconfig.AppDir(), "models.yaml"))
	return candidates
}

func durationOrDefault(value, def time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return def
}
