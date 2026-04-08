package skill

import cfg "github.com/mossagents/moss/config"

type Config = cfg.Config
type SkillConfig = cfg.SkillConfig

const (
	APITypeClaude            = cfg.APITypeClaude
	APITypeGemini            = cfg.APITypeGemini
	APITypeOpenAICompletions = cfg.APITypeOpenAICompletions
	APITypeOpenAIResponses   = cfg.APITypeOpenAIResponses
	TrustTrusted             = cfg.TrustTrusted
	TrustRestricted          = cfg.TrustRestricted
)

func LoadConfig(path string) (*Config, error) {
	return cfg.LoadConfig(path)
}

func MergeConfigs(configs ...*Config) *Config {
	return cfg.MergeConfigs(configs...)
}

func SaveConfig(path string, config *Config) error {
	return cfg.SaveConfig(path, config)
}

func AppDir() string {
	return cfg.AppDir()
}

func SetAppName(name string) {
	cfg.SetAppName(name)
}

func EnsureAppDir() error {
	return cfg.EnsureAppDir()
}

func DefaultGlobalConfigPath() string {
	return cfg.DefaultGlobalConfigPath()
}

func LoadGlobalConfig() (*Config, error) {
	return cfg.LoadGlobalConfig()
}

func DefaultGlobalSystemPromptTemplatePath() string {
	return cfg.DefaultGlobalSystemPromptTemplatePath()
}

func DefaultProjectSystemPromptTemplatePath(workspace string) string {
	return cfg.DefaultProjectSystemPromptTemplatePath(workspace)
}

func NormalizeTrustLevel(trust string) string {
	return cfg.NormalizeTrustLevel(trust)
}

func RenderSystemPrompt(workspace, defaultTemplate string, data map[string]any) string {
	return cfg.RenderSystemPrompt(workspace, defaultTemplate, data)
}

func RenderSystemPromptForTrust(workspace, trust, defaultTemplate string, data map[string]any) string {
	return cfg.RenderSystemPromptForTrust(workspace, trust, defaultTemplate, data)
}
