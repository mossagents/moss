package runtime

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/mossagents/moss/kernel/model"
)

// ─────────────────────────────────────────────
// PromptCompiler 接口（§7.2、§5.5）
// ─────────────────────────────────────────────

// CompiledPrompt 是 PromptCompiler 的输出，代表一次已物化的 prompt。
type CompiledPrompt struct {
	// Messages 是发给 LLM 的最终消息序列。
	Messages []model.Message `json:"messages"`
	// PromptHash 稳定 hash，用于 prompt_materialized 事件中的 prompt_hash 字段。
	PromptHash string `json:"prompt_hash"`
	// SelectedLayerIDs 本次包含的 layer 列表（有序）。
	SelectedLayerIDs []string `json:"selected_layer_ids"`
	// TruncatedLayerIDs 因预算不足被截断的 layer 列表（§7.3）。
	TruncatedLayerIDs []string `json:"truncated_layer_ids,omitempty"`
	// EstimatedTokens 估算总 token 数（主 token）。
	EstimatedTokens int `json:"estimated_tokens,omitempty"`
}

// PromptCompiler 接受 SessionBlueprint、MaterializedState 和 PromptLayerProvider 列表，
// 输出编译后的 CompiledPrompt（含 PromptHash、截断 layer 信息）。
type PromptCompiler interface {
	// Compile 编译 prompt。
	// layers 是当前 turn 可用的全部 PromptLayerProvider（含 system / user / assistant scope）。
	Compile(blueprint SessionBlueprint, state *MaterializedState, layers []PromptLayerProvider) (CompiledPrompt, error)
}

// ─────────────────────────────────────────────
// DefaultPromptCompiler 最小实现
// ─────────────────────────────────────────────

// DefaultPromptCompiler 是最小可用的 PromptCompiler 实现。
// 它按 Priority 排序 layer，在 ContextBudget 范围内依次纳入，
// 超出预算的 layer 记入 TruncatedLayerIDs。
type DefaultPromptCompiler struct {
	// TokenEstimator 估算 ContentParts 的 token 数，默认使用字符数 / 4 的简单估算。
	TokenEstimator func(parts []model.ContentPart) int
}

// NewDefaultPromptCompiler 创建 DefaultPromptCompiler。
func NewDefaultPromptCompiler() *DefaultPromptCompiler {
	return &DefaultPromptCompiler{
		TokenEstimator: simpleTokenEstimate,
	}
}

// Compile 实现 PromptCompiler 接口。
func (c *DefaultPromptCompiler) Compile(
	blueprint SessionBlueprint,
	state *MaterializedState,
	layers []PromptLayerProvider,
) (CompiledPrompt, error) {
	// 1. 按 Priority 升序排列（Priority 越小 = 越优先）
	sorted := make([]PromptLayerProvider, len(layers))
	copy(sorted, layers)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	budget := blueprint.ContextBudget.MainTokenBudget
	if budget <= 0 {
		budget = 100_000 // 保底预算
	}

	var (
		selected    []PromptLayerProvider
		truncated   []string
		totalTokens int
	)

	// 2. 按优先级纳入 layer，超预算则截断
	for _, layer := range sorted {
		est := c.TokenEstimator(layer.ContentParts)
		if totalTokens+est > budget {
			truncated = append(truncated, layer.LayerID)
			continue
		}
		selected = append(selected, layer)
		totalTokens += est
	}

	// 3. 将 selected layers 转换为 Messages
	messages := layersToMessages(selected, state)

	// 4. 计算 PromptHash
	hash := computePromptHash(messages)

	selectedIDs := make([]string, len(selected))
	for i, l := range selected {
		selectedIDs[i] = l.LayerID
	}

	return CompiledPrompt{
		Messages:          messages,
		PromptHash:        hash,
		SelectedLayerIDs:  selectedIDs,
		TruncatedLayerIDs: truncated,
		EstimatedTokens:   totalTokens,
	}, nil
}

// layersToMessages 将 PromptLayerProvider 列表转换为 model.Message 列表。
// system scope → Role=system；user scope → Role=user；assistant scope → Role=assistant。
func layersToMessages(layers []PromptLayerProvider, state *MaterializedState) []model.Message {
	// 先收集 system layers（优先级序），再追加对话历史，最后 user layers
	var systemMsgs, userMsgs, assistantMsgs []model.Message

	for _, layer := range layers {
		parts := layer.ContentParts
		if len(parts) == 0 {
			continue
		}
		var role model.Role
		switch layer.Scope {
		case "system":
			role = model.RoleSystem
		case "assistant":
			role = model.RoleAssistant
		default:
			role = model.RoleUser
		}

		msg := model.Message{Role: role, ContentParts: parts}
		switch role {
		case model.RoleSystem:
			systemMsgs = append(systemMsgs, msg)
		case model.RoleAssistant:
			assistantMsgs = append(assistantMsgs, msg)
		default:
			userMsgs = append(userMsgs, msg)
		}
	}

	// 拼接：system → history（若有 state）→ user
	var messages []model.Message
	messages = append(messages, systemMsgs...)

	// 注入 persistent history（§7.3 layer 提升规则）
	if state != nil {
		for _, item := range state.PersistentHistory {
			if !item.Active {
				continue
			}
			r := model.RoleUser
			if item.Content.Role == "assistant" {
				r = model.RoleAssistant
			}
			messages = append(messages, model.Message{
				Role:         r,
				ContentParts: []model.ContentPart{{Type: model.ContentPartText, Text: item.Content.Text}},
			})
		}
	}

	messages = append(messages, assistantMsgs...)
	messages = append(messages, userMsgs...)
	return messages
}

// computePromptHash 计算 Messages 的稳定 hash（§5.5 物化契约）。
func computePromptHash(msgs []model.Message) string {
	var parts []string
	for _, m := range msgs {
		for _, p := range m.ContentParts {
			parts = append(parts, fmt.Sprintf("%s:%s:%s", m.Role, p.Type, p.Text))
		}
	}
	repr := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(repr))
	return fmt.Sprintf("%x", sum[:16])
}

// simpleTokenEstimate 简单 token 估算：字符数 / 4（§7.2 budget 保障层保底）。
func simpleTokenEstimate(parts []model.ContentPart) int {
	total := 0
	for _, p := range parts {
		total += len(p.Text)
	}
	if total == 0 {
		return 1
	}
	return (total / 4) + 1
}
