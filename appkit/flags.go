package appkit

import (
	"github.com/mossagents/moss/internal/strutil"
	"flag"
	"os"
	"strconv"
	"strings"

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

	EnableSummarize bool
	EnableRAG       bool
	PromptAssembly  string
	PromptVersion   string

	BudgetGovernance string
	GlobalMaxTokens  int
	GlobalMaxSteps   int
	GlobalWarnAt     float64
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
	fs.BoolVar(&f.EnableSummarize, "enable-summarize", false, "Enable summarize middleware")
	fs.BoolVar(&f.EnableRAG, "enable-rag", false, "Enable RAG middleware")
	fs.StringVar(&f.PromptAssembly, "prompt-assembly", "", "Prompt assembly mode: unified|legacy")
	fs.StringVar(&f.PromptVersion, "prompt-version", "", "Prompt version tag override")
	fs.StringVar(&f.BudgetGovernance, "budget-governance", "", "Global budget governance mode: off|observe-only|enforce")
	fs.IntVar(&f.GlobalMaxTokens, "global-max-tokens", 0, "Global token budget cap (0 = unlimited)")
	fs.IntVar(&f.GlobalMaxSteps, "global-max-steps", 0, "Global step budget cap (0 = unlimited)")
	fs.Float64Var(&f.GlobalWarnAt, "global-budget-warn-at", 0, "Global budget warning ratio, e.g. 0.8")
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
	fs.BoolVar(&f.EnableSummarize, "enable-summarize", false, "Enable summarize middleware")
	fs.BoolVar(&f.EnableRAG, "enable-rag", false, "Enable RAG middleware")
	fs.StringVar(&f.PromptAssembly, "prompt-assembly", "", "Prompt assembly mode: unified|legacy")
	fs.StringVar(&f.PromptVersion, "prompt-version", "", "Prompt version tag override")
	fs.StringVar(&f.BudgetGovernance, "budget-governance", "", "Global budget governance mode: off|observe-only|enforce")
	fs.IntVar(&f.GlobalMaxTokens, "global-max-tokens", 0, "Global token budget cap (0 = unlimited)")
	fs.IntVar(&f.GlobalMaxSteps, "global-max-steps", 0, "Global step budget cap (0 = unlimited)")
	fs.Float64Var(&f.GlobalWarnAt, "global-budget-warn-at", 0, "Global budget warning ratio, e.g. 0.8")
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
		f.Name = strutil.FirstNonEmpty(f.Name, os.Getenv(prefix+"_NAME"))
		f.Provider = strutil.FirstNonEmpty(f.Provider, os.Getenv(prefix+"_PROVIDER"), os.Getenv(prefix+"_API_TYPE"))
		f.Model = strutil.FirstNonEmpty(f.Model, os.Getenv(prefix+"_MODEL"))
		f.Workspace = strutil.FirstNonEmpty(f.Workspace, os.Getenv(prefix+"_WORKSPACE"))
		f.Trust = strutil.FirstNonEmpty(f.Trust, os.Getenv(prefix+"_TRUST"))
		f.Profile = strutil.FirstNonEmpty(f.Profile, os.Getenv(prefix+"_PROFILE"))
		f.APIKey = strutil.FirstNonEmpty(f.APIKey, os.Getenv(prefix+"_API_KEY"))
		f.BaseURL = strutil.FirstNonEmpty(f.BaseURL, os.Getenv(prefix+"_BASE_URL"))
		f.PromptAssembly = strutil.FirstNonEmpty(f.PromptAssembly, os.Getenv(prefix+"_PROMPT_ASSEMBLY"))
		f.PromptVersion = strutil.FirstNonEmpty(f.PromptVersion, os.Getenv(prefix+"_PROMPT_VERSION"))
		f.BudgetGovernance = strutil.FirstNonEmpty(f.BudgetGovernance, os.Getenv(prefix+"_BUDGET_GOVERNANCE"))
		if raw := strings.TrimSpace(os.Getenv(prefix + "_GLOBAL_MAX_TOKENS")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				f.GlobalMaxTokens = v
			}
		}
		if raw := strings.TrimSpace(os.Getenv(prefix + "_GLOBAL_MAX_STEPS")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				f.GlobalMaxSteps = v
			}
		}
		if raw := strings.TrimSpace(os.Getenv(prefix + "_GLOBAL_BUDGET_WARN_AT")); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				f.GlobalWarnAt = v
			}
		}
	}
}

// ApplyDefaults 在 CLI、配置文件、环境变量合并完成后补齐默认值。
func (f *AppFlags) ApplyDefaults() {
	f.normalizeProviderFields()
	f.Provider = strutil.FirstNonEmpty(f.Provider, config.APITypeOpenAICompletions)
	f.Name = strutil.FirstNonEmpty(f.Name, f.Provider)
	f.Workspace = strutil.FirstNonEmpty(f.Workspace, ".")
	f.Trust = strutil.FirstNonEmpty(f.Trust, config.TrustRestricted)
	f.PromptAssembly = strutil.FirstNonEmpty(f.PromptAssembly, "unified")
	f.BudgetGovernance = normalizeBudgetGovernance(strutil.FirstNonEmpty(f.BudgetGovernance, "observe-only"))
	if f.GlobalWarnAt < 0 {
		f.GlobalWarnAt = 0
	}
}

func normalizeBudgetGovernance(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "off", "enforce", "observe-only":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "observe-only"
	}
}

func (f *AppFlags) mergeGlobalConfig() {
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil || globalCfg == nil {
		globalCfg = &config.Config{}
	}

	identity := globalCfg.ProviderIdentity()
	f.Name = strutil.FirstNonEmpty(f.Name, identity.Name)
	f.Provider = strutil.FirstNonEmpty(f.Provider, identity.Provider, config.APITypeOpenAICompletions)
	f.Model = strutil.FirstNonEmpty(f.Model, globalCfg.Model)
	f.APIKey = strutil.FirstNonEmpty(f.APIKey, globalCfg.APIKey)
	f.BaseURL = strutil.FirstNonEmpty(f.BaseURL, globalCfg.BaseURL)
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
