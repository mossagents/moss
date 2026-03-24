package appkit

import (
	"flag"
	"os"
	"runtime"

	"github.com/mossagi/moss/kernel/skill"
)

// CommonFlags 包含所有 MOSS 应用共享的 CLI 参数。
// 解析优先级：CLI flag > 全局配置文件 > 环境变量 > 默认值。
type CommonFlags struct {
	Provider  string
	Model     string
	Workspace string
	Trust     string
	APIKey    string
	BaseURL   string
}

// ParseCommonFlags 注册并解析通用 CLI 参数，合并全局配置文件的值。
// 调用者可以在调用前通过 flag.XxxVar 注册额外参数，共享同一 FlagSet。
func ParseCommonFlags() *CommonFlags {
	f := &CommonFlags{}
	BindCommonFlags(flag.CommandLine, f)
	flag.Parse()
	f.MergeGlobalConfig()
	f.MergeEnv("MOSS")
	return f
}

// BindCommonFlags 将通用参数注册到指定 FlagSet。
func BindCommonFlags(fs *flag.FlagSet, f *CommonFlags) {
	fs.StringVar(&f.Provider, "provider", "openai", "LLM provider: claude|openai")
	fs.StringVar(&f.Model, "model", "", "Model name")
	fs.StringVar(&f.Workspace, "workspace", ".", "Workspace directory")
	fs.StringVar(&f.Trust, "trust", "trusted", "Trust level: trusted|restricted")
	fs.StringVar(&f.APIKey, "api-key", "", "API key (overrides env)")
	fs.StringVar(&f.BaseURL, "base-url", "", "API base URL")
}

// MergeGlobalConfig 从全局配置文件补充未通过 CLI 设置的字段。
// 在手动解析 flag 后调用此方法来合并配置。
func (f *CommonFlags) MergeGlobalConfig() {
	f.mergeGlobalConfig()
}

// MergeEnv 按顺序从环境变量补充未显式设置的字段。
// 例如 prefixes=["MINIWORK", "MOSS"] 时，会尝试 MINIWORK_PROVIDER、MOSS_PROVIDER。
func (f *CommonFlags) MergeEnv(prefixes ...string) {
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		f.Provider = FirstNonEmpty(f.Provider, os.Getenv(prefix+"_PROVIDER"))
		f.Model = FirstNonEmpty(f.Model, os.Getenv(prefix+"_MODEL"))
		f.Workspace = FirstNonEmpty(f.Workspace, os.Getenv(prefix+"_WORKSPACE"))
		f.Trust = FirstNonEmpty(f.Trust, os.Getenv(prefix+"_TRUST"))
		f.APIKey = FirstNonEmpty(f.APIKey, os.Getenv(prefix+"_API_KEY"))
		f.BaseURL = FirstNonEmpty(f.BaseURL, os.Getenv(prefix+"_BASE_URL"))
	}
}

func (f *CommonFlags) mergeGlobalConfig() {
	globalCfg, err := skill.LoadGlobalConfig()
	if err != nil || globalCfg == nil {
		globalCfg = &skill.Config{}
	}

	f.Provider = FirstNonEmpty(f.Provider, globalCfg.Provider, "openai")
	f.Model = FirstNonEmpty(f.Model, globalCfg.Model)
	f.APIKey = FirstNonEmpty(f.APIKey, globalCfg.APIKey)
	f.BaseURL = FirstNonEmpty(f.BaseURL, globalCfg.BaseURL)
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

// RenderSystemPrompt 使用标准模板上下文渲染 system prompt，并允许附加领域专属变量。
func RenderSystemPrompt(workspace, defaultTemplate string, extra map[string]any) string {
	ctx := DefaultTemplateContext(workspace)
	for key, value := range extra {
		ctx[key] = value
	}
	return skill.RenderSystemPrompt(workspace, defaultTemplate, ctx)
}
