package swarm

import (
	"fmt"
	"sort"
	"strings"
	"time"

	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
)

// RunStatus describes the lifecycle of a swarm run.
type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// ThreadStatus describes the lifecycle of a swarm thread.
type ThreadStatus string

const (
	ThreadPending   ThreadStatus = "pending"
	ThreadRunning   ThreadStatus = "running"
	ThreadBlocked   ThreadStatus = "blocked"
	ThreadCompleted ThreadStatus = "completed"
	ThreadFailed    ThreadStatus = "failed"
	ThreadCancelled ThreadStatus = "cancelled"
)

// Role identifies the responsibility of a swarm thread.
type Role string

const (
	RolePlanner     Role = "planner"
	RoleSupervisor  Role = "supervisor"
	RoleWorker      Role = "worker"
	RoleSynthesizer Role = "synthesizer"
	RoleReviewer    Role = "reviewer"
)

// MessageKind captures the intent of a peer-to-peer swarm message.
type MessageKind string

const (
	MessageAssignment MessageKind = "assignment"
	MessageQuestion   MessageKind = "question"
	MessageAnswer     MessageKind = "answer"
	MessageStatus     MessageKind = "status"
	MessageHandoff    MessageKind = "handoff"
	MessageCancel     MessageKind = "cancel"
)

// ArtifactKind identifies the shared artifact type published by a swarm thread.
type ArtifactKind string

const (
	ArtifactSummary        ArtifactKind = "summary"
	ArtifactFinding        ArtifactKind = "finding"
	ArtifactPlanFragment   ArtifactKind = "plan_fragment"
	ArtifactPatchProposal  ArtifactKind = "patch_proposal"
	ArtifactCitationSet    ArtifactKind = "citation_set"
	ArtifactSourceSet      ArtifactKind = "source_set"
	ArtifactConfidenceNote ArtifactKind = "confidence_note"
	ArtifactSynthesisDraft ArtifactKind = "synthesis_draft"
)

// Budget defines step/token/time ceilings.
type Budget struct {
	MaxSteps   int `json:"max_steps,omitempty"`
	MaxTokens  int `json:"max_tokens,omitempty"`
	TimeoutSec int `json:"timeout_sec,omitempty"`
}

func (b Budget) Validate() error {
	if b.MaxSteps < 0 {
		return fmt.Errorf("max_steps must be >= 0")
	}
	if b.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must be >= 0")
	}
	if b.TimeoutSec < 0 {
		return fmt.Errorf("timeout_sec must be >= 0")
	}
	return nil
}

func (b Budget) IsZero() bool {
	return b.MaxSteps == 0 && b.MaxTokens == 0 && b.TimeoutSec == 0
}

func (b Budget) TaskBudget() taskrt.TaskBudget {
	return taskrt.TaskBudget{
		MaxSteps:   b.MaxSteps,
		MaxTokens:  b.MaxTokens,
		TimeoutSec: b.TimeoutSec,
	}
}

// BudgetFromTaskBudget lifts a child-task budget into the swarm domain.
func BudgetFromTaskBudget(in taskrt.TaskBudget) Budget {
	return Budget{
		MaxSteps:   in.MaxSteps,
		MaxTokens:  in.MaxTokens,
		TimeoutSec: in.TimeoutSec,
	}
}

// Contract defines the stable execution and governance contract of a swarm thread.
type Contract struct {
	Role             Role               `json:"role,omitempty"`
	Goal             string             `json:"goal,omitempty"`
	InputContext     string             `json:"input_context,omitempty"`
	ThreadBudget     Budget             `json:"thread_budget,omitempty"`
	TaskBudget       Budget             `json:"task_budget,omitempty"`
	ApprovalCeiling  tool.ApprovalClass `json:"approval_ceiling,omitempty"`
	WritableScopes   []string           `json:"writable_scopes,omitempty"`
	MemoryScope      string             `json:"memory_scope,omitempty"`
	AllowedEffects   []tool.Effect      `json:"allowed_effects,omitempty"`
	PublishArtifacts []ArtifactKind     `json:"publish_artifacts,omitempty"`
	SubscribeKinds   []MessageKind      `json:"subscribe_kinds,omitempty"`
	Metadata         map[string]any     `json:"metadata,omitempty"`
}

// Normalized returns a trimmed, de-duplicated copy suitable for persistence.
func (c Contract) Normalized() Contract {
	out := c
	out.Role = Role(strings.TrimSpace(string(c.Role)))
	out.Goal = strings.TrimSpace(c.Goal)
	out.InputContext = strings.TrimSpace(c.InputContext)
	out.ApprovalCeiling = tool.ApprovalClass(strings.TrimSpace(string(c.ApprovalCeiling)))
	out.MemoryScope = strings.TrimSpace(c.MemoryScope)
	out.WritableScopes = normalizeStrings(c.WritableScopes)
	out.AllowedEffects = normalizeEffects(c.AllowedEffects)
	out.PublishArtifacts = normalizeArtifactKinds(c.PublishArtifacts)
	out.SubscribeKinds = normalizeMessageKinds(c.SubscribeKinds)
	out.Metadata = cloneMap(c.Metadata)
	return out
}

// EffectiveTaskBudget returns the per-task budget, defaulting to the thread budget.
func (c Contract) EffectiveTaskBudget() Budget {
	norm := c.Normalized()
	if norm.TaskBudget.IsZero() {
		return norm.ThreadBudget
	}
	return norm.TaskBudget
}

func (c Contract) Validate() error {
	norm := c.Normalized()
	if err := norm.ThreadBudget.Validate(); err != nil {
		return fmt.Errorf("thread_budget: %w", err)
	}
	if err := norm.TaskBudget.Validate(); err != nil {
		return fmt.Errorf("task_budget: %w", err)
	}
	if norm.Role == "" {
		return fmt.Errorf("role is required")
	}
	if !validApprovalClass(norm.ApprovalCeiling) {
		return fmt.Errorf("approval_ceiling %q is invalid", norm.ApprovalCeiling)
	}
	for _, scope := range norm.WritableScopes {
		if scope == "" {
			return fmt.Errorf("writable_scopes must not contain empty values")
		}
	}
	for _, effect := range norm.AllowedEffects {
		if strings.TrimSpace(string(effect)) == "" {
			return fmt.Errorf("allowed_effects must not contain empty values")
		}
	}
	for _, kind := range norm.PublishArtifacts {
		if strings.TrimSpace(string(kind)) == "" {
			return fmt.Errorf("publish_artifacts must not contain empty values")
		}
	}
	for _, kind := range norm.SubscribeKinds {
		if strings.TrimSpace(string(kind)) == "" {
			return fmt.Errorf("subscribe_kinds must not contain empty values")
		}
	}
	return nil
}

// ChildTaskContract projects the swarm contract onto the existing child-task contract.
func (c Contract) ChildTaskContract() taskrt.TaskContract {
	norm := c.Normalized()
	taskBudget := norm.EffectiveTaskBudget()
	out := taskrt.TaskContract{
		Goal:            norm.Goal,
		InputContext:    norm.InputContext,
		Budget:          taskBudget.TaskBudget(),
		ApprovalCeiling: norm.ApprovalCeiling,
		WritableScopes:  append([]string(nil), norm.WritableScopes...),
		MemoryScope:     norm.MemoryScope,
		AllowedEffects:  append([]tool.Effect(nil), norm.AllowedEffects...),
	}
	if len(norm.PublishArtifacts) > 0 {
		out.ReturnArtifacts = make([]string, 0, len(norm.PublishArtifacts))
		for _, kind := range norm.PublishArtifacts {
			out.ReturnArtifacts = append(out.ReturnArtifacts, string(kind))
		}
	}
	return out
}

// ContractFromTaskContract lifts an existing child-task contract into the swarm domain.
func ContractFromTaskContract(in taskrt.TaskContract) Contract {
	out := Contract{
		Goal:            strings.TrimSpace(in.Goal),
		InputContext:    strings.TrimSpace(in.InputContext),
		TaskBudget:      BudgetFromTaskBudget(in.Budget),
		ApprovalCeiling: in.ApprovalCeiling,
		WritableScopes:  append([]string(nil), in.WritableScopes...),
		MemoryScope:     strings.TrimSpace(in.MemoryScope),
		AllowedEffects:  append([]tool.Effect(nil), in.AllowedEffects...),
	}
	if len(in.ReturnArtifacts) > 0 {
		out.PublishArtifacts = make([]ArtifactKind, 0, len(in.ReturnArtifacts))
		for _, kind := range in.ReturnArtifacts {
			out.PublishArtifacts = append(out.PublishArtifacts, ArtifactKind(strings.TrimSpace(kind)))
		}
	}
	return out.Normalized()
}

// Run is the durable root object of one swarm execution.
type Run struct {
	ID            string         `json:"id"`
	Goal          string         `json:"goal,omitempty"`
	Status        RunStatus      `json:"status"`
	RootSessionID string         `json:"root_session_id,omitempty"`
	WorkspaceID   string         `json:"workspace_id,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
	CompletedAt   time.Time      `json:"completed_at,omitempty"`
}

// Thread describes one role-bearing execution thread within a swarm run.
type Thread struct {
	ID             string         `json:"id"`
	RunID          string         `json:"run_id"`
	SessionID      string         `json:"session_id,omitempty"`
	ParentThreadID string         `json:"parent_thread_id,omitempty"`
	TaskID         string         `json:"task_id,omitempty"`
	Role           Role           `json:"role"`
	Goal           string         `json:"goal,omitempty"`
	Status         ThreadStatus   `json:"status"`
	Contract       Contract       `json:"contract,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"created_at,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at,omitempty"`
	CompletedAt    time.Time      `json:"completed_at,omitempty"`
}

// Task is the swarm-native control-plane task node.
type Task struct {
	ID           string            `json:"id"`
	RunID        string            `json:"run_id"`
	ThreadID     string            `json:"thread_id,omitempty"`
	ParentTaskID string            `json:"parent_task_id,omitempty"`
	AssigneeRole Role              `json:"assignee_role,omitempty"`
	Goal         string            `json:"goal"`
	Status       taskrt.TaskStatus `json:"status"`
	DependsOn    []string          `json:"depends_on,omitempty"`
	Contract     Contract          `json:"contract,omitempty"`
	SessionID    string            `json:"session_id,omitempty"`
	ArtifactIDs  []string          `json:"artifact_ids,omitempty"`
	Error        string            `json:"error,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
	CreatedAt    time.Time         `json:"created_at,omitempty"`
	UpdatedAt    time.Time         `json:"updated_at,omitempty"`
}

// Message is the durable peer-to-peer message contract between swarm threads.
type Message struct {
	ID           string         `json:"id"`
	RunID        string         `json:"run_id"`
	ThreadID     string         `json:"thread_id,omitempty"`
	FromThreadID string         `json:"from_thread_id,omitempty"`
	ToThreadID   string         `json:"to_thread_id,omitempty"`
	TaskID       string         `json:"task_id,omitempty"`
	Kind         MessageKind    `json:"kind"`
	Subject      string         `json:"subject,omitempty"`
	Content      string         `json:"content,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at,omitempty"`
}

// ArtifactRef points at a shared artifact produced during a swarm run.
type ArtifactRef struct {
	ID        string         `json:"id"`
	RunID     string         `json:"run_id"`
	ThreadID  string         `json:"thread_id,omitempty"`
	TaskID    string         `json:"task_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Name      string         `json:"name"`
	Kind      ArtifactKind   `json:"kind"`
	Version   int            `json:"version,omitempty"`
	MIMEType  string         `json:"mime_type,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
}

func validApprovalClass(class tool.ApprovalClass) bool {
	switch class {
	case "", tool.ApprovalClassNone, tool.ApprovalClassPolicyGuarded, tool.ApprovalClassExplicitUser, tool.ApprovalClassSupervisorOnly:
		return true
	default:
		return false
	}
}

func normalizeStrings(in []string) []string {
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
	sort.Strings(out)
	return out
}

func normalizeEffects(in []tool.Effect) []tool.Effect {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[tool.Effect]struct{}, len(in))
	out := make([]tool.Effect, 0, len(in))
	for _, item := range in {
		item = tool.Effect(strings.TrimSpace(string(item)))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizeArtifactKinds(in []ArtifactKind) []ArtifactKind {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[ArtifactKind]struct{}, len(in))
	out := make([]ArtifactKind, 0, len(in))
	for _, item := range in {
		item = ArtifactKind(strings.TrimSpace(string(item)))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizeMessageKinds(in []MessageKind) []MessageKind {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[MessageKind]struct{}, len(in))
	out := make([]MessageKind, 0, len(in))
	for _, item := range in {
		item = MessageKind(strings.TrimSpace(string(item)))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
