package eval

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"math"
	"os"
	"path/filepath"
	"strings"
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
	if err := validateCase(c, path); err != nil {
		return EvalCase{}, err
	}
	return c, nil
}

func validateCase(c EvalCase, path string) error {
	if strings.TrimSpace(c.ID) == "" {
		return casePathError(path, "id", "must not be empty")
	}
	if len(c.Input.Messages) == 0 && len(c.Input.RawMessages) == 0 {
		return casePathError(path, "input.messages", "must contain at least one message")
	}
	for i, m := range c.Input.RawMessages {
		if strings.TrimSpace(m.Role) == "" {
			return casePathError(path, fmt.Sprintf("input.messages[%d].role", i), "must not be empty")
		}
		if strings.TrimSpace(m.Content) == "" {
			return casePathError(path, fmt.Sprintf("input.messages[%d].content", i), "must not be empty")
		}
	}
	if !hasExpectConstraint(c.Expect) {
		return casePathError(path, "expect", "must define at least one assertion")
	}
	for name, weight := range c.Scoring.Weights {
		if strings.TrimSpace(name) == "" {
			return casePathError(path, "scoring.weights", "judge name must not be empty")
		}
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
			return casePathError(path, fmt.Sprintf("scoring.weights[%q]", name), "must be > 0")
		}
	}
	return nil
}

func hasExpectConstraint(expect EvalExpect) bool {
	return len(expect.Contains) > 0 ||
		len(expect.NotContains) > 0 ||
		len(expect.ToolCalled) > 0 ||
		len(expect.ToolNot) > 0 ||
		expect.MaxSteps > 0 ||
		expect.Judge != nil
}

func casePathError(path, field, msg string) error {
	return fmt.Errorf("eval: validate case %s: %s %s", path, field, msg)
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
