package workspace

// Capabilities 报告 Workspace 实现的实际隔离能力。
// 调用方（policy 层、工具）据此决定降级策略。
type Capabilities struct {
	FileSystemIsolation IsolationMethod `json:"file_system_isolation"`
	NetworkIsolation    IsolationMethod `json:"network_isolation"`
	ProcessIsolation    IsolationMethod `json:"process_isolation"`
	ResourceEnforcement bool            `json:"resource_enforcement"`
}

// IsolationMethod 描述具体隔离实现方式。
type IsolationMethod string

const (
	IsolationMethodNone      IsolationMethod = "none"       // 无隔离
	IsolationMethodPathCheck IsolationMethod = "path_check" // 用户态路径检查
	IsolationMethodLandlock  IsolationMethod = "landlock"   // Linux Landlock LSM
	IsolationMethodNamespace IsolationMethod = "namespace"  // Linux mount/net namespace
	IsolationMethodSeatbelt  IsolationMethod = "seatbelt"   // macOS sandbox-exec
	IsolationMethodContainer IsolationMethod = "container"  // Docker/OCI 容器
	IsolationMethodFirewall  IsolationMethod = "firewall"   // Windows firewall
	IsolationMethodProxy     IsolationMethod = "proxy"      // 环境变量 proxy（可绕过）
)
