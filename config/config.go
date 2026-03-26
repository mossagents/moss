package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

var appName = "moss"

func SetAppName(name string) { appName = name }

func AppName() string { return appName }

func AppDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "."+appName)
}

func EnsureAppDir() error {
	dir := AppDir()
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

type Config struct {
	Provider string        `yaml:"provider,omitempty"`
	Model    string        `yaml:"model,omitempty"`
	BaseURL  string        `yaml:"base_url,omitempty"`
	APIKey   string        `yaml:"api_key,omitempty"`
	Skills   []SkillConfig `yaml:"skills,omitempty"`
}

type SkillConfig struct {
	Name      string            `yaml:"name"`
	Transport string            `yaml:"transport,omitempty"`
	Command   string            `yaml:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty"`
	URL       string            `yaml:"url,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Enabled   *bool             `yaml:"enabled,omitempty"`
}

func (sc SkillConfig) IsEnabled() bool {
	if sc.Enabled == nil {
		return true
	}
	return *sc.Enabled
}

func (sc SkillConfig) IsMCP() bool {
	return sc.Transport == "stdio" || sc.Transport == "sse"
}

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

func MergeConfigs(configs ...*Config) *Config {
	seen := make(map[string]int)
	var result []SkillConfig

	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		for _, sc := range cfg.Skills {
			if idx, ok := seen[sc.Name]; ok {
				result[idx] = sc
			} else {
				seen[sc.Name] = len(result)
				result = append(result, sc)
			}
		}
	}

	return &Config{Skills: result}
}

func DefaultGlobalConfigPath() string {
	d := AppDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "config.yaml")
}

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

func DefaultProjectConfigPath(workspace string) string {
	return filepath.Join(workspace, "moss.yaml")
}

func DefaultGlobalSystemPromptTemplatePath() string {
	d := AppDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "system_prompt.tmpl")
}

func DefaultProjectSystemPromptTemplatePath(workspace string) string {
	return filepath.Join(workspace, "."+AppName(), "system_prompt.tmpl")
}

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

// DefaultTemplateContext 返回 system prompt 模板渲染的通用上下文变量。
// 包括 OS、Shell、Arch、Hostname、Workspace 等常用字段。
// 调用者可在返回的 map 中追加领域专属字段。
func DefaultTemplateContext(workspace string) map[string]any {
	osName := runtime.GOOS
	shell := "bash"
	if osName == "windows" {
		shell = "powershell"
	}
	hostname, _ := os.Hostname()
	return map[string]any{
		"OS":        osName,
		"Shell":     shell,
		"Arch":      runtime.GOARCH,
		"Hostname":  hostname,
		"Workspace": workspace,
	}
}

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
