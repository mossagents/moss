package port

import (
	"context"
	"time"
)

// Workspace 是 Agent 工作区的抽象层。
// 将文件系统操作从 Sandbox 中解耦，使不同部署场景
// （本地、Docker、云存储、内存虚拟文件系统）可以提供各自的实现。
type Workspace interface {
	// ReadFile 从工作区读取文件。
	ReadFile(ctx context.Context, path string) ([]byte, error)
	// WriteFile 向工作区写入文件。
	WriteFile(ctx context.Context, path string, content []byte) error
	// ListFiles 按 glob 模式列出文件。
	ListFiles(ctx context.Context, pattern string) ([]string, error)
	// Stat 获取文件元信息。找不到时返回 ErrNotExist。
	Stat(ctx context.Context, path string) (FileInfo, error)
	// DeleteFile 删除指定文件。
	DeleteFile(ctx context.Context, path string) error
}

// FileInfo 描述文件元信息。
type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
}

// Executor 是命令执行的抽象层。
// 与 Workspace 正交：可组合不同的 Workspace + Executor 实现。
type Executor interface {
	// Execute 在隔离环境中执行命令。
	Execute(ctx context.Context, cmd string, args []string) (ExecOutput, error)
}

// ExecOutput 是命令执行的结果。
type ExecOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// NoOpExecutor 拒绝所有命令执行，用于纯对话场景。
type NoOpExecutor struct{}

func (NoOpExecutor) Execute(_ context.Context, cmd string, _ []string) (ExecOutput, error) {
	return ExecOutput{}, &executorDisabledError{cmd: cmd}
}

type executorDisabledError struct {
	cmd string
}

func (e *executorDisabledError) Error() string {
	return "command execution is disabled: " + e.cmd
}
