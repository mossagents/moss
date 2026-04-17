package workspace

// SecurityPolicy 是 Workspace 的安全策略。
// 由 Workspace 实现在每次文件/命令操作时强制执行。
type SecurityPolicy struct {
	FileRules        []FileAccessRule `json:"file_rules,omitempty"`
	ProtectedPaths   []string         `json:"protected_paths,omitempty"`    // 强制只读子路径（如 .git/）
	DenyReadPatterns []string         `json:"deny_read_patterns,omitempty"` // 拒绝读取的 glob（如 **/.env）
	NetworkMode      ExecNetworkMode  `json:"network_mode,omitempty"`
	AllowedHosts     []string         `json:"allowed_hosts,omitempty"`
	ReadOnly         bool             `json:"read_only,omitempty"` // 整个 workspace 只读
}

// FileAccessMode 描述文件访问级别。
type FileAccessMode string

const (
	FileAccessRead  FileAccessMode = "read"
	FileAccessWrite FileAccessMode = "write"
	FileAccessNone  FileAccessMode = "none" // deny-read
)

// FileAccessRule 描述单条文件访问规则。
type FileAccessRule struct {
	Path   string         `json:"path"` // 精确路径或 glob
	Access FileAccessMode `json:"access"`
}
