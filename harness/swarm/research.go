package swarm

import (
	"fmt"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/ids"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

// ResearchRunSeed captures the minimal inputs required to bootstrap a research swarm.
type ResearchRunSeed struct {
	RunID         string `json:"run_id,omitempty"`
	Goal          string `json:"goal"`
	RootSessionID string `json:"root_session_id,omitempty"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
}

// ResearchSeed materializes the initial run/thread/task state for a research swarm.
type ResearchSeed struct {
	Run     kswarm.Run          `json:"run"`
	Threads []kswarm.Thread     `json:"threads,omitempty"`
	Tasks   []taskrt.TaskRecord `json:"tasks,omitempty"`
}

// ResearchOrchestrator is the research-first orchestration template built on top
// of the harness swarm runtime adapter.
type ResearchOrchestrator struct {
	runtime     *Runtime
	planner     RoleSpec
	supervisor  RoleSpec
	worker      RoleSpec
	synthesizer RoleSpec
	reviewer    RoleSpec
}

// NewResearchOrchestrator binds the default role pack into a research-first orchestrator.
func NewResearchOrchestrator(rt *Runtime) (*ResearchOrchestrator, error) {
	if rt == nil {
		return nil, fmt.Errorf("runtime must not be nil")
	}
	resolve := func(role kswarm.Role) (RoleSpec, error) {
		spec, ok := rt.Role(role)
		if !ok {
			return RoleSpec{}, fmt.Errorf("role %q is not configured on runtime", role)
		}
		return spec, nil
	}
	planner, err := resolve(kswarm.RolePlanner)
	if err != nil {
		return nil, err
	}
	supervisor, err := resolve(kswarm.RoleSupervisor)
	if err != nil {
		return nil, err
	}
	worker, err := resolve(kswarm.RoleWorker)
	if err != nil {
		return nil, err
	}
	synthesizer, err := resolve(kswarm.RoleSynthesizer)
	if err != nil {
		return nil, err
	}
	reviewer, err := resolve(kswarm.RoleReviewer)
	if err != nil {
		return nil, err
	}
	return &ResearchOrchestrator{
		runtime:     rt,
		planner:     planner,
		supervisor:  supervisor,
		worker:      worker,
		synthesizer: synthesizer,
		reviewer:    reviewer,
	}, nil
}

// Seed materializes the initial run state for a research swarm.
func (o *ResearchOrchestrator) Seed(seed ResearchRunSeed) (*ResearchSeed, error) {
	if o == nil {
		return nil, fmt.Errorf("research orchestrator must not be nil")
	}
	goal := strings.TrimSpace(seed.Goal)
	if goal == "" {
		return nil, fmt.Errorf("goal is required")
	}
	runID := strings.TrimSpace(seed.RunID)
	if runID == "" {
		runID = "swarm-" + ids.New()
	}
	now := time.Now().UTC()
	supervisorThreadID := runID + "-supervisor"
	plannerThreadID := runID + "-planner"
	synthThreadID := runID + "-synthesizer"
	reviewerThreadID := runID + "-reviewer"

	run := kswarm.Run{
		ID:            runID,
		Goal:          goal,
		Status:        kswarm.RunRunning,
		RootSessionID: strings.TrimSpace(seed.RootSessionID),
		WorkspaceID:   strings.TrimSpace(seed.WorkspaceID),
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	threads := []kswarm.Thread{
		o.thread(supervisorThreadID, runID, "", "", goal, o.supervisor, now),
		o.thread(plannerThreadID, runID, supervisorThreadID, "", goal, o.planner, now),
		o.thread(synthThreadID, runID, supervisorThreadID, "", goal, o.synthesizer, now),
		o.thread(reviewerThreadID, runID, supervisorThreadID, "", goal, o.reviewer, now),
	}

	tasks := []taskrt.TaskRecord{
		o.roleTask(runID, plannerThreadID, o.planner, "Plan the research swarm for: "+goal, "", nil, now),
		o.roleTask(runID, supervisorThreadID, o.supervisor, "Supervise the research swarm for: "+goal, "", nil, now),
	}

	return &ResearchSeed{
		Run:     run,
		Threads: threads,
		Tasks:   tasks,
	}, nil
}

// WorkerTask creates a bounded research worker task.
func (o *ResearchOrchestrator) WorkerTask(runID, threadID, goal, inputContext string, dependsOn []string) taskrt.TaskRecord {
	return o.roleTask(runID, threadID, o.worker, goal, inputContext, dependsOn, time.Now().UTC())
}

// SynthesisTask creates a synthesis task that turns findings into a draft/final answer.
func (o *ResearchOrchestrator) SynthesisTask(runID, threadID, goal, inputContext string, dependsOn []string) taskrt.TaskRecord {
	return o.roleTask(runID, threadID, o.synthesizer, goal, inputContext, dependsOn, time.Now().UTC())
}

// ReviewTask creates a review task for validating intermediate/final outputs.
func (o *ResearchOrchestrator) ReviewTask(runID, threadID, goal, inputContext string, dependsOn []string) taskrt.TaskRecord {
	return o.roleTask(runID, threadID, o.reviewer, goal, inputContext, dependsOn, time.Now().UTC())
}

// ReviewRequestMessage routes a structured review request to the reviewer thread.
func (o *ResearchOrchestrator) ReviewRequestMessage(runID, fromThreadID, toThreadID, taskID, subject, content, reason string) taskrt.TaskMessage {
	return o.governanceMessage(runID, fromThreadID, toThreadID, taskID, kswarm.MessageStatus, subject, content, kswarm.GovernanceReviewRequested, reason)
}

// RedirectMessage records a supervisor redirect from one thread to another.
func (o *ResearchOrchestrator) RedirectMessage(runID, fromThreadID, toThreadID, taskID, subject, content, reason string) taskrt.TaskMessage {
	return o.governanceMessage(runID, fromThreadID, toThreadID, taskID, kswarm.MessageHandoff, subject, content, kswarm.GovernanceRedirected, reason)
}

// TakeoverMessage records a reviewer/supervisor takeover decision for a task.
func (o *ResearchOrchestrator) TakeoverMessage(runID, fromThreadID, toThreadID, taskID, subject, content, reason string) taskrt.TaskMessage {
	return o.governanceMessage(runID, fromThreadID, toThreadID, taskID, kswarm.MessageHandoff, subject, content, kswarm.GovernanceTakenOver, reason)
}

func (o *ResearchOrchestrator) thread(id, runID, parentThreadID, taskID, goal string, spec RoleSpec, now time.Time) kswarm.Thread {
	return kswarm.Thread{
		ID:             id,
		RunID:          runID,
		ParentThreadID: strings.TrimSpace(parentThreadID),
		TaskID:         strings.TrimSpace(taskID),
		Role:           spec.Protocol.Role,
		Goal:           goal,
		Status:         kswarm.ThreadRunning,
		Contract:       spec.Protocol.DefaultContract.Normalized(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func (o *ResearchOrchestrator) governanceMessage(runID, fromThreadID, toThreadID, taskID string, kind kswarm.MessageKind, subject, content string, action kswarm.GovernanceAction, reason string) taskrt.TaskMessage {
	now := time.Now().UTC()
	threadID := strings.TrimSpace(toThreadID)
	if threadID == "" {
		threadID = strings.TrimSpace(fromThreadID)
	}
	return taskrt.TaskMessage{
		ID:           ids.New(),
		TaskID:       strings.TrimSpace(taskID),
		SwarmRunID:   strings.TrimSpace(runID),
		ThreadID:     threadID,
		FromThreadID: strings.TrimSpace(fromThreadID),
		ToThreadID:   strings.TrimSpace(toThreadID),
		Kind:         string(kind),
		Subject:      strings.TrimSpace(subject),
		Content:      strings.TrimSpace(content),
		Metadata:     kswarm.GovernanceMetadata(action, reason, nil),
		CreatedAt:    now,
	}
}

func (o *ResearchOrchestrator) roleTask(runID, threadID string, spec RoleSpec, goal, inputContext string, dependsOn []string, now time.Time) taskrt.TaskRecord {
	contract := spec.Protocol.DefaultContract.Normalized()
	if goal = strings.TrimSpace(goal); goal != "" {
		contract.Goal = goal
	}
	if inputContext = strings.TrimSpace(inputContext); inputContext != "" {
		contract.InputContext = inputContext
	}
	taskID := ids.New()
	return taskrt.TaskRecord{
		ID:         taskID,
		AgentName:  spec.normalized().AgentName,
		Goal:       contract.Goal,
		Status:     taskrt.TaskPending,
		SwarmRunID: strings.TrimSpace(runID),
		ThreadID:   strings.TrimSpace(threadID),
		DependsOn:  append([]string(nil), dependsOn...),
		Contract:   contract.ChildTaskContract(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}
