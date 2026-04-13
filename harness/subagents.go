package harness

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/runtime"
	"gopkg.in/yaml.v3"
)

// SubagentConfig aliases the runtime leaf-agent catalog config so callers can
// stay on the canonical harness surface.
type SubagentConfig = agent.AgentConfig

// SubagentCatalog aliases the runtime-backed leaf-agent catalog type.
type SubagentCatalog = agent.Registry

type subagentFileConfig struct {
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Tools        []string `yaml:"tools"`
	MaxSteps     int      `yaml:"max_steps"`
	TrustLevel   string   `yaml:"trust_level"`
}

// NewSubagentCatalog returns an empty leaf-agent catalog.
func NewSubagentCatalog() *SubagentCatalog {
	return agent.NewRegistry()
}

// SubagentCatalogValue returns a Feature that installs a pre-built subagent
// catalog into the runtime-backed delegation layer.
func SubagentCatalogValue(reg *SubagentCatalog) Feature {
	return FeatureFunc{
		FeatureName: "subagent-catalog",
		MetadataValue: FeatureMetadata{
			Key:   "subagent-catalog",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			if reg == nil {
				return fmt.Errorf("subagent catalog must not be nil")
			}
			h.Kernel().Apply(runtime.WithAgentRegistry(reg))
			return nil
		},
	}
}

// SubagentCatalogOf returns the runtime-backed leaf-agent catalog attached to
// the kernel. This is the canonical public accessor for configured subagents.
func SubagentCatalogOf(k *kernel.Kernel) *SubagentCatalog {
	return runtime.AgentRegistry(k)
}

// RegisterSubagent registers a configured leaf subagent into the kernel's
// subagent catalog. Code-defined multi-agent composition should continue to use
// kernel.Agent plus harness/patterns.
func RegisterSubagent(k *kernel.Kernel, cfg SubagentConfig) error {
	reg := SubagentCatalogOf(k)
	if _, exists := reg.Get(cfg.Name); exists {
		return nil
	}
	if err := reg.Register(cfg); err != nil {
		return fmt.Errorf("register subagent %s: %w", cfg.Name, err)
	}
	return nil
}

// LoadSubagentsFromYAML loads leaf subagents from a YAML file into the
// kernel's subagent catalog.
func LoadSubagentsFromYAML(k *kernel.Kernel, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read subagents file: %w", err)
	}

	var defs map[string]subagentFileConfig
	if err := yaml.Unmarshal(data, &defs); err != nil {
		return fmt.Errorf("parse subagents file: %w", err)
	}

	for name, def := range defs {
		cfg := SubagentConfig{
			Name:         name,
			Description:  def.Description,
			SystemPrompt: strings.TrimSpace(def.SystemPrompt),
			Tools:        def.Tools,
			MaxSteps:     def.MaxSteps,
			TrustLevel:   def.TrustLevel,
		}
		if err := RegisterSubagent(k, cfg); err != nil {
			return err
		}
	}
	return nil
}
