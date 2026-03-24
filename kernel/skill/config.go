package skill

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// appName 是应用名称，决定全局配置目录（~/.<appName>）。
// 默认为 "moss"，第三方应用可通过 SetAppName 自定义。
var appName = "moss"

// SetAppName 设置应用名称，影响全局配置目录路径。
// 必须在任何配置读写操作之前调用。
//
// 示例：
//
//	skill.SetAppName("minicode") // 配置目录变为 ~/.minicode
func SetAppName(name string) { appName = name }

// AppName 返回当前应用名称。
func AppName() string { return appName }

const defaultConfigTemplate = `# Global config for moss
# Priority: CLI flags > config file > environment variables

# provider: openai
# model: gpt-4o
# base_url: ""
# api_key: ""

skills:
	# Example MCP skill via stdio
	# - name: my-mcp-server
	#   transport: stdio
	#   command: npx
	#   args: ["-y", "@example/mcp-server"]

	# Example MCP skill via SSE
	# - name: remote-mcp
	#   transport: sse
	#   url: http://localhost:3000/sse
`

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

// DefaultGlobalConfigPath 返回全局配置文件路径（~/.<appName>/config.yaml）。
func DefaultGlobalConfigPath() string {
	d := MossDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "config.yaml")
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

// MossDir 返回全局配置目录路径（~/.<appName>）。
func MossDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "."+appName)
}

// EnsureMossDir 确保 ~/.<appName> 目录存在，不存在则创建。
// 同时会在全局配置文件不存在时创建一个可编辑的模板文件。
func EnsureMossDir() error {
	dir := MossDir()
	if dir == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	cfgPath := DefaultGlobalConfigPath()
	if cfgPath == "" {
		return nil
	}

	f, err := os.OpenFile(cfgPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("create config template %s: %w", cfgPath, err)
	}
	defer f.Close()

	if _, err := f.WriteString(defaultConfigTemplate); err != nil {
		return fmt.Errorf("write config template %s: %w", cfgPath, err)
	}

	return nil
}
