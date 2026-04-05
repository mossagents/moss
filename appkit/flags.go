package appkit

import (
	"flag"
	"os"

	config "github.com/mossagents/moss/config"
	"github.com/spf13/pflag"
)

// AppFlags 包含所有 MOSS 应用共享的 CLI 参数。
// 解析优先级：CLI flag > 全局配置文件 > 环境变量 > 默认值。
type AppFlags struct {
	Name      string
	Provider  string
	Model     string
	Workspace string
	Trust     string
	Profile   string
	APIKey    string
	BaseURL   string
}

// ParseAppFlags 注册并解析通用 CLI 参数，合并全局配置文件的值。
// 调用者可以在调用前通过 flag.XxxVar 注册额外参数，共享同一 FlagSet。
func ParseAppFlags() *AppFlags {
	f := &AppFlags{}
	BindAppFlags(flag.CommandLine, f)
	flag.Parse()
	f.MergeGlobalConfig()
	f.MergeEnv("MOSS")
	f.ApplyDefaults()
	return f
}

// BindAppFlags 将通用参数注册到指定 FlagSet。
func BindAppFlags(fs *flag.FlagSet, f *AppFlags) {
	fs.StringVar(&f.Name, "name", "", "LLM provider display name, e.g. openai-completions|openai-responses|deepseek")
	fs.StringVar(&f.Provider, "provider", "", "LLM provider: claude|openai-completions|openai-responses|gemini")
	fs.StringVar(&f.Model, "model", "", "Model name")
	fs.StringVar(&f.Workspace, "workspace", "", "Workspace directory")
	fs.StringVar(&f.Trust, "trust", "", "Trust level: trusted|restricted")
	fs.StringVar(&f.Profile, "profile", "", "Profile name")
	fs.StringVar(&f.APIKey, "api-key", "", "API key (overrides env)")
	fs.StringVar(&f.BaseURL, "base-url", "", "API base URL")
}

// BindAppPFlags 将通用参数注册到指定 pflag FlagSet，供 Cobra CLI 直接使用。
func BindAppPFlags(fs *pflag.FlagSet, f *AppFlags) {
	fs.StringVar(&f.Name, "name", "", "LLM provider display name, e.g. openai-completions|openai-responses|deepseek")
	fs.StringVar(&f.Provider, "provider", "", "LLM provider: claude|openai-completions|openai-responses|gemini")
	fs.StringVar(&f.Model, "model", "", "Model name")
	fs.StringVar(&f.Workspace, "workspace", "", "Workspace directory")
	fs.StringVar(&f.Trust, "trust", "", "Trust level: trusted|restricted")
	fs.StringVar(&f.Profile, "profile", "", "Profile name")
	fs.StringVar(&f.APIKey, "api-key", "", "API key (overrides env)")
	fs.StringVar(&f.BaseURL, "base-url", "", "API base URL")
}

// MergeGlobalConfig 从全局配置文件补充未通过 CLI 设置的字段。
// 在手动解析 flag 后调用此方法来合并配置。
func (f *AppFlags) MergeGlobalConfig() {
	f.mergeGlobalConfig()
}

// MergeEnv 按顺序从环境变量补充未显式设置的字段。
// 例如 prefixes=["MINIWORK", "MOSS"] 时，会尝试 MINIWORK_PROVIDER、MOSS_PROVIDER。
func (f *AppFlags) MergeEnv(prefixes ...string) {
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		f.Name = FirstNonEmpty(f.Name, os.Getenv(prefix+"_NAME"))
		f.Provider = FirstNonEmpty(f.Provider, os.Getenv(prefix+"_PROVIDER"), os.Getenv(prefix+"_API_TYPE"))
		f.Model = FirstNonEmpty(f.Model, os.Getenv(prefix+"_MODEL"))
		f.Workspace = FirstNonEmpty(f.Workspace, os.Getenv(prefix+"_WORKSPACE"))
		f.Trust = FirstNonEmpty(f.Trust, os.Getenv(prefix+"_TRUST"))
		f.Profile = FirstNonEmpty(f.Profile, os.Getenv(prefix+"_PROFILE"))
		f.APIKey = FirstNonEmpty(f.APIKey, os.Getenv(prefix+"_API_KEY"))
		f.BaseURL = FirstNonEmpty(f.BaseURL, os.Getenv(prefix+"_BASE_URL"))
	}
}

// ApplyDefaults 在 CLI、配置文件、环境变量合并完成后补齐默认值。
func (f *AppFlags) ApplyDefaults() {
	f.normalizeProviderFields()
	f.Provider = FirstNonEmpty(f.Provider, config.APITypeOpenAICompletions)
	f.Name = FirstNonEmpty(f.Name, f.Provider)
	f.Workspace = FirstNonEmpty(f.Workspace, ".")
	f.Trust = FirstNonEmpty(f.Trust, config.TrustRestricted)
}

func (f *AppFlags) mergeGlobalConfig() {
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil || globalCfg == nil {
		globalCfg = &config.Config{}
	}

	identity := globalCfg.ProviderIdentity()
	f.Name = FirstNonEmpty(f.Name, identity.Name)
	f.Provider = FirstNonEmpty(f.Provider, identity.Provider, config.APITypeOpenAICompletions)
	f.Model = FirstNonEmpty(f.Model, globalCfg.Model)
	f.APIKey = FirstNonEmpty(f.APIKey, globalCfg.APIKey)
	f.BaseURL = FirstNonEmpty(f.BaseURL, globalCfg.BaseURL)
	f.normalizeProviderFields()
}

func (f *AppFlags) normalizeProviderFields() {
	if f == nil {
		return
	}
	identity := f.ProviderIdentity()
	f.Provider = identity.Provider
	f.Name = identity.Name
}

func (f *AppFlags) ProviderIdentity() config.ProviderIdentity {
	if f == nil {
		return config.ProviderIdentity{}
	}
	return config.NormalizeProviderIdentity("", f.Provider, f.Name)
}

func (f *AppFlags) EffectiveAPIType() string {
	return f.ProviderIdentity().EffectiveAPIType()
}

func (f *AppFlags) DisplayProviderName() string {
	return f.ProviderIdentity().DisplayName()
}
