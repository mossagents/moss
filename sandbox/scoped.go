package sandbox

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/mossagents/moss/kernel/port"
)

// ScopedWorkspace 为 Workspace 添加路径前缀隔离。
// 适用于多租户场景：不同用户/房间共享底层存储，但文件路径互相隔离。
type ScopedWorkspace struct {
	prefix string
	inner  port.Workspace
}

// NewScopedWorkspace 创建带前缀隔离的 Workspace。
// prefix 不应以 "/" 开头，会自动规范化。
func NewScopedWorkspace(prefix string, inner port.Workspace) *ScopedWorkspace {
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &ScopedWorkspace{prefix: prefix, inner: inner}
}

func (s *ScopedWorkspace) scopedPath(p string) (string, error) {
	// Normalize path separators and strip leading slash.
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "/")

	// Clean the path to resolve any "." or ".." segments before checking.
	// path.Clean operates on slash-separated paths (not OS-specific).
	cleaned := path.Clean("/" + p) // prepend "/" so Clean doesn't strip the root

	// Reject any path that escapes the scoped root after normalization.
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return "", fmt.Errorf("path %q escapes scoped workspace", p)
		}
	}

	// Strip the leading "/" re-added for cleaning, then apply the prefix.
	rel := strings.TrimPrefix(cleaned, "/")
	return s.prefix + rel, nil
}

func (s *ScopedWorkspace) ReadFile(ctx context.Context, path string) ([]byte, error) {
	sp, err := s.scopedPath(path)
	if err != nil {
		return nil, err
	}
	return s.inner.ReadFile(ctx, sp)
}

func (s *ScopedWorkspace) WriteFile(ctx context.Context, path string, content []byte) error {
	sp, err := s.scopedPath(path)
	if err != nil {
		return err
	}
	return s.inner.WriteFile(ctx, sp, content)
}

func (s *ScopedWorkspace) ListFiles(ctx context.Context, pattern string) ([]string, error) {
	sp, err := s.scopedPath(pattern)
	if err != nil {
		return nil, err
	}
	files, err := s.inner.ListFiles(ctx, sp)
	if err != nil {
		return nil, err
	}
	// 从结果中去除前缀
	result := make([]string, 0, len(files))
	for _, f := range files {
		result = append(result, strings.TrimPrefix(f, s.prefix))
	}
	return result, nil
}

func (s *ScopedWorkspace) Stat(ctx context.Context, path string) (port.FileInfo, error) {
	sp, err := s.scopedPath(path)
	if err != nil {
		return port.FileInfo{}, err
	}
	return s.inner.Stat(ctx, sp)
}

func (s *ScopedWorkspace) DeleteFile(ctx context.Context, path string) error {
	sp, err := s.scopedPath(path)
	if err != nil {
		return err
	}
	return s.inner.DeleteFile(ctx, sp)
}
