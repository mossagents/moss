package prompt

import (
	"embed"
	"fmt"
	"io/fs"
)

// defaultPromptsFS 是内置 prompt 文件的嵌入文件系统。
//
//go:embed prompts
var defaultPromptsFS embed.FS

// EmbedLoader 从 embed.FS 加载 prompt 模板。
// 支持多级嵌套目录，与 FSLoader 格式完全一致。
type EmbedLoader struct {
	FS     fs.FS
	Prefix string // 可选：子目录前缀（默认 "."）
}

// Load 扫描嵌入 FS 中的所有 .yaml / .yml 文件，返回已解析的模板列表。
func (l *EmbedLoader) Load() ([]PromptTemplate, error) {
	root := l.FS
	prefix := l.Prefix
	if prefix == "" {
		prefix = "."
	}
	// 提取子目录视图（如果 Prefix 非 "."）
	if prefix != "." {
		sub, err := fs.Sub(l.FS, prefix)
		if err != nil {
			return nil, fmt.Errorf("embed loader: sub dir %q: %w", prefix, err)
		}
		root = sub
	}
	// 复用 FSLoader 的解析逻辑
	inner := &FSLoader{Root: root}
	return inner.Load()
}

// DefaultRegistry 返回加载了内置 prompt 模板的 MemoryRegistry。
//
// 内置 prompt 文件位于 kernel/prompt/prompts/ 目录，通过 go:embed 打包进二进制。
// 调用方可以继续调用 registry.Register() 注册自定义 prompt 覆盖内置版本。
func DefaultRegistry() (*MemoryRegistry, error) {
	sub, err := fs.Sub(defaultPromptsFS, "prompts")
	if err != nil {
		return nil, fmt.Errorf("prompt: open embedded prompts dir: %w", err)
	}
	loader := &EmbedLoader{FS: sub}
	templates, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("prompt: load embedded prompts: %w", err)
	}
	reg := NewMemoryRegistry()
	for _, t := range templates {
		if err := reg.Register(t); err != nil {
			return nil, fmt.Errorf("prompt: register %q: %w", t.ID, err)
		}
	}
	return reg, nil
}

// MustDefaultRegistry 与 DefaultRegistry 相同，但在失败时 panic。
// 用于 init() 或全局变量初始化场景。
func MustDefaultRegistry() *MemoryRegistry {
	reg, err := DefaultRegistry()
	if err != nil {
		panic("prompt: failed to load default registry: " + err.Error())
	}
	return reg
}
