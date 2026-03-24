package skill

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

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

// DefaultGlobalConfigPath 返回全局配置文件路径。
func DefaultGlobalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".moss", "config.yaml")
}

// DefaultProjectConfigPath 返回项目级配置文件路径。
func DefaultProjectConfigPath(workspace string) string {
	return filepath.Join(workspace, "moss.yaml")
}

// SaveConfig 将配置写入指定路径，自动创建父目录。
func SaveConfig(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// MossDir 返回 ~/.moss 目录路径。
func MossDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".moss")
}

// EnsureMossDir 确保 ~/.moss 目录存在，不存在则创建。
func EnsureMossDir() error {
	dir := MossDir()
	if dir == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	return os.MkdirAll(dir, 0700)
}
