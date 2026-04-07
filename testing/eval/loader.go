package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadCase 从 YAML 文件加载单个 EvalCase。
func LoadCase(path string) (EvalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return EvalCase{}, fmt.Errorf("eval: load case %s: %w", path, err)
	}
	var c EvalCase
	if err := yaml.Unmarshal(data, &c); err != nil {
		return EvalCase{}, fmt.Errorf("eval: parse case %s: %w", path, err)
	}
	if c.ID == "" {
		c.ID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return c, nil
}

// LoadDir 递归加载目录下所有 .yaml / .yml 文件作为 EvalCase。
func LoadDir(dir string) ([]EvalCase, error) {
	var cases []EvalCase
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
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
		c, err := LoadCase(path)
		if err != nil {
			return err
		}
		cases = append(cases, c)
		return nil
	})
	return cases, err
}
