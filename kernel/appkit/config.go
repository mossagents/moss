package appkit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/mossagi/moss/kernel/skill"
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
//	appkit.SetAppName("minicode") // 配置目录变为 ~/.minicode
func SetAppName(name string) {
	appName = name
	skill.SetAppName(name) // 同步到 skill 包
}

// AppName 返回当前应用名称。
func AppName() string { return appName }

// AppDir 返回全局配置目录路径（~/.<appName>）。
func AppDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "."+appName)
}

// EnsureAppDir 确保 ~/.<appName> 目录存在，不存在则创建。
// 同时会在全局配置文件不存在时创建一个可编辑的模板文件。
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

// SaveConfig 将配置写入指定路径，自动创建父目录。
func SaveConfig(path string, cfg *skill.Config) error {
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

// DefaultGlobalConfigPath 返回全局配置文件路径（~/.<appName>/config.yaml）。
// 代理到 skill.DefaultGlobalConfigPath()。
func DefaultGlobalConfigPath() string {
	return skill.DefaultGlobalConfigPath()
}

// LoadGlobalConfig 加载全局配置（~/.<appName>/config.yaml）。
// 代理到 skill.LoadGlobalConfig()。
func LoadGlobalConfig() (*skill.Config, error) {
	return skill.LoadGlobalConfig()
}

// DefaultProjectConfigPath 返回项目级配置文件路径。
// 代理到 skill.DefaultProjectConfigPath()。
func DefaultProjectConfigPath(workspace string) string {
	return skill.DefaultProjectConfigPath(workspace)
}

// DefaultGlobalSystemPromptTemplatePath 返回全局 system prompt 模板路径（~/.<appName>/system_prompt.tmpl）。
func DefaultGlobalSystemPromptTemplatePath() string {
	d := AppDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "system_prompt.tmpl")
}

// DefaultProjectSystemPromptTemplatePath 返回项目级 system prompt 模板路径（./<appName>/system_prompt.tmpl）。
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
