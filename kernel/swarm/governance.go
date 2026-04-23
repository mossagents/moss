package swarm

import "strings"

const (
	MetadataGovernanceAction    = "swarm_governance_action"
	MetadataGovernanceReason    = "swarm_governance_reason"
	MetadataGovernanceActorRole = "swarm_governance_actor_role"
)

// GovernanceAction captures a durable governance decision or intervention.
type GovernanceAction string

const (
	GovernanceReviewRequested GovernanceAction = "review_requested"
	GovernanceRedirected      GovernanceAction = "redirected"
	GovernanceTakenOver       GovernanceAction = "taken_over"
	GovernanceApproved        GovernanceAction = "approved"
	GovernanceRejected        GovernanceAction = "rejected"
)

// GovernanceMetadata returns a normalized metadata map for governance actions.
func GovernanceMetadata(action GovernanceAction, reason string, extras map[string]any) map[string]any {
	out := cloneMap(extras)
	if out == nil {
		out = make(map[string]any, 3)
	}
	if action = GovernanceAction(strings.TrimSpace(string(action))); action != "" {
		out[MetadataGovernanceAction] = string(action)
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		out[MetadataGovernanceReason] = reason
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// GovernanceActionFromMetadata extracts the governance action from metadata.
func GovernanceActionFromMetadata(meta map[string]any) GovernanceAction {
	if len(meta) == 0 {
		return ""
	}
	raw, _ := meta[MetadataGovernanceAction].(string)
	return GovernanceAction(strings.TrimSpace(raw))
}
