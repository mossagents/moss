package runtime

import (
	"fmt"
	"os"
	"strings"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/kernel"
	"gopkg.in/yaml.v3"
)

type SubagentFileConfig struct {
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Tools        []string `yaml:"tools"`
	MaxSteps     int      `yaml:"max_steps"`
	TrustLevel   string   `yaml:"trust_level"`
}

func RegisterSubagent(k *kernel.Kernel, cfg agent.AgentConfig) error {
	reg := AgentRegistry(k)
	if _, exists := reg.Get(cfg.Name); exists {
		return nil
	}
	if err := reg.Register(cfg); err != nil {
		return fmt.Errorf("register subagent %s: %w", cfg.Name, err)
	}
	return nil
}

func LoadSubagentsFromYAML(k *kernel.Kernel, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read subagents file: %w", err)
	}

	var defs map[string]SubagentFileConfig
	if err := yaml.Unmarshal(data, &defs); err != nil {
		return fmt.Errorf("parse subagents file: %w", err)
	}

	for name, def := range defs {
		cfg := agent.AgentConfig{
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
