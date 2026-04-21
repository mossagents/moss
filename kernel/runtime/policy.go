package runtime

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// ─────────────────────────────────────────────
// PolicyCompiler 接口（§7.1、§14.1）
// ─────────────────────────────────────────────

// PolicyCompiler 接受 RuntimeRequest，输出经编译的 EffectiveToolPolicy（含 PolicyHash）。
// PolicyHash 必须稳定且确定性：相同输入必须产生相同 hash，用于 prompt_materialized 和
// tool_called 事件中的 policy_hash 关联。
type PolicyCompiler interface {
	// Compile 编译 RuntimeRequest 中的权限配置，返回 EffectiveToolPolicy。
	Compile(req RuntimeRequest) (EffectiveToolPolicy, error)
}

// ─────────────────────────────────────────────
// DefaultPolicyCompiler 最小实现
// ─────────────────────────────────────────────

// BuiltinProfile 定义内置权限配置（§14.1 Guardian 契约）。
type BuiltinProfile struct {
	TrustLevel            string
	AllowedTools          []string
	DeniedTools           []string
	ApprovalRequiredTools []string
}

// DefaultPolicyCompiler 是最小可用的 PolicyCompiler 实现。
// 它通过 PermissionProfile 名字查找内置或注册的策略配置，
// 计算确定性 PolicyHash，不依赖任何外部服务。
type DefaultPolicyCompiler struct {
	// profiles 存储注册的策略配置，key 为 PermissionProfile 名。
	profiles map[string]BuiltinProfile
}

// NewDefaultPolicyCompiler 创建带默认内置配置的 DefaultPolicyCompiler。
func NewDefaultPolicyCompiler() *DefaultPolicyCompiler {
	c := &DefaultPolicyCompiler{
		profiles: make(map[string]BuiltinProfile),
	}
	// 注册内置配置（§14.1 trust level 约束）
	c.profiles["read-only"] = BuiltinProfile{
		TrustLevel:   "low",
		AllowedTools: []string{"read_file", "list_dir", "search_files"},
		DeniedTools:  []string{"write_file", "run_command", "delete_file"},
	}
	c.profiles["workspace-write"] = BuiltinProfile{
		TrustLevel:            "medium",
		AllowedTools:          []string{"read_file", "list_dir", "search_files", "write_file", "create_file"},
		ApprovalRequiredTools: []string{"run_command", "delete_file"},
	}
	c.profiles["full"] = BuiltinProfile{
		TrustLevel: "high",
		// 空 AllowedTools 表示允许所有工具
	}
	// 别名：approval mode 名称与 permission profile 名称的双向兼容映射（§阶段4）。
	// "confirm" 等同于 "workspace-write"；"full-auto" 等同于 "full"。
	// 旧路径中 ApplyApprovalModeWithTrust 使用 approval mode 字符串，
	// 新路径中 RuntimeRequest.PermissionProfile 使用 profile 名称；两套名称通过别名对齐。
	c.profiles["confirm"] = c.profiles["workspace-write"]
	c.profiles["full-auto"] = c.profiles["full"]
	c.profiles[""] = c.profiles["workspace-write"] // 默认配置
	return c
}

// RegisterProfile 注册自定义策略配置（允许产品层扩展）。
func (c *DefaultPolicyCompiler) RegisterProfile(name string, profile BuiltinProfile) {
	c.profiles[name] = profile
}

// Compile 实现 PolicyCompiler 接口。
func (c *DefaultPolicyCompiler) Compile(req RuntimeRequest) (EffectiveToolPolicy, error) {
	profile, ok := c.profiles[req.PermissionProfile]
	if !ok {
		// 未知配置降级到最小权限
		profile = c.profiles["read-only"]
	}

	policy := EffectiveToolPolicy{
		TrustLevel:            profile.TrustLevel,
		AllowedTools:          profile.AllowedTools,
		DeniedTools:           profile.DeniedTools,
		ApprovalRequiredTools: profile.ApprovalRequiredTools,
		Raw: map[string]any{
			"profile":         req.PermissionProfile,
			"workspace":       req.Workspace,
			"trust_level":     profile.TrustLevel,
			"workspace_trust": req.WorkspaceTrust, // §阶段4: 存储以供 BlueprintPolicyApplier 重建 ToolPolicy
		},
	}
	policy.PolicyHash = computePolicyHash(policy)
	return policy, nil
}

// computePolicyHash 计算确定性 PolicyHash（§7.1 PolicyHash 约束）。
// 相同策略内容必须产生相同 hash，实现使用 SHA-256 前 16 字节的十六进制表示。
func computePolicyHash(p EffectiveToolPolicy) string {
	// 对列表排序以保证确定性
	allowed := sortedCopy(p.AllowedTools)
	denied := sortedCopy(p.DeniedTools)
	approval := sortedCopy(p.ApprovalRequiredTools)

	repr := strings.Join([]string{
		"trust=" + p.TrustLevel,
		"allowed=" + strings.Join(allowed, ","),
		"denied=" + strings.Join(denied, ","),
		"approval=" + strings.Join(approval, ","),
	}, "|")

	sum := sha256.Sum256([]byte(repr))
	return fmt.Sprintf("%x", sum[:16])
}

func sortedCopy(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	cp := make([]string, len(s))
	copy(cp, s)
	sort.Strings(cp)
	return cp
}
