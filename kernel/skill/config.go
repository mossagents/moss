package skill

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// appName 是应用名称，决定全局配置目录（~/.<appName>）。
// 默认为 "moss"，第三方应用可通过 SetAppName 自定义。
// 注意：SetAppName 现已迁移到 appkit 包，请使用 appkit.SetAppName()。
var appName = "moss"

// SetAppName 设置应用名称，影响全局配置目录路径。
// 必须在任何配置读写操作之前调用。
//
// 已废弃：请使用 appkit.SetAppName() 代替。
//
// 示例：
//
//	skill.SetAppName("minicode") // 配置目录变为 ~/.minicode
func SetAppName(name string) { appName = name }

// AppName 返回当前应用名称。
//
// 已废弃：请使用 appkit.AppName() 代替，但此函数仍可用于兼容。
func AppName() string { return appName }

// _mossDir 内部函数，返回全局配置目录路径（~/.<appName>）。
// 仅用于 skill 包内部的配置路径构造。
func _mossDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "."+appName)
}

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

// DefaultConfigTemplate 返回全局配置文件的默认模板。
func DefaultConfigTemplate() string {
	return defaultConfigTemplate
}

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

// DefaultGlobalConfigPath 返回全局配置文件路径（~/.<appName>/config.yaml）。
func DefaultGlobalConfigPath() string {
	d := _mossDir()
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

// DefaultGlobalSystemPromptTemplatePath 返回全局 system prompt 模板路径（~/.<appName>/system_prompt.tmpl）。
func DefaultGlobalSystemPromptTemplatePath() string {
	d := _mossDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "system_prompt.tmpl")
}

// DefaultProjectSystemPromptTemplatePath 返回项目级 system prompt 模板路径（./.<appName>/system_prompt.tmpl）。
func DefaultProjectSystemPromptTemplatePath(workspace string) string {
	return filepath.Join(workspace, "."+AppName(), "system_prompt.tmpl")
}

// LoadSystemPromptTemplate 加载可选 system prompt 模板。
// 优先级：项目级 > 全局级。
func LoadSystemPromptTemplate(workspace string) (string, error) {
	projectPath := DefaultProjectSystemPromptTemplatePath(workspace)
	if projectPath != "" {
		if data, err := os.ReadFile(projectPath); err == nil {
			return string(data), nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("read system prompt template %s: %w", projectPath, err)
		}
	}

	globalPath := DefaultGlobalSystemPromptTemplatePath()
	if globalPath != "" {
		if data, err := os.ReadFile(globalPath); err == nil {
			return string(data), nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("read system prompt template %s: %w", globalPath, err)
		}
	}

	return "", nil
}

// RenderSystemPrompt 渲染 system prompt。
// 若存在项目/全局模板则覆盖 defaultTemplate；渲染失败时回退到 defaultTemplate 渲染结果。
func RenderSystemPrompt(workspace, defaultTemplate string, data map[string]any) string {
	tplSrc := defaultTemplate
	if loaded, err := LoadSystemPromptTemplate(workspace); err == nil && strings.TrimSpace(loaded) != "" {
		tplSrc = loaded
	}

	if rendered, err := renderPromptTemplate(tplSrc, data); err == nil {
		return rendered
	}

	if rendered, err := renderPromptTemplate(defaultTemplate, data); err == nil {
		return rendered
	}

	return defaultTemplate
}

func renderPromptTemplate(src string, data map[string]any) (string, error) {
	tpl, err := template.New("system_prompt").Parse(src)
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	if err := tpl.Execute(&b, data); err != nil {
		return "", err
	}

	return b.String(), nil
}

// 以下函数已移到 appkit 包，请使用以下替代方案：
//
// SaveConfig 已移到 appkit.SaveConfig()
// MossDir 已移到 appkit.MossDir()
// EnsureMossDir 已移到 appkit.EnsureMossDir()
//
// 为了向后兼容性，以下函数仍可通过 skill 包访问（但不再定义在此）
