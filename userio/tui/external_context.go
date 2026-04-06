package tui

import (
	"fmt"
	"mime"
	"net/http"
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
	_ = workspace
	return input, nil
}

func buildUserContentParts(input, workspace string) ([]port.ContentPart, error) {
	_, _, parts, err := buildComposerSubmission(input, workspace, nil)
	return parts, err
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

func isMediaPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	case ".wav", ".mp3", ".mpeg", ".m4a", ".ogg", ".flac":
		return true
	case ".mp4", ".webm", ".mov", ".avi", ".mkv":
		return true
	default:
		return false
	}
}

func detectMediaPart(path string, data []byte) (port.ContentPartType, string, error) {
	extMIME := strings.ToLower(strings.TrimSpace(mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))))
	sniffMIME := strings.ToLower(strings.TrimSpace(http.DetectContentType(data)))
	extFamily := mediaFamily(extMIME)
	sniffFamily := mediaFamily(sniffMIME)

	if extFamily != "" && sniffFamily != "" && extFamily != sniffFamily {
		return "", "", fmt.Errorf("mentioned media %s has mime mismatch: extension=%q, content=%q", path, extMIME, sniffMIME)
	}

	family := extFamily
	if family == "" {
		family = sniffFamily
	}
	if family == "" {
		return "", "", fmt.Errorf("mentioned media %s has unsupported or ambiguous media type (extension=%q, content=%q)", path, extMIME, sniffMIME)
	}

	resolvedMIME := extMIME
	if mediaFamily(resolvedMIME) != family {
		resolvedMIME = sniffMIME
	}
	if mediaFamily(resolvedMIME) != family {
		switch family {
		case "image":
			resolvedMIME = "image/png"
		case "audio":
			resolvedMIME = "audio/wav"
		case "video":
			resolvedMIME = "video/mp4"
		}
	}

	switch family {
	case "image":
		return port.ContentPartInputImage, resolvedMIME, nil
	case "audio":
		return port.ContentPartInputAudio, resolvedMIME, nil
	case "video":
		return port.ContentPartInputVideo, resolvedMIME, nil
	default:
		return "", "", fmt.Errorf("mentioned media %s has unsupported media family %q", path, family)
	}
}

func mediaFamily(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	default:
		return ""
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
