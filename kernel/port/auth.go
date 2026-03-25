package port

import "context"

// Identity 表示已认证用户的身份信息。
type Identity struct {
	UserID   string         `json:"user_id"`
	TenantID string         `json:"tenant_id,omitempty"`
	Roles    []string       `json:"roles"`
	Meta     map[string]any `json:"meta,omitempty"`
}

// HasRole 检查用户是否拥有指定角色。
func (id *Identity) HasRole(role string) bool {
	for _, r := range id.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// Authenticator 用于验证令牌并返回身份信息。
// 实现可对接 JWT、OAuth、API Key 等认证机制。
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*Identity, error)
}
