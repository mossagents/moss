package adapters

import (
	"fmt"
	"strings"

	"github.com/mossagents/moss/adapters/claude"
	"github.com/mossagents/moss/adapters/openai"
	"github.com/mossagents/moss/kernel/port"
)

// BuildLLM 根据 apiType 构建 LLM 实例。
//
// 支持的 apiType：
//   - "openai"：OpenAI 兼容 API（也适用于本地 LLM via base URL）
//   - "claude" / "anthropic"：Anthropic Claude API
//
// apiKey 和 baseURL 为空时使用环境变量默认值。
func BuildLLM(apiType, model, apiKey, baseURL string) (port.LLM, error) {
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

	case "openai":
		var opts []openai.Option
		if model != "" {
			opts = append(opts, openai.WithModel(model))
		}
		if baseURL != "" || apiKey != "" {
			return openai.NewWithBaseURL(apiKey, baseURL, opts...), nil
		}
		return openai.New("", opts...), nil

	default:
		return nil, fmt.Errorf("unknown api_type: %s (supported: claude, openai)", apiType)
	}
}
