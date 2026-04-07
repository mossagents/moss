package prompt

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// PromptTemplate 描述一个可渲染的 prompt 模板。
type PromptTemplate struct {
	ID          string            `yaml:"id"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description,omitempty"`
	Tags        []string          `yaml:"tags,omitempty"`
	Template    string            `yaml:"template"`
	Defaults    map[string]any    `yaml:"defaults,omitempty"`
	Partials    []string          `yaml:"partials,omitempty"`
	ModelHints  map[string]string `yaml:"model_hints,omitempty"`
}

// PromptRender 是渲染后的 prompt 结果。
type PromptRender struct {
	Content  string
	Tokens   int
	Metadata map[string]any
}

// Registry 管理 prompt 模板的注册和渲染。
type Registry interface {
	Get(id string) (*PromptTemplate, error)
	GetVersion(id, version string) (*PromptTemplate, error)
	Render(id string, vars map[string]any) (PromptRender, error)
	Register(t PromptTemplate) error
	List(tags ...string) []string
}

// MemoryRegistry 是 Registry 的内存实现。
type MemoryRegistry struct {
	templates map[string][]PromptTemplate // id -> versions (newest last)
}

// NewMemoryRegistry 创建一个空的内存 Registry。
func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{templates: make(map[string][]PromptTemplate)}
}

// Register 注册一个模板。相同 ID 的不同版本可以共存。
func (r *MemoryRegistry) Register(t PromptTemplate) error {
	if t.ID == "" {
		return fmt.Errorf("prompt template ID cannot be empty")
	}
	if t.Template == "" {
		return fmt.Errorf("prompt template %q: Template cannot be empty", t.ID)
	}
	r.templates[t.ID] = append(r.templates[t.ID], t)
	return nil
}

// Get 返回指定 ID 的最新版本模板。
func (r *MemoryRegistry) Get(id string) (*PromptTemplate, error) {
	versions, ok := r.templates[id]
	if !ok || len(versions) == 0 {
		return nil, fmt.Errorf("prompt template %q not found", id)
	}
	t := versions[len(versions)-1]
	return &t, nil
}

// GetVersion 返回指定 ID 和版本的模板。
func (r *MemoryRegistry) GetVersion(id, version string) (*PromptTemplate, error) {
	versions, ok := r.templates[id]
	if !ok {
		return nil, fmt.Errorf("prompt template %q not found", id)
	}
	for _, t := range versions {
		if t.Version == version {
			cp := t
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("prompt template %q version %q not found", id, version)
}

// Render 渲染指定 ID 的模板，使用提供的变量覆盖默认值。
func (r *MemoryRegistry) Render(id string, vars map[string]any) (PromptRender, error) {
	tmpl, err := r.Get(id)
	if err != nil {
		return PromptRender{}, err
	}
	return r.renderTemplate(tmpl, vars)
}

// List 返回所有已注册的模板 ID，可按 tag 过滤。
func (r *MemoryRegistry) List(tags ...string) []string {
	out := make([]string, 0, len(r.templates))
	for id, versions := range r.templates {
		if len(versions) == 0 {
			continue
		}
		if len(tags) == 0 {
			out = append(out, id)
			continue
		}
		latest := versions[len(versions)-1]
		if hasAnyTag(latest.Tags, tags) {
			out = append(out, id)
		}
	}
	return out
}

// renderTemplate 将模板和变量合并后渲染。
func (r *MemoryRegistry) renderTemplate(tmpl *PromptTemplate, vars map[string]any) (PromptRender, error) {
	// 合并变量：defaults 优先级低于调用者传入的 vars
	merged := make(map[string]any, len(tmpl.Defaults)+len(vars))
	for k, v := range tmpl.Defaults {
		merged[k] = v
	}
	for k, v := range vars {
		merged[k] = v
	}

	// 构建 Go template，注册 partials
	t := template.New(tmpl.ID).Option("missingkey=zero")
	for _, partialID := range tmpl.Partials {
		partial, err := r.Get(partialID)
		if err != nil {
			return PromptRender{}, fmt.Errorf("partial %q not found for template %q: %w", partialID, tmpl.ID, err)
		}
		if _, err := t.New(partialID).Parse(partial.Template); err != nil {
			return PromptRender{}, fmt.Errorf("parse partial %q: %w", partialID, err)
		}
	}

	if _, err := t.Parse(tmpl.Template); err != nil {
		return PromptRender{}, fmt.Errorf("parse template %q: %w", tmpl.ID, err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, merged); err != nil {
		return PromptRender{}, fmt.Errorf("render template %q: %w", tmpl.ID, err)
	}

	content := buf.String()
	return PromptRender{
		Content: content,
		Tokens:  estimateTokens(content),
		Metadata: map[string]any{
			"template_id":      tmpl.ID,
			"template_version": tmpl.Version,
		},
	}, nil
}

func hasAnyTag(haystack, needles []string) bool {
	for _, needle := range needles {
		for _, h := range haystack {
			if h == needle {
				return true
			}
		}
	}
	return false
}

// estimateTokens 简单估算 token 数（按空格分词）。
func estimateTokens(s string) int {
	return len(strings.Fields(s))
}
