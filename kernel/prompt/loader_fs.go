package prompt

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FSLoader 从文件系统加载 prompt 模板。
// 支持 .yaml / .yml 格式，按目录递归扫描。
type FSLoader struct {
	Root fs.FS
}

// Load 扫描 Root 下所有 .yaml/.yml 文件，返回已解析的模板列表。
func (l *FSLoader) Load() ([]PromptTemplate, error) {
	var templates []PromptTemplate
	err := fs.WalkDir(l.Root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		data, err := fs.ReadFile(l.Root, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		var tmpl PromptTemplate
		if err := yaml.Unmarshal(data, &tmpl); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if tmpl.ID == "" || tmpl.Template == "" {
			return nil // 跳过不完整的文件
		}
		templates = append(templates, tmpl)
		return nil
	})
	return templates, err
}

// Build 加载所有模板并返回已填充的 Registry。
func (l *FSLoader) Build() (*MemoryRegistry, error) {
	templates, err := l.Load()
	if err != nil {
		return nil, err
	}
	reg := NewMemoryRegistry()
	for _, t := range templates {
		if err := reg.Register(t); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
