package session

import (
	intr "github.com/mossagents/moss/kernel/interaction"
	"strings"
	"time"
)

const ApprovalStateKey = "approval_state"

type ApprovalState struct {
	Rules              []ApprovalRule         `json:"rules,omitempty"`
	GrantedPermissions intr.PermissionProfile `json:"granted_permissions,omitempty"`
}

type ApprovalRule struct {
	CacheKey   string                    `json:"cache_key,omitempty"`
	CacheLabel string                    `json:"cache_label,omitempty"`
	ToolName   string                    `json:"tool_name,omitempty"`
	Category   intr.ApprovalCategory     `json:"category,omitempty"`
	Type       intr.ApprovalDecisionType `json:"type,omitempty"`
	CreatedAt  time.Time                 `json:"created_at,omitempty"`
}

func ApprovalStateOf(sess *Session) ApprovalState {
	if sess == nil || sess.State == nil {
		return ApprovalState{}
	}
	raw, ok := sess.State[ApprovalStateKey]
	if !ok {
		return ApprovalState{}
	}
	switch state := raw.(type) {
	case ApprovalState:
		return cloneApprovalState(state)
	case *ApprovalState:
		if state == nil {
			return ApprovalState{}
		}
		return cloneApprovalState(*state)
	default:
		return ApprovalState{}
	}
}

func SetApprovalState(sess *Session, state ApprovalState) {
	if sess == nil {
		return
	}
	if sess.State == nil {
		sess.State = map[string]any{}
	}
	sess.State[ApprovalStateKey] = cloneApprovalState(state)
}

func RememberApprovalRule(sess *Session, req *intr.ApprovalRequest, decisionType intr.ApprovalDecisionType, now time.Time) {
	if sess == nil || req == nil {
		return
	}
	cacheKey := strings.TrimSpace(req.CacheKey)
	if cacheKey == "" {
		return
	}
	state := ApprovalStateOf(sess)
	for _, rule := range state.Rules {
		if strings.EqualFold(rule.CacheKey, cacheKey) {
			return
		}
	}
	state.Rules = append(state.Rules, ApprovalRule{
		CacheKey:   cacheKey,
		CacheLabel: strings.TrimSpace(req.CacheLabel),
		ToolName:   strings.TrimSpace(req.ToolName),
		Category:   req.Category,
		Type:       decisionType,
		CreatedAt:  now.UTC(),
	})
	SetApprovalState(sess, state)
}

func MatchingApprovalRule(sess *Session, req *intr.ApprovalRequest) (ApprovalRule, bool) {
	if sess == nil || req == nil {
		return ApprovalRule{}, false
	}
	cacheKey := strings.TrimSpace(req.CacheKey)
	if cacheKey == "" {
		return ApprovalRule{}, false
	}
	state := ApprovalStateOf(sess)
	for _, rule := range state.Rules {
		if strings.EqualFold(rule.CacheKey, cacheKey) {
			return rule, true
		}
	}
	return ApprovalRule{}, false
}

func MergeGrantedPermissions(sess *Session, perms *intr.PermissionProfile) {
	if sess == nil || perms == nil {
		return
	}
	state := ApprovalStateOf(sess)
	state.GrantedPermissions = mergePermissionProfiles(state.GrantedPermissions, *perms)
	SetApprovalState(sess, state)
}

func GrantedPermissionsOf(sess *Session) intr.PermissionProfile {
	return ApprovalStateOf(sess).GrantedPermissions
}

func PermissionProfileCovers(granted intr.PermissionProfile, needed *intr.PermissionProfile) bool {
	if needed == nil {
		return false
	}
	for _, path := range needed.CommandPaths {
		if !containsFolded(granted.CommandPaths, path) {
			return false
		}
	}
	for _, host := range needed.HTTPHosts {
		if !containsFolded(granted.HTTPHosts, host) {
			return false
		}
	}
	if needed.CommandNetwork != nil {
		if granted.CommandNetwork == nil {
			return false
		}
		if needed.CommandNetwork.Enabled && !granted.CommandNetwork.Enabled {
			return false
		}
		for _, host := range needed.CommandNetwork.AllowHosts {
			if !containsFolded(granted.CommandNetwork.AllowHosts, host) {
				return false
			}
		}
	}
	return true
}

func cloneApprovalState(state ApprovalState) ApprovalState {
	state.Rules = append([]ApprovalRule(nil), state.Rules...)
	state.GrantedPermissions = clonePermissionProfile(state.GrantedPermissions)
	return state
}

func clonePermissionProfile(profile intr.PermissionProfile) intr.PermissionProfile {
	profile.CommandPaths = append([]string(nil), profile.CommandPaths...)
	profile.HTTPHosts = append([]string(nil), profile.HTTPHosts...)
	if profile.CommandNetwork != nil {
		cloned := *profile.CommandNetwork
		cloned.AllowHosts = append([]string(nil), profile.CommandNetwork.AllowHosts...)
		profile.CommandNetwork = &cloned
	}
	return profile
}

func mergePermissionProfiles(base, extra intr.PermissionProfile) intr.PermissionProfile {
	base.CommandPaths = appendUniqueFold(base.CommandPaths, extra.CommandPaths...)
	base.HTTPHosts = appendUniqueFold(base.HTTPHosts, extra.HTTPHosts...)
	if extra.CommandNetwork != nil {
		if base.CommandNetwork == nil {
			base.CommandNetwork = &intr.CommandNetworkPermission{}
		}
		base.CommandNetwork.Enabled = base.CommandNetwork.Enabled || extra.CommandNetwork.Enabled
		base.CommandNetwork.AllowHosts = appendUniqueFold(base.CommandNetwork.AllowHosts, extra.CommandNetwork.AllowHosts...)
	}
	return base
}

func appendUniqueFold(items []string, values ...string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || containsFolded(items, value) {
			continue
		}
		items = append(items, value)
	}
	return items
}

func containsFolded(items []string, target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	for _, item := range items {
		if strings.TrimSpace(strings.ToLower(item)) == target {
			return true
		}
	}
	return false
}
