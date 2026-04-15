package product

import (
	"fmt"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/model"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
)

type PricingCatalog struct {
	Models map[string]ModelPricing `yaml:"models" json:"models"`
}

type ModelPricing struct {
	PromptPer1MUSD     float64 `yaml:"prompt_per_1m_usd" json:"prompt_per_1m_usd"`
	CompletionPer1MUSD float64 `yaml:"completion_per_1m_usd" json:"completion_per_1m_usd"`
}

func ResolvePricingCatalogPath(workspace, explicit string) string {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit
	}
	for _, candidate := range PricingCatalogCandidates(workspace) {
		if pathExists(candidate) {
			return candidate
		}
	}
	return ""
}

func OpenPricingCatalog(workspace, explicit string) (*PricingCatalog, string, error) {
	path := ResolvePricingCatalogPath(workspace, explicit)
	if path == "" {
		return nil, "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path, fmt.Errorf("read pricing catalog: %w", err)
	}
	var catalog PricingCatalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, path, fmt.Errorf("parse pricing catalog: %w", err)
	}
	if catalog.Models == nil {
		catalog.Models = map[string]ModelPricing{}
	}
	return &catalog, path, nil
}

func (c *PricingCatalog) Estimate(usage model.TokenUsage, model string) (float64, bool) {
	if c == nil || len(c.Models) == 0 {
		return 0, false
	}
	price, ok := c.Models[strings.TrimSpace(model)]
	if !ok {
		return 0, false
	}
	cost := (float64(usage.PromptTokens) / 1_000_000.0 * price.PromptPer1MUSD) +
		(float64(usage.CompletionTokens) / 1_000_000.0 * price.CompletionPer1MUSD)
	return cost, true
}

func PricingCatalogCandidates(workspace string) []string {
	candidates := make([]string, 0, 3)
	workspace = strings.TrimSpace(workspace)
	if workspace != "" {
		candidates = append(candidates,
			filepath.Join(workspace, ".mosscode", "pricing.yaml"),
			filepath.Join(workspace, ".moss", "pricing.yaml"),
		)
	}
	candidates = append(candidates, filepath.Join(appconfig.AppDir(), "pricing.yaml"))
	return candidates
}
