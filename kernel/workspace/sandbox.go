package workspace

import (
	"context"
	"fmt"
	"time"
)

// ResourceLimits 表示 workspace 的资源限制。
// Zero values for limit fields mean "unlimited" (no enforcement).
type ResourceLimits struct {
	MaxFileSize    int64         `json:"max_file_size"`
	CommandTimeout time.Duration `json:"command_timeout"`
	AllowedPaths   []string      `json:"allowed_paths"`

	MaxOutputBytes int64         `json:"max_output_bytes,omitempty"`
	IODrainTimeout time.Duration `json:"io_drain_timeout,omitempty"`

	MaxMemoryBytes int64                 `json:"max_memory_bytes,omitempty"`
	MaxCPUPercent  int                   `json:"max_cpu_percent,omitempty"`
	MaxProcesses   int                   `json:"max_processes,omitempty"`
	MaxOpenFiles   int                   `json:"max_open_files,omitempty"`
	NetworkPolicy  NetworkPolicy         `json:"network_policy,omitempty"`
	IsolationLevel SandboxIsolationLevel `json:"isolation_level,omitempty"`
	MaxDiskBytes   int64                 `json:"max_disk_bytes,omitempty"`
	ReadOnly       bool                  `json:"read_only,omitempty"`
}

// NetworkPolicy 定义 sandbox 网络访问规则。
type NetworkPolicy struct {
	AllowOutbound bool     `json:"allow_outbound"`
	AllowedHosts  []string `json:"allowed_hosts,omitempty"`
	BlockedPorts  []int    `json:"blocked_ports,omitempty"`
}

// SandboxIsolationLevel 描述 sandbox 提供的隔离能力级别。
// 与 IsolationLevel（执行请求的隔离需求）不同，此类型描述 sandbox 的隔离实现能力。
type SandboxIsolationLevel string

const (
	SandboxIsolationNone      SandboxIsolationLevel = ""          // 无隔离（本地 sandbox）
	SandboxIsolationProcess   SandboxIsolationLevel = "process"   // 进程级隔离
	SandboxIsolationContainer SandboxIsolationLevel = "container" // 容器级隔离（Docker）
	SandboxIsolationVM        SandboxIsolationLevel = "vm"        // VM 级隔离
)

// ResourceUsage 表示 sandbox 当前资源消耗快照。
type ResourceUsage struct {
	MemoryBytes   int64         `json:"memory_bytes"`
	CPUPercent    float64       `json:"cpu_percent"`
	DiskBytes     int64         `json:"disk_bytes"`
	ProcessCount  int           `json:"process_count"`
	OpenFileCount int           `json:"open_file_count"`
	Uptime        time.Duration `json:"uptime"`
}

// ResourceMonitor 为支持的 sandbox 提供资源使用监控能力。
type ResourceMonitor interface {
	Usage(ctx context.Context) (ResourceUsage, error)
}

// LimitsExceeded 检查资源使用是否超过配置限制。
// 返回人类可读的违规描述列表（为空则表示在限制范围内）。
// Zero-valued limits 视为 unlimited（不强制执行）。
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
