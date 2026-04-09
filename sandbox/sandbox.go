package sandbox

import (
	"context"
	"fmt"
	"time"

	kws "github.com/mossagents/moss/kernel/workspace"
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
// Zero values for limit fields mean "unlimited" (no enforcement).
type ResourceLimits struct {
	MaxFileSize    int64         `json:"max_file_size"`
	CommandTimeout time.Duration `json:"command_timeout"`
	AllowedPaths   []string      `json:"allowed_paths"`

	MaxMemoryBytes int64          `json:"max_memory_bytes,omitempty"`
	MaxCPUPercent  int            `json:"max_cpu_percent,omitempty"`
	MaxProcesses   int            `json:"max_processes,omitempty"`
	MaxOpenFiles   int            `json:"max_open_files,omitempty"`
	NetworkPolicy  NetworkPolicy  `json:"network_policy,omitempty"`
	IsolationLevel IsolationLevel `json:"isolation_level,omitempty"`
	MaxDiskBytes   int64          `json:"max_disk_bytes,omitempty"`
	ReadOnly       bool           `json:"read_only,omitempty"`
}

// NetworkPolicy defines network access rules for a sandbox.
type NetworkPolicy struct {
	AllowOutbound bool     `json:"allow_outbound"`
	AllowedHosts  []string `json:"allowed_hosts,omitempty"`
	BlockedPorts  []int    `json:"blocked_ports,omitempty"`
}

// IsolationLevel describes the degree of isolation a sandbox provides.
type IsolationLevel string

const (
	IsolationNone      IsolationLevel = ""          // no isolation (local sandbox)
	IsolationProcess   IsolationLevel = "process"   // process-level isolation
	IsolationContainer IsolationLevel = "container" // container-level isolation (Docker)
	IsolationVM        IsolationLevel = "vm"        // VM-level isolation
)

// ResourceUsage represents a snapshot of current resource consumption.
type ResourceUsage struct {
	MemoryBytes   int64         `json:"memory_bytes"`
	CPUPercent    float64       `json:"cpu_percent"`
	DiskBytes     int64         `json:"disk_bytes"`
	ProcessCount  int           `json:"process_count"`
	OpenFileCount int           `json:"open_file_count"`
	Uptime        time.Duration `json:"uptime"`
}

// ResourceMonitor provides resource usage monitoring for sandboxes that support it.
type ResourceMonitor interface {
	Usage(ctx context.Context) (ResourceUsage, error)
}

// LimitsExceeded checks if resource usage exceeds the configured limits.
// Returns a list of human-readable violation descriptions (empty if within limits).
// Zero-valued limits are treated as unlimited.
func LimitsExceeded(limits ResourceLimits, usage ResourceUsage) []string {
	var violations []string

	if limits.MaxMemoryBytes > 0 && usage.MemoryBytes > limits.MaxMemoryBytes {
		violations = append(violations, fmt.Sprintf(
			"memory usage %d bytes exceeds limit %d bytes", usage.MemoryBytes, limits.MaxMemoryBytes,
		))
	}
	if limits.MaxCPUPercent > 0 && usage.CPUPercent > float64(limits.MaxCPUPercent) {
		violations = append(violations, fmt.Sprintf(
			"CPU usage %.1f%% exceeds limit %d%%", usage.CPUPercent, limits.MaxCPUPercent,
		))
	}
	if limits.MaxDiskBytes > 0 && usage.DiskBytes > limits.MaxDiskBytes {
		violations = append(violations, fmt.Sprintf(
			"disk usage %d bytes exceeds limit %d bytes", usage.DiskBytes, limits.MaxDiskBytes,
		))
	}
	if limits.MaxProcesses > 0 && usage.ProcessCount > limits.MaxProcesses {
		violations = append(violations, fmt.Sprintf(
			"process count %d exceeds limit %d", usage.ProcessCount, limits.MaxProcesses,
		))
	}
	if limits.MaxOpenFiles > 0 && usage.OpenFileCount > limits.MaxOpenFiles {
		violations = append(violations, fmt.Sprintf(
			"open file count %d exceeds limit %d", usage.OpenFileCount, limits.MaxOpenFiles,
		))
	}

	return violations
}
