package skill

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// appName 是应用名称，决定全局配置目录（~/.<appName>）。
// 默认为 "moss"，第三方应用可通过 SetAppName 自定义。
// 已废弃：请使用 appkit.SetAppName() 代替。
var appName = "moss"

// SetAppName 设置应用名称，影响全局配置目录路径。
// 必须在任何配置读写操作之前调用。
//
// 已废弃：请使用 appkit.SetAppName() 代替。
func SetAppName(name string) { appName = name }

// AppName 返回当前应用名称。
//
// 已废弃：请使用 appkit.AppName() 代替。
func AppName() string { return appName }

// Config 是 moss.yaml 或 ~/.moss/config.yaml 中的配置。
type Config struct {
	Provider string        `yaml:"provider,omitempty"`
	Model    string        `yaml:"model,omitempty"`
	BaseURL  string        `yaml:"base_url,omitempty"`
	APIKey   string        `yaml:"api_key,omitempty"`
	Skills   []SkillConfig `yaml:"skills,omitempty"`
}

// SkillConfig 描述一个 skill 的加载配置。
type SkillConfig struct {
	Name      string            `yaml:"name"`
	Transport string            `yaml:"transport,omitempty"` // "stdio" | "sse" | ""(built-in)
	Command   string            `yaml:"command,omitempty"`   // stdio transport: 启动命令
	Args      []string          `yaml:"args,omitempty"`      // 命令参数
	URL       string            `yaml:"url,omitempty"`       // sse transport: 服务地址
	Env       map[string]string `yaml:"env,omitempty"`       // 环境变量
	Enabled   *bool             `yaml:"enabled,omitempty"`   // 默认 true
}

// IsEnabled 返回该 skill 是否启用。
func (sc SkillConfig) IsEnabled() bool {
	if sc.Enabled == nil {
		return true
	}
	return *sc.Enabled
}

// IsMCP 返回该 skill 是否为 MCP skill。
func (sc SkillConfig) IsMCP() bool {
	return sc.Transport == "stdio" || sc.Transport == "sse"
}

// LoadConfig 从指定路径加载配置文件。文件不存在时返回空配置。
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

// MergeConfigs 合并多个配置，后面的覆盖前面的同名 skill。
func MergeConfigs(configs ...*Config) *Config {
	seen := make(map[string]int) // name → index in result
	var result []SkillConfig

	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		for _, sc := range cfg.Skills {
			if idx, ok := seen[sc.Name]; ok {
				result[idx] = sc // 覆盖
			} else {
				seen[sc.Name] = len(result)
				result = append(result, sc)
			}
		}
	}

	return &Config{Skills: result}
}

// --- 应用配置相关（内部使用和向后兼容）---

// DefaultGlobalConfigPath 返回全局配置文件路径（~/.<appName>/config.yaml）。
func DefaultGlobalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	d := filepath.Join(home, "."+appName)
	if d == "" {
		return ""
	}
	return filepath.Join(d, "config.yaml")
}

// LoadGlobalConfig 加载全局配置（~/.<appName>/config.yaml）。
func LoadGlobalConfig() (*Config, error) {
	yamlPath := DefaultGlobalConfigPath()
	if yamlPath == "" {
		return &Config{}, nil
	}

	if _, err := os.Stat(yamlPath); err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("stat global config %s: %w", yamlPath, err)
	}

	return LoadConfig(yamlPath)
}

// DefaultProjectConfigPath 返回项目级配置文件路径。
func DefaultProjectConfigPath(workspace string) string {
	return filepath.Join(workspace, "moss.yaml")
}

