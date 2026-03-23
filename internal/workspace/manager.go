package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type TrustLevel string

const (
	TrustLevelTrusted    TrustLevel = "trusted"
	TrustLevelRestricted TrustLevel = "restricted"
)

type Manager struct {
	Root  string
	Trust TrustLevel
}

func New(root string, trust TrustLevel) *Manager {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return &Manager{Root: abs, Trust: trust}
}

func (m *Manager) ResolvePath(p string) (string, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(m.Root, p)
	}
	clean := filepath.Clean(p)
	// Ensure the cleaned path is the root itself or a child by checking for a separator suffix
	root := m.Root
	if !strings.HasPrefix(clean, root+string(filepath.Separator)) && clean != root {
		return "", fmt.Errorf("path %q escapes workspace root", p)
	}
	return clean, nil
}

func (m *Manager) ListFiles(pattern string) ([]string, error) {
	full := filepath.Join(m.Root, pattern)
	matches, err := filepath.Glob(full)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, match := range matches {
		rel, err := filepath.Rel(m.Root, match)
		if err == nil {
			result = append(result, rel)
		} else {
			result = append(result, match)
		}
	}
	return result, nil
}

func (m *Manager) ReadFile(p string) (string, error) {
	resolved, err := m.ResolvePath(p)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *Manager) WriteFile(p, content string) error {
	resolved, err := m.ResolvePath(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return err
	}
	return os.WriteFile(resolved, []byte(content), 0644)
}

func (m *Manager) RunCommand(cmd string, args []string, timeout time.Duration) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, cmd, args...)
	c.Dir = m.Root
	out, err := c.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return string(out), -1, err
		}
	}
	return string(out), exitCode, nil
}
