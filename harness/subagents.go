package harness

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mossagents/moss/harness/agent"
	"github.com/mossagents/moss/kernel"
	"gopkg.in/yaml.v3"
)

// SubagentConfig is the harness-owned public config for a leaf subagent.
type SubagentConfig struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Tools        []string `yaml:"tools"`
	MaxSteps     int      `yaml:"max_steps"`
	TrustLevel   string   `yaml:"trust_level"`
}

func (c SubagentConfig) agentConfig() agent.AgentConfig {
	return agent.AgentConfig{
		Name:         c.Name,
		Description:  c.Description,
		SystemPrompt: c.SystemPrompt,
		Tools:        append([]string(nil), c.Tools...),
		MaxSteps:     c.MaxSteps,
		TrustLevel:   c.TrustLevel,
	}
}

func subagentConfigFromAgent(cfg agent.AgentConfig) SubagentConfig {
	return SubagentConfig{
		Name:         cfg.Name,
		Description:  cfg.Description,
		SystemPrompt: cfg.SystemPrompt,
		Tools:        append([]string(nil), cfg.Tools...),
		MaxSteps:     cfg.MaxSteps,
		TrustLevel:   cfg.TrustLevel,
	}
}

// SubagentCatalog is the harness-owned public facade over the agent-backed
// leaf-agent registry.
type SubagentCatalog struct {
	inner *agent.Registry
}

func wrapSubagentCatalog(reg *agent.Registry) *SubagentCatalog {
	if reg == nil {
		return nil
	}
	return &SubagentCatalog{inner: reg}
}

func (c *SubagentCatalog) agentRegistry() *agent.Registry {
	if c == nil {
		return nil
	}
	return c.inner
}

func (c *SubagentCatalog) Register(cfg SubagentConfig) error {
	if c == nil || c.inner == nil {
		return fmt.Errorf("subagent catalog must not be nil")
	}
	return c.inner.Register(cfg.agentConfig())
}

func (c *SubagentCatalog) Get(name string) (SubagentConfig, bool) {
	if c == nil || c.inner == nil {
		return SubagentConfig{}, false
	}
	cfg, ok := c.inner.Get(name)
	if !ok {
		return SubagentConfig{}, false
	}
	return subagentConfigFromAgent(cfg), true
}

func (c *SubagentCatalog) List() []SubagentConfig {
	if c == nil || c.inner == nil {
		return nil
	}
	items := c.inner.List()
	out := make([]SubagentConfig, 0, len(items))
	for _, item := range items {
		out = append(out, subagentConfigFromAgent(item))
	}
	return out
}

type subagentFileConfig struct {
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Tools        []string `yaml:"tools"`
	MaxSteps     int      `yaml:"max_steps"`
	TrustLevel   string   `yaml:"trust_level"`
}

// NewSubagentCatalog returns an empty leaf-agent catalog.
func NewSubagentCatalog() *SubagentCatalog {
	return wrapSubagentCatalog(agent.NewRegistry())
}

// SubagentCatalogValue returns a Feature that installs a pre-built subagent
// catalog into the agent-owned delegation substrate.
func SubagentCatalogValue(reg *SubagentCatalog) Feature {
	return FeatureFunc{
		FeatureName: "subagent-catalog",
		MetadataValue: FeatureMetadata{
			Key:   "subagent-catalog",
			Phase: FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *Harness) error {
			if reg == nil || reg.agentRegistry() == nil {
				return fmt.Errorf("subagent catalog must not be nil")
			}
			if err := agent.SetKernelRegistry(h.Kernel(), reg.agentRegistry()); err != nil {
				return err
			}
			return agent.EnsureKernelDelegation(h.Kernel())
		},
	}
}

// SubagentCatalogOf returns the agent-backed leaf-agent catalog attached to
// the kernel. This is the canonical public accessor for configured subagents.
func SubagentCatalogOf(k *kernel.Kernel) *SubagentCatalog {
	return wrapSubagentCatalog(agent.KernelRegistry(k))
}

// RegisterSubagent registers a configured leaf subagent into the kernel's
// subagent catalog. Code-defined multi-agent composition should continue to use
// kernel.Agent plus harness/patterns.
func RegisterSubagent(k *kernel.Kernel, cfg SubagentConfig) error {
	reg := SubagentCatalogOf(k)
	if reg == nil {
		return fmt.Errorf("subagent catalog is unavailable")
	}
	if _, exists := reg.Get(cfg.Name); exists {
		return nil
	}
	if err := reg.Register(cfg); err != nil {
		return fmt.Errorf("register subagent %s: %w", cfg.Name, err)
	}
	return agent.EnsureKernelDelegation(k)
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
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("subagent name is empty in %s", path)
		}
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
