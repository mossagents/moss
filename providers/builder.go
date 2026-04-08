package providers

import (
	"fmt"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/providers/claude"
	"github.com/mossagents/moss/providers/gemini"
	"github.com/mossagents/moss/providers/openai"
	"strings"
)

// BuildLLM 根据 provider 构建 LLM 实例。
//
// 支持的 provider：
//   - "openai-completions"（兼容别名："openai"）：OpenAI Chat Completions API
//   - "openai-responses"：OpenAI Responses API
//   - "claude" / "anthropic"：Anthropic Claude API
//   - "gemini" / "google"：Google Gemini API
//
// apiKey 和 baseURL 为空时使用环境变量默认值。
func BuildLLM(apiType, model, apiKey, baseURL string) (mdl.LLM, error) {
	switch strings.ToLower(apiType) {
	case "claude", "anthropic":
		var opts []claude.Option
		if model != "" {
			opts = append(opts, claude.WithModel(model))
		}
		if baseURL != "" || apiKey != "" {
			return claude.NewWithBaseURL(apiKey, baseURL, opts...), nil
		}
		return claude.New("", opts...), nil

	case "openai", "openai-completions":
		var opts []openai.Option
		if model != "" {
			opts = append(opts, openai.WithModel(model))
		}
		if baseURL != "" || apiKey != "" {
			return openai.NewWithBaseURL(apiKey, baseURL, opts...), nil
		}
		return openai.New("", opts...), nil

	case "openai-responses":
		return nil, fmt.Errorf("provider %q is reserved for the OpenAI Responses API and is not implemented yet", apiType)

	case "gemini", "google":
		var opts []gemini.Option
		if model != "" {
			opts = append(opts, gemini.WithModel(model))
		}
		if baseURL != "" || apiKey != "" {
			return gemini.NewWithBaseURL(apiKey, baseURL, opts...), nil
		}
		return gemini.New("", opts...), nil

	default:
		return nil, fmt.Errorf("unknown provider: %s (supported: claude, openai-completions, openai-responses, gemini)", apiType)
	}
}
