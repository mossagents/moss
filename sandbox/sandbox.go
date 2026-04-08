package sandbox

import (
	"context"
	kws "github.com/mossagents/moss/kernel/workspace"
	"time"
)

// Sandbox 是安全隔离层，所有文件与命令操作必须经由此接口。
type Sandbox interface {
	// ResolvePath 解析并验证路径，防止路径逃逸。
	ResolvePath(path string) (string, error)
	// ListFiles 按 glob pattern 列出文件。
	ListFiles(pattern string) ([]string, error)
	// ReadFile 读取文件内容。
	ReadFile(path string) ([]byte, error)
	// WriteFile 写入文件内容。
	WriteFile(path string, content []byte) error
	// Execute 执行命令。
	Execute(ctx context.Context, req kws.ExecRequest) (kws.ExecOutput, error)
	// Limits 返回当前资源限制。
	Limits() ResourceLimits
}

// Output 是命令执行的结果。
type Output = kws.ExecOutput

// ResourceLimits 表示 sandbox 的资源限制。
type ResourceLimits struct {
	MaxFileSize    int64         `json:"max_file_size"`
	CommandTimeout time.Duration `json:"command_timeout"`
	AllowedPaths   []string      `json:"allowed_paths"`
}
