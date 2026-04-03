package tui

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	runtimepkg "runtime"
	"strconv"
	"strings"

	"github.com/mossagents/moss/kernel/port"
)

const maxAttachedFileBytes = 16 * 1024

func expandInlineFileMentions(input, workspace string) (string, error) {
	if !strings.Contains(input, "@") {
		return input, nil
	}
	tokens := strings.Fields(input)
	attachments := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, token := range tokens {
		if !strings.HasPrefix(token, "@") || len(token) == 1 {
			continue
		}
		path, ok := resolveMentionPath(workspace, strings.TrimPrefix(token, "@"))
		if !ok {
			return "", fmt.Errorf("mentioned file %s not found", token)
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		if isImagePath(path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read mentioned file %s: %w", path, err)
		}
		content := string(data)
		truncated := false
		if len(content) > maxAttachedFileBytes {
			content = content[:maxAttachedFileBytes]
			truncated = true
		}
		content = strings.TrimSpace(content)
		if truncated {
			content += "\n...[truncated]"
		}
		attachments = append(attachments, fmt.Sprintf("--- %s ---\n%s", path, content))
	}
	if len(attachments) == 0 {
		return input, nil
	}
	return strings.TrimSpace(input) + "\n\nAttached context:\n" + strings.Join(attachments, "\n\n"), nil
}

func buildUserContentParts(input, workspace string) ([]port.ContentPart, error) {
	parts := []port.ContentPart{port.TextPart(strings.TrimSpace(input))}
	if !strings.Contains(input, "@") {
		return parts, nil
	}
	tokens := strings.Fields(input)
	seen := make(map[string]struct{})
	for _, token := range tokens {
		if !strings.HasPrefix(token, "@") || len(token) == 1 {
			continue
		}
		path, ok := resolveMentionPath(workspace, strings.TrimPrefix(token, "@"))
		if !ok || !isImagePath(path) {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read image mention %s: %w", path, err)
		}
		mimeType := strings.TrimSpace(mime.TypeByExtension(strings.ToLower(filepath.Ext(path))))
		if !strings.HasPrefix(mimeType, "image/") {
			return nil, fmt.Errorf("mentioned image %s has unsupported mime type %q", path, mimeType)
		}
		parts = append(parts, port.ImageInlinePart(
			port.ContentPartInputImage,
			mimeType,
			base64.StdEncoding.EncodeToString(data),
			path,
		))
	}
	return parts, nil
}

func resolveMentionPath(workspace, raw string) (string, bool) {
	raw = strings.TrimSpace(strings.Trim(raw, `"'.,;:()[]{}<>`))
	if raw == "" {
		return "", false
	}
	candidates := []string{raw}
	if !filepath.IsAbs(raw) && strings.TrimSpace(workspace) != "" {
		candidates = append([]string{filepath.Join(workspace, raw)}, candidates...)
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return candidate, true
		}
		return abs, true
	}
	return "", false
}

func mentionTokenForComposer(workspace, raw string) (string, error) {
	path, ok := resolveMentionPath(workspace, raw)
	if !ok {
		return "", fmt.Errorf("mentioned path %q was not found", strings.TrimSpace(raw))
	}
	if strings.TrimSpace(workspace) != "" {
		if rel, err := filepath.Rel(workspace, path); err == nil && !strings.HasPrefix(rel, "..") {
			return "@" + filepath.Clean(rel), nil
		}
	}
	return "@" + filepath.Clean(path), nil
}

func isImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

func openWorkspacePath(workspace, spec string) (string, error) {
	path, line := parseLocationSpec(spec)
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

func parseLocationSpec(spec string) (string, int) {
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
