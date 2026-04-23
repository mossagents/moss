package swarm

import (
	"context"
	"fmt"
	"strings"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	kswarm "github.com/mossagents/moss/kernel/swarm"
)

// RoleProtocol describes the default contract and publish/subscribe shape of a role.
type RoleProtocol struct {
	Role            kswarm.Role           `json:"role"`
	DefaultContract kswarm.Contract       `json:"default_contract,omitempty"`
	Publishes       []kswarm.ArtifactKind `json:"publishes,omitempty"`
	Subscribes      []kswarm.MessageKind  `json:"subscribes,omitempty"`
}

func (p RoleProtocol) normalized() RoleProtocol {
	out := p
	out.Role = kswarm.Role(strings.TrimSpace(string(p.Role)))
	out.DefaultContract = p.DefaultContract.Normalized()
	if out.DefaultContract.Role == "" {
		out.DefaultContract.Role = out.Role
	}
	out.Publishes = append([]kswarm.ArtifactKind(nil), p.Publishes...)
	out.Subscribes = append([]kswarm.MessageKind(nil), p.Subscribes...)
	return out
}

func (p RoleProtocol) Validate() error {
	norm := p.normalized()
	if norm.Role == "" {
		return fmt.Errorf("role is required")
	}
	if norm.DefaultContract.Role != norm.Role {
		return fmt.Errorf("default contract role %q must match protocol role %q", norm.DefaultContract.Role, norm.Role)
	}
	if err := norm.DefaultContract.Validate(); err != nil {
		return fmt.Errorf("default contract: %w", err)
	}
	return nil
}

// RoleSpec describes one installable swarm role agent.
type RoleSpec struct {
	Protocol     RoleProtocol `json:"protocol"`
	AgentName    string       `json:"agent_name,omitempty"`
	Description  string       `json:"description,omitempty"`
	SystemPrompt string       `json:"system_prompt"`
	Tools        []string     `json:"tools,omitempty"`
	MaxSteps     int          `json:"max_steps,omitempty"`
	TrustLevel   string       `json:"trust_level,omitempty"`
}

func (s RoleSpec) normalized() RoleSpec {
	out := s
	out.Protocol = s.Protocol.normalized()
	if strings.TrimSpace(out.AgentName) == "" {
		out.AgentName = "swarm-" + string(out.Protocol.Role)
	}
	out.AgentName = strings.TrimSpace(out.AgentName)
	out.Description = strings.TrimSpace(out.Description)
	out.SystemPrompt = strings.TrimSpace(out.SystemPrompt)
	out.TrustLevel = strings.TrimSpace(out.TrustLevel)
	if out.TrustLevel == "" {
		out.TrustLevel = "restricted"
	}
	if out.MaxSteps <= 0 {
		out.MaxSteps = 30
	}
	out.Tools = normalizeTools(out.Tools)
	return out
}

func (s RoleSpec) Validate() error {
	norm := s.normalized()
	if err := norm.Protocol.Validate(); err != nil {
		return err
	}
	if norm.AgentName == "" {
		return fmt.Errorf("agent_name is required")
	}
	if norm.SystemPrompt == "" {
		return fmt.Errorf("system_prompt is required")
	}
	if len(norm.Tools) == 0 {
		return fmt.Errorf("tools must not be empty for role %q", norm.Protocol.Role)
	}
	return nil
}

func (s RoleSpec) SubagentConfig() harness.SubagentConfig {
	norm := s.normalized()
	return harness.SubagentConfig{
		Name:         norm.AgentName,
		Description:  norm.Description,
		SystemPrompt: norm.SystemPrompt,
		Tools:        append([]string(nil), norm.Tools...),
		MaxSteps:     norm.MaxSteps,
		TrustLevel:   norm.TrustLevel,
	}
}

// InstallRolePack registers all roles into the harness subagent catalog.
func InstallRolePack(k *kernel.Kernel, specs ...RoleSpec) error {
	if k == nil {
		return fmt.Errorf("kernel must not be nil")
	}
	for _, spec := range specs {
		if err := spec.Validate(); err != nil {
			return err
		}
		if err := harness.RegisterSubagent(k, spec.SubagentConfig()); err != nil {
			return err
		}
	}
	return nil
}

// RolePackFeature exposes a harness feature that installs a role pack.
func RolePackFeature(specs ...RoleSpec) harness.Feature {
	copied := append([]RoleSpec(nil), specs...)
	return harness.FeatureFunc{
		FeatureName: "swarm-role-pack",
		MetadataValue: harness.FeatureMetadata{
			Key:   "swarm-role-pack",
			Phase: harness.FeaturePhaseConfigure,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			if h == nil || h.Kernel() == nil {
				return fmt.Errorf("harness kernel is required")
			}
			return InstallRolePack(h.Kernel(), copied...)
		},
	}
}

// DefaultResearchRolePack returns a ready-to-install research-first role pack.
func DefaultResearchRolePack() []RoleSpec {
	return []RoleSpec{
		{
			Protocol: RoleProtocol{
				Role: kswarm.RolePlanner,
				DefaultContract: kswarm.Contract{
					Role:            kswarm.RolePlanner,
					ApprovalCeiling: "policy_guarded",
					PublishArtifacts: []kswarm.ArtifactKind{
						kswarm.ArtifactPlanFragment,
					},
					SubscribeKinds: []kswarm.MessageKind{
						kswarm.MessageStatus,
						kswarm.MessageQuestion,
					},
				},
				Publishes:  []kswarm.ArtifactKind{kswarm.ArtifactPlanFragment},
				Subscribes: []kswarm.MessageKind{kswarm.MessageStatus, kswarm.MessageQuestion},
			},
			Description:  "Breaks a research goal into structured tasks and plan fragments.",
			SystemPrompt: "You are the swarm planner. Decompose the research goal into bounded tasks, keep contracts tight, and publish clear plan fragments for downstream roles.",
			Tools:        []string{"plan_task", "list_tasks", "update_task"},
			MaxSteps:     20,
		},
		{
			Protocol: RoleProtocol{
				Role: kswarm.RoleSupervisor,
				DefaultContract: kswarm.Contract{
					Role:            kswarm.RoleSupervisor,
					ApprovalCeiling: "policy_guarded",
					PublishArtifacts: []kswarm.ArtifactKind{
						kswarm.ArtifactSummary,
						kswarm.ArtifactPlanFragment,
					},
					SubscribeKinds: []kswarm.MessageKind{
						kswarm.MessageStatus,
						kswarm.MessageQuestion,
						kswarm.MessageAnswer,
						kswarm.MessageHandoff,
					},
				},
				Publishes:  []kswarm.ArtifactKind{kswarm.ArtifactSummary, kswarm.ArtifactPlanFragment},
				Subscribes: []kswarm.MessageKind{kswarm.MessageStatus, kswarm.MessageQuestion, kswarm.MessageAnswer, kswarm.MessageHandoff},
			},
			Description:  "Supervises the swarm, manages task flow, and decides when to spawn or synthesize.",
			SystemPrompt: "You are the swarm supervisor. Route work, watch budgets and approvals, ask for clarification when evidence is insufficient, and keep the swarm moving toward a coherent final result.",
			Tools:        []string{"plan_task", "list_tasks", "update_task", "claim_task", "send_mail", "read_mailbox", "spawn_agent", "query_agent", "read_agent"},
			MaxSteps:     30,
		},
		{
			Protocol: RoleProtocol{
				Role: kswarm.RoleWorker,
				DefaultContract: kswarm.Contract{
					Role:            kswarm.RoleWorker,
					ApprovalCeiling: "policy_guarded",
					PublishArtifacts: []kswarm.ArtifactKind{
						kswarm.ArtifactFinding,
						kswarm.ArtifactSourceSet,
						kswarm.ArtifactConfidenceNote,
					},
					SubscribeKinds: []kswarm.MessageKind{
						kswarm.MessageAssignment,
						kswarm.MessageQuestion,
					},
				},
				Publishes:  []kswarm.ArtifactKind{kswarm.ArtifactFinding, kswarm.ArtifactSourceSet, kswarm.ArtifactConfidenceNote},
				Subscribes: []kswarm.MessageKind{kswarm.MessageAssignment, kswarm.MessageQuestion},
			},
			Description:  "Executes bounded research tasks and reports evidence back to the swarm.",
			SystemPrompt: "You are a swarm worker. Claim one bounded task, gather evidence carefully, publish findings and source sets, and surface uncertainty explicitly instead of guessing.",
			Tools:        []string{"claim_task", "update_task", "send_mail", "read_mailbox"},
			MaxSteps:     40,
		},
		{
			Protocol: RoleProtocol{
				Role: kswarm.RoleSynthesizer,
				DefaultContract: kswarm.Contract{
					Role:            kswarm.RoleSynthesizer,
					ApprovalCeiling: "policy_guarded",
					PublishArtifacts: []kswarm.ArtifactKind{
						kswarm.ArtifactSynthesisDraft,
						kswarm.ArtifactSummary,
					},
					SubscribeKinds: []kswarm.MessageKind{
						kswarm.MessageAssignment,
						kswarm.MessageStatus,
					},
				},
				Publishes:  []kswarm.ArtifactKind{kswarm.ArtifactSynthesisDraft, kswarm.ArtifactSummary},
				Subscribes: []kswarm.MessageKind{kswarm.MessageAssignment, kswarm.MessageStatus},
			},
			Description:  "Synthesizes worker output into a coherent draft and final answer.",
			SystemPrompt: "You are the synthesizer. Turn the swarm's findings into a concise, evidence-backed answer, preserving key tradeoffs, caveats, and provenance.",
			Tools:        []string{"claim_task", "update_task", "send_mail", "read_mailbox"},
			MaxSteps:     30,
		},
		{
			Protocol: RoleProtocol{
				Role: kswarm.RoleReviewer,
				DefaultContract: kswarm.Contract{
					Role:            kswarm.RoleReviewer,
					ApprovalCeiling: "policy_guarded",
					PublishArtifacts: []kswarm.ArtifactKind{
						kswarm.ArtifactSummary,
						kswarm.ArtifactConfidenceNote,
					},
					SubscribeKinds: []kswarm.MessageKind{
						kswarm.MessageAssignment,
						kswarm.MessageStatus,
					},
				},
				Publishes:  []kswarm.ArtifactKind{kswarm.ArtifactSummary, kswarm.ArtifactConfidenceNote},
				Subscribes: []kswarm.MessageKind{kswarm.MessageAssignment, kswarm.MessageStatus},
			},
			Description:  "Reviews intermediate or final outputs for gaps, risk, and unsupported claims.",
			SystemPrompt: "You are the reviewer. Inspect the swarm's intermediate and final outputs, flag unsupported claims, and publish concise review feedback tied to evidence quality.",
			Tools:        []string{"claim_task", "update_task", "send_mail", "read_mailbox"},
			MaxSteps:     20,
		},
	}
}

func normalizeTools(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
