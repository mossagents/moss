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

const (
	TrustTrusted    = "trusted"
	TrustRestricted = "restricted"
)

func SetAppName(name string) { appName = name }

func AppName() string { return appName }

func NormalizeTrustLevel(trust string) string {
	switch strings.ToLower(strings.TrimSpace(trust)) {
	case "", TrustTrusted:
		return TrustTrusted
	case TrustRestricted:
		return TrustRestricted
	default:
		return TrustRestricted
	}
}

func ProjectAssetsAllowed(trust string) bool {
	return NormalizeTrustLevel(trust) == TrustTrusted
}

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
	LegacyAPIType     string                   `yaml:"api_type,omitempty" json:"-"`
	Name              string                   `yaml:"name,omitempty"`
	Provider          string                   `yaml:"provider,omitempty"`
	Model             string                   `yaml:"model,omitempty"`
	BaseURL           string                   `yaml:"base_url,omitempty"`
	APIKey            string                   `yaml:"api_key,omitempty"`
	Models            []ModelConfig            `yaml:"models,omitempty"`
	BaseInstructions  string                   `yaml:"base_instructions,omitempty"`
	ModelInstructions string                   `yaml:"model_instructions,omitempty"`
	DefaultProfile    string                   `yaml:"default_profile,omitempty"`
	Profiles          map[string]ProfileConfig `yaml:"profiles,omitempty"`
	Skills            []SkillConfig            `yaml:"skills,omitempty"`
	TUI               TUIConfig                `yaml:"tui,omitempty"`
}

type ModelConfig struct {
	LegacyAPIType string `yaml:"api_type,omitempty" json:"-"`
	Name          string `yaml:"name,omitempty"`
	Provider      string `yaml:"provider,omitempty"`
	Model         string `yaml:"model,omitempty"`
	BaseURL       string `yaml:"base_url,omitempty"`
	APIKey        string `yaml:"api_key,omitempty"`
	Default       bool   `yaml:"default,omitempty"`
}

type TUIConfig struct {
	Theme                string               `yaml:"theme,omitempty"`
	StatusLine           []string             `yaml:"status_line,omitempty"`
	Personality          string               `yaml:"personality,omitempty"`
	FastMode             *bool                `yaml:"fast_mode,omitempty"`
	Experimental         []string             `yaml:"experimental,omitempty"`
	SelectedProvider     string               `yaml:"selected_provider,omitempty"`
	SelectedProviderName string               `yaml:"selected_provider_name,omitempty"`
	SelectedModel        string               `yaml:"selected_model,omitempty"`
	ProjectApprovalRules []ApprovalRuleConfig `yaml:"project_approval_rules,omitempty"`
}

type ApprovalRuleConfig struct {
	ToolName string `yaml:"tool_name,omitempty"`
	Key      string `yaml:"key,omitempty"`
	Label    string `yaml:"label,omitempty"`
}

type SkillConfig struct {
	Name      string            `yaml:"name"`
	Transport string            `yaml:"transport,omitempty"`
	Command   string            `yaml:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty"`
	URL       string            `yaml:"url,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Enabled   *bool             `yaml:"enabled,omitempty"`
	Required  *bool             `yaml:"required,omitempty"`
}

type ProfileConfig struct {
	Label     string                 `yaml:"label,omitempty"`
	TaskMode  string                 `yaml:"task_mode,omitempty"`
	Trust     string                 `yaml:"trust,omitempty"`
	Approval  string                 `yaml:"approval,omitempty"`
	Session   SessionProfileConfig   `yaml:"session,omitempty"`
	Execution ExecutionProfileConfig `yaml:"execution,omitempty"`
}

type SessionProfileConfig struct {
	MaxSteps  int `yaml:"max_steps,omitempty"`
	MaxTokens int `yaml:"max_tokens,omitempty"`
}

type ExecutionProfileConfig struct {
	CommandAccess  string              `yaml:"command_access,omitempty"`
	HTTPAccess     string              `yaml:"http_access,omitempty"`
	CommandTimeout string              `yaml:"command_timeout,omitempty"`
	HTTPTimeout    string              `yaml:"http_timeout,omitempty"`
	CommandRules   []CommandRuleConfig `yaml:"command_rules,omitempty"`
	HTTPRules      []HTTPRuleConfig    `yaml:"http_rules,omitempty"`
}

type CommandRuleConfig struct {
	Name   string `yaml:"name,omitempty"`
	Match  string `yaml:"match,omitempty"`
	Access string `yaml:"access,omitempty"`
}

type HTTPRuleConfig struct {
	Name    string   `yaml:"name,omitempty"`
	Match   string   `yaml:"match,omitempty"`
	Methods []string `yaml:"methods,omitempty"`
	Access  string   `yaml:"access,omitempty"`
}

type ProviderIdentity struct {
	APIType  string
	Provider string
	Name     string
}

const (
	APITypeClaude            = "claude"
	APITypeGemini            = "gemini"
	APITypeOpenAICompletions = "openai-completions"
	APITypeOpenAIResponses   = "openai-responses"
)

func (sc SkillConfig) IsEnabled() bool {
	if sc.Enabled == nil {
		return true
	}
	return *sc.Enabled
}

func (sc SkillConfig) IsMCP() bool {
	return sc.Transport == "stdio" || sc.Transport == "sse"
}

func (sc SkillConfig) IsRequired() bool {
	if sc.Required == nil {
		return false
	}
	return *sc.Required
}

func NormalizeProviderIdentity(apiType, provider, name string) ProviderIdentity {
	apiType = normalizeLLMAPIType(apiType)
	provider = normalizeLLMAPIType(provider)
	name = strings.TrimSpace(name)

	apiType = firstNonEmpty(provider, apiType)
	provider = firstNonEmpty(provider, apiType)
	name = firstNonEmpty(name, apiType, provider)

	return ProviderIdentity{
		APIType:  apiType,
		Provider: provider,
		Name:     name,
	}
}

func (p ProviderIdentity) EffectiveAPIType() string {
	return normalizeLLMAPIType(firstNonEmpty(strings.TrimSpace(p.APIType), strings.TrimSpace(p.Provider)))
}

func (p ProviderIdentity) DisplayName() string {
	return firstNonEmpty(strings.TrimSpace(p.Name), p.EffectiveAPIType())
}

func (p ProviderIdentity) Label() string {
	apiType := p.EffectiveAPIType()
	name := p.DisplayName()
	switch {
	case name == "":
		return apiType
	case apiType == "":
		return name
	case strings.EqualFold(apiType, name):
		return name
	default:
		return fmt.Sprintf("%s (%s)", name, apiType)
	}
}

func normalizeLLMAPIType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case "openai":
		return APITypeOpenAICompletions
	case "openai-completion", "openai-completions", "chat-completions", "openai-chat-completions":
		return APITypeOpenAICompletions
	case "openai-response", "openai-responses", "responses", "openai-response-api":
		return APITypeOpenAIResponses
	case "anthropic":
		return APITypeClaude
	case "google":
		return APITypeGemini
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
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
	cfg.normalizeProviderFields()
	return &cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	copyCfg := *cfg
	if len(cfg.Models) > 0 {
		copyCfg.Models = append([]ModelConfig(nil), cfg.Models...)
	}
	copyCfg.projectLegacyFieldsToModels()
	copyCfg.normalizeProviderFields()
	copyCfg.Provider = ""
	copyCfg.LegacyAPIType = ""
	copyCfg.Name = ""
	copyCfg.Model = ""
	copyCfg.BaseURL = ""
	copyCfg.APIKey = ""
	for i := range copyCfg.Models {
		copyCfg.Models[i].LegacyAPIType = ""
	}
	data, err := yaml.Marshal(&copyCfg)
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

	cfg := &Config{Skills: result}
	cfg.normalizeProviderFields()
	return cfg
}

func (c *Config) normalizeProviderFields() {
	if c == nil {
		return
	}
	c.normalizeModels()
	if selected := c.selectedModel(); selected != nil {
		identity := NormalizeProviderIdentity(selected.LegacyAPIType, selected.Provider, selected.Name)
		selected.LegacyAPIType = ""
		selected.Provider = identity.Provider
		selected.Name = identity.Name
		c.LegacyAPIType = ""
		c.Provider = selected.Provider
		c.Name = selected.Name
		c.Model = strings.TrimSpace(selected.Model)
		c.BaseURL = strings.TrimSpace(selected.BaseURL)
		c.APIKey = strings.TrimSpace(selected.APIKey)
		return
	}
	identity := NormalizeProviderIdentity(c.LegacyAPIType, c.Provider, c.Name)
	c.LegacyAPIType = ""
	c.Provider = identity.Provider
	c.Name = identity.Name
	c.Model = strings.TrimSpace(c.Model)
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.APIKey = strings.TrimSpace(c.APIKey)
}

func (c *Config) ProviderIdentity() ProviderIdentity {
	if c == nil {
		return ProviderIdentity{}
	}
	if selected := c.selectedModel(); selected != nil {
		return NormalizeProviderIdentity(selected.LegacyAPIType, selected.Provider, selected.Name)
	}
	return NormalizeProviderIdentity(c.LegacyAPIType, c.Provider, c.Name)
}

func (c *Config) EffectiveAPIType() string {
	return c.ProviderIdentity().EffectiveAPIType()
}

func (c *Config) DisplayProviderName() string {
	return c.ProviderIdentity().DisplayName()
}

func (c *Config) normalizeModels() {
	if c == nil {
		return
	}
	for i := range c.Models {
		identity := NormalizeProviderIdentity(c.Models[i].LegacyAPIType, c.Models[i].Provider, c.Models[i].Name)
		c.Models[i].LegacyAPIType = ""
		c.Models[i].Provider = identity.Provider
		c.Models[i].Name = identity.Name
		c.Models[i].Model = strings.TrimSpace(c.Models[i].Model)
		c.Models[i].BaseURL = strings.TrimSpace(c.Models[i].BaseURL)
		c.Models[i].APIKey = strings.TrimSpace(c.Models[i].APIKey)
	}
	hasDefault := false
	for i := range c.Models {
		if c.Models[i].Default {
			hasDefault = true
			break
		}
	}
	if len(c.Models) == 0 {
		if !c.hasLegacyModelFields() {
			return
		}
		identity := NormalizeProviderIdentity(c.LegacyAPIType, c.Provider, c.Name)
		c.Models = []ModelConfig{{
			Provider: identity.Provider,
			Name:     identity.Name,
			Model:    strings.TrimSpace(c.Model),
			BaseURL:  strings.TrimSpace(c.BaseURL),
			APIKey:   strings.TrimSpace(c.APIKey),
			Default:  true,
		}}
		return
	}
	if !hasDefault {
		c.Models[0].Default = true
	}
}

func (c *Config) selectedModel() *ModelConfig {
	if c == nil {
		return nil
	}
	idx := c.selectedModelIndex()
	if idx < 0 {
		return nil
	}
	return &c.Models[idx]
}

func (c *Config) selectedModelIndex() int {
	if c == nil || len(c.Models) == 0 {
		return -1
	}
	for i := range c.Models {
		if c.Models[i].Default {
			return i
		}
	}
	return 0
}

func (c *Config) hasLegacyModelFields() bool {
	if c == nil {
		return false
	}
	return strings.TrimSpace(c.LegacyAPIType) != "" ||
		strings.TrimSpace(c.Provider) != "" ||
		strings.TrimSpace(c.Name) != "" ||
		strings.TrimSpace(c.Model) != "" ||
		strings.TrimSpace(c.BaseURL) != "" ||
		strings.TrimSpace(c.APIKey) != ""
}

func (c *Config) legacyFieldsDifferFrom(model ModelConfig) bool {
	if c == nil || !c.hasLegacyModelFields() {
		return false
	}
	identity := NormalizeProviderIdentity(c.LegacyAPIType, c.Provider, c.Name)
	modelIdentity := NormalizeProviderIdentity(model.LegacyAPIType, model.Provider, model.Name)
	return identity.Provider != modelIdentity.Provider ||
		identity.Name != modelIdentity.Name ||
		strings.TrimSpace(c.Model) != strings.TrimSpace(model.Model) ||
		strings.TrimSpace(c.BaseURL) != strings.TrimSpace(model.BaseURL) ||
		strings.TrimSpace(c.APIKey) != strings.TrimSpace(model.APIKey)
}

func (c *Config) projectLegacyFieldsToModels() {
	if c == nil || !c.hasLegacyModelFields() {
		return
	}
	identity := NormalizeProviderIdentity(c.LegacyAPIType, c.Provider, c.Name)
	if len(c.Models) == 0 {
		c.Models = []ModelConfig{{
			Provider: identity.Provider,
			Name:     identity.Name,
			Model:    strings.TrimSpace(c.Model),
			BaseURL:  strings.TrimSpace(c.BaseURL),
			APIKey:   strings.TrimSpace(c.APIKey),
			Default:  true,
		}}
		return
	}
	idx := c.selectedModelIndex()
	if idx < 0 {
		idx = 0
	}
	selected := c.Models[idx]
	if !c.legacyFieldsDifferFrom(selected) {
		return
	}
	c.Models[idx].LegacyAPIType = ""
	c.Models[idx].Provider = identity.Provider
	c.Models[idx].Name = identity.Name
	c.Models[idx].Model = strings.TrimSpace(c.Model)
	c.Models[idx].BaseURL = strings.TrimSpace(c.BaseURL)
	c.Models[idx].APIKey = strings.TrimSpace(c.APIKey)
	c.Models[idx].Default = true
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

func LoadProjectConfig(workspace string) (*Config, error) {
	path := DefaultProjectConfigPath(workspace)
	if path == "" {
		return &Config{}, nil
	}
	return LoadConfig(path)
}

func LoadProjectConfigForTrust(workspace, trust string) (*Config, error) {
	if !ProjectAssetsAllowed(trust) {
		return &Config{}, nil
	}
	return LoadProjectConfig(workspace)
}

func ResolvePromptInstructionLayers(workspace, trust string) (string, string, error) {
	globalCfg, err := LoadGlobalConfig()
	if err != nil {
		return "", "", err
	}
	projectCfg, err := LoadProjectConfigForTrust(workspace, trust)
	if err != nil {
		return "", "", err
	}
	configInstructions := firstNonEmpty(projectCfg.BaseInstructions, globalCfg.BaseInstructions)
	modelInstructions := firstNonEmpty(projectCfg.ModelInstructions, globalCfg.ModelInstructions)
	return configInstructions, modelInstructions, nil
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
	return LoadSystemPromptTemplateForTrust(workspace, TrustTrusted)
}

func LoadSystemPromptTemplateForTrust(workspace, trust string) (string, error) {
	if ProjectAssetsAllowed(trust) {
		projectPath := DefaultProjectSystemPromptTemplatePath(workspace)
		if projectPath != "" {
			if data, err := os.ReadFile(projectPath); err == nil {
				return string(data), nil
			} else if !os.IsNotExist(err) {
				return "", fmt.Errorf("read system prompt template %s: %w", projectPath, err)
			}
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
	return RenderSystemPromptForTrust(workspace, TrustTrusted, defaultTemplate, data)
}

func RenderSystemPromptForTrust(workspace, trust, defaultTemplate string, data map[string]any) string {
	tplSrc := defaultTemplate
	if loaded, err := LoadSystemPromptTemplateForTrust(workspace, trust); err == nil && strings.TrimSpace(loaded) != "" {
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

# models:
#   - default: true
#     provider: openai-completions
#     name: openai-completions
#     model: gpt-4o
#     base_url: ""
#     api_key: ""

# tui:
#   # theme: default
#   # personality: friendly
#   # fast_mode: false
#   # status_line: [model, workspace, profile, approval, thread, messages]
#   # experimental: [background-ps, composer-mentions, statusline-customization]

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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
