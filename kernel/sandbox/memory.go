package sandbox

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mossagi/moss/kernel/port"
)

// MemoryWorkspace 是基于内存的 Workspace 实现。
// 适用于：测试、短生命周期 Agent、miniroom 每房间独立文件系统。
// 线程安全。
type MemoryWorkspace struct {
	mu       sync.RWMutex
	files    map[string]memFile
	maxTotal int64 // 总容量限制（字节），0 表示无限制
	used     int64 // 当前已用字节
}

type memFile struct {
	content []byte
	modTime time.Time
}

// MemoryWorkspaceOption 配置 MemoryWorkspace。
type MemoryWorkspaceOption func(*MemoryWorkspace)

// WithMaxTotalSize 设置工作区总容量限制。
func WithMaxTotalSize(n int64) MemoryWorkspaceOption {
	return func(m *MemoryWorkspace) { m.maxTotal = n }
}

// NewMemoryWorkspace 创建内存工作区。
func NewMemoryWorkspace(opts ...MemoryWorkspaceOption) *MemoryWorkspace {
	m := &MemoryWorkspace{files: make(map[string]memFile)}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *MemoryWorkspace) ReadFile(_ context.Context, filePath string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p := normalizePath(filePath)
	f, ok := m.files[p]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", p)
	}
	out := make([]byte, len(f.content))
	copy(out, f.content)
	return out, nil
}

func (m *MemoryWorkspace) WriteFile(_ context.Context, filePath string, content []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p := normalizePath(filePath)

	// 计算容量变化
	var oldSize int64
	if old, ok := m.files[p]; ok {
		oldSize = int64(len(old.content))
	}
	newSize := int64(len(content))
	delta := newSize - oldSize

	if m.maxTotal > 0 && m.used+delta > m.maxTotal {
		return fmt.Errorf("workspace capacity exceeded: used=%d, need=%d, limit=%d", m.used, m.used+delta, m.maxTotal)
	}

	data := make([]byte, len(content))
	copy(data, content)
	m.files[p] = memFile{content: data, modTime: time.Now()}
	m.used += delta
	return nil
}

func (m *MemoryWorkspace) ListFiles(_ context.Context, pattern string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p := normalizePath(pattern)
	var matches []string
	for name := range m.files {
		ok, _ := path.Match(p, name)
		if ok {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches, nil
}

func (m *MemoryWorkspace) Stat(_ context.Context, filePath string) (port.FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p := normalizePath(filePath)
	f, ok := m.files[p]
	if !ok {
		return port.FileInfo{}, fmt.Errorf("file not found: %s", p)
	}
	return port.FileInfo{
		Name:    path.Base(p),
		Size:    int64(len(f.content)),
		IsDir:   false,
		ModTime: f.modTime,
	}, nil
}

func (m *MemoryWorkspace) DeleteFile(_ context.Context, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p := normalizePath(filePath)
	f, ok := m.files[p]
	if !ok {
		return fmt.Errorf("file not found: %s", p)
	}
	m.used -= int64(len(f.content))
	delete(m.files, p)
	return nil
}

// normalizePath 将路径规范化为 forward-slash 格式。
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "/")
	return p
}
