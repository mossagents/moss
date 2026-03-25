package sandbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/mossagi/moss/kernel/port"
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
	p = strings.ReplaceAll(p, "\\", "/")
	// 防止路径逃逸
	if strings.Contains(p, "..") {
		return "", fmt.Errorf("path %q contains '..' which is not allowed in scoped workspace", p)
	}
	p = strings.TrimPrefix(p, "/")
	return s.prefix + p, nil
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
