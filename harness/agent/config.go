// Package agent 实现 Agent 注册表与委派机制。
//
// Agent 通过 YAML 配置文件定义，每个 Agent 包含名称、系统提示、
// 可用工具白名单等属性。支持同步委派 (delegate_agent) 和
// 异步委派 (spawn_agent + query_agent)。
package agent

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
)

// AgentConfig 描述一个 Agent 的配置。
type AgentConfig struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Tools        []string `yaml:"tools"`
	MaxSteps     int      `yaml:"max_steps"`
	TrustLevel   string   `yaml:"trust_level"`
}

func (c *AgentConfig) validate() error {
	if c.Name == "" {
		return fmt.Errorf("agent config: name is required")
	}
	if c.SystemPrompt == "" {
		return fmt.Errorf("agent %q: system_prompt is required", c.Name)
	}
	if c.MaxSteps <= 0 {
		c.MaxSteps = 30
	}
	if c.TrustLevel == "" {
		c.TrustLevel = "restricted"
	}
	return nil
}

// ParseConfigFile 从 YAML 文件解析 AgentConfig。
func ParseConfigFile(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadConfigsFromDir 从目录中加载所有 .yaml/.yml Agent 配置文件。
func LoadConfigsFromDir(dir string) ([]AgentConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var configs []AgentConfig
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		cfg, err := ParseConfigFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", e.Name(), err)
		}
		configs = append(configs, *cfg)
	}
	return configs, nil
}
