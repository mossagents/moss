package kernel

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/mossagents/moss/kernel/model"
	kruntime "github.com/mossagents/moss/kernel/runtime"
)

// ────────────────────────────────────────────────────────────────────
// PromptLayerRegistry — PromptAssembler 的 PromptLayerProvider 替代品（阶段 4）
// ────────────────────────────────────────────────────────────────────

// promptLayerEntry 描述一个已注册的 prompt layer builder。
type promptLayerEntry struct {
	layerID  string
	priority int
	scope    string
	fn       func(*Kernel) (string, bool)
}

// PromptLayerRegistry 管理运行时动态 prompt layer 构建器。
//
// 各 harness 组件在 kernel 初始化阶段调用 Add() 注册 builder；
// 在调用 RunAgentFromBlueprint 前，调用 Build(k) 收集所有 layer。
//
// 这是 PromptAssembler 的直接替代品（§阶段4），保持相同的生命周期语义，
// 但返回结构化的 PromptLayerProvider 而非拼接的字符串。
type PromptLayerRegistry struct {
	mu      sync.RWMutex
	entries []promptLayerEntry
	frozen  bool
}

func newPromptLayerRegistry() *PromptLayerRegistry {
	return &PromptLayerRegistry{}
}

// Add 注册一个 layer builder。
// layerID 唯一标识此 layer（用于去重和审计）；
// priority 决定排序（越大越靠后，与 PromptAssembler.Add 的 order 语义相同）；
// scope 决定 layer 层级，通常为 "system"；
// fn 在每次 Build() 时调用，返回 (content, ok)——ok=false 或 content 为空则跳过。
//
// 若 Registry 已冻结（Boot 阶段结束后），返回错误。
func (r *PromptLayerRegistry) Add(layerID string, priority int, scope string, fn func(*Kernel) (string, bool)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("PromptLayerRegistry is frozen: cannot register layer %q after boot", layerID)
	}
	r.entries = append(r.entries, promptLayerEntry{
		layerID:  layerID,
		priority: priority,
		scope:    scope,
		fn:       fn,
	})
	return nil
}

// Build 收集所有已注册 builder 的输出，按 priority 升序排列，返回 PromptLayerProvider 切片。
// 调用方通常在执行 RunAgentFromBlueprint 前调用此方法。
func (r *PromptLayerRegistry) Build(k *Kernel) []kruntime.PromptLayerProvider {
	r.mu.RLock()
	entries := append([]promptLayerEntry(nil), r.entries...)
	r.mu.RUnlock()

	// 按 priority 升序排序（低 priority 先出现）
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].priority < entries[j].priority
	})

	var layers []kruntime.PromptLayerProvider
	for _, e := range entries {
		content, ok := e.fn(k)
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		layers = append(layers, kruntime.PromptLayerProvider{
			LayerID:          e.layerID,
			Scope:            e.scope,
			Priority:         e.priority,
			ContentParts:     []model.ContentPart{model.TextPart(content)},
			PersistenceScope: kruntime.PersistenceScopePersistent,
			Provenance:       "kernel-prompt-layer-registry",
		})
	}
	return layers
}

// freeze 在 Boot 完成后调用，禁止进一步注册（与 PromptAssembler.freeze 对称）。
func (r *PromptLayerRegistry) freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
}

// BuildSystemPromptFromLayers 将 k 的 PromptLayerRegistry 输出拼接为单一 system prompt 字符串。
// 主要供测试使用；生产路径请直接使用 Build(k) 返回的 PromptLayerProvider 切片。
func BuildSystemPromptFromLayers(k *Kernel) string {
	var sysPrompt string
	for _, layer := range k.PromptLayers().Build(k) {
		if t := model.ContentPartsToPlainText(layer.ContentParts); t != "" {
			if sysPrompt != "" {
				sysPrompt += "\n\n" + t
			} else {
				sysPrompt = t
			}
		}
	}
	return sysPrompt
}
