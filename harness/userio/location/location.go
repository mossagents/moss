package location

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	runtimepkg "runtime"
	"strconv"
	"strings"
)

func OpenWorkspacePath(workspace, spec string) (string, error) {
	path, line := ParseLocationSpec(spec)
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is empty")
	}
	if !filepath.IsAbs(path) && strings.TrimSpace(workspace) != "" {
		path = filepath.Join(workspace, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("open target %s: %w", abs, err)
	}
	if editor, err := exec.LookPath("code"); err == nil {
		target := abs
		if line > 0 {
			target = fmt.Sprintf("%s:%d", abs, line)
		}
		if err := exec.Command(editor, "-g", target).Start(); err != nil {
			return "", fmt.Errorf("launch VS Code: %w", err)
		}
		return fmt.Sprintf("Opened %s in VS Code.", target), nil
	}
	if custom := strings.TrimSpace(os.Getenv("EDITOR")); custom != "" {
		parts := strings.Fields(custom)
		args := append(parts[1:], abs)
		if err := exec.Command(parts[0], args...).Start(); err != nil {
			return "", fmt.Errorf("launch editor %s: %w", parts[0], err)
		}
		return fmt.Sprintf("Opened %s in %s.", abs, parts[0]), nil
	}
	switch runtimepkg.GOOS {
	case "windows":
		if err := exec.Command("cmd", "/c", "start", "", abs).Start(); err != nil {
			return "", fmt.Errorf("launch default editor: %w", err)
		}
	case "darwin":
		if err := exec.Command("open", abs).Start(); err != nil {
			return "", fmt.Errorf("launch default editor: %w", err)
		}
	default:
		if err := exec.Command("xdg-open", abs).Start(); err != nil {
			return "", fmt.Errorf("launch default editor: %w", err)
		}
	}
	return fmt.Sprintf("Opened %s with the default editor.", abs), nil
}

func ParseLocationSpec(spec string) (string, int) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", 0
	}
	idx := strings.LastIndex(spec, ":")
	if idx <= 0 || idx >= len(spec)-1 {
		return spec, 0
	}
	line, err := strconv.Atoi(spec[idx+1:])
	if err != nil || line <= 0 {
		return spec, 0
	}
	return spec[:idx], line
}
