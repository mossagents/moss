package swarm

import (
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

func TestResearchOrchestratorSeed(t *testing.T) {
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	rt, err := NewRuntime(kernel.New(
		kernel.WithSessionStore(store),
		kernel.WithTaskRuntime(taskrt.NewMemoryTaskRuntime()),
		kernel.WithArtifactStore(artifact.NewMemoryStore()),
	))
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	orchestrator, err := NewResearchOrchestrator(rt)
	if err != nil {
		t.Fatalf("NewResearchOrchestrator: %v", err)
	}
	seed, err := orchestrator.Seed(ResearchRunSeed{
		RunID:         "swarm-research",
		Goal:          "Investigate agent swarm architecture",
		RootSessionID: "sess-root",
		WorkspaceID:   "ws-1",
	})
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if seed.Run.ID != "swarm-research" || seed.Run.RootSessionID != "sess-root" {
		t.Fatalf("unexpected run: %+v", seed.Run)
	}
	if len(seed.Threads) != 4 || len(seed.Tasks) != 2 {
		t.Fatalf("unexpected seed sizes: %+v", seed)
	}
	if seed.Tasks[0].SwarmRunID != "swarm-research" || seed.Tasks[0].AgentName != "swarm-planner" {
		t.Fatalf("unexpected planner task: %+v", seed.Tasks[0])
	}
	if seed.Tasks[1].AgentName != "swarm-supervisor" {
		t.Fatalf("unexpected supervisor task: %+v", seed.Tasks[1])
	}
}

func TestResearchOrchestratorWorkerAndReviewTasks(t *testing.T) {
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	rt, err := NewRuntime(kernel.New(
		kernel.WithSessionStore(store),
		kernel.WithTaskRuntime(taskrt.NewMemoryTaskRuntime()),
		kernel.WithArtifactStore(artifact.NewMemoryStore()),
	))
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	orchestrator, err := NewResearchOrchestrator(rt)
	if err != nil {
		t.Fatalf("NewResearchOrchestrator: %v", err)
	}
	worker := orchestrator.WorkerTask("swarm-1", "thread-worker", "Collect sources", "repo", []string{"dep-1"})
	if worker.AgentName != "swarm-worker" || worker.Contract.InputContext != "repo" || len(worker.DependsOn) != 1 {
		t.Fatalf("unexpected worker task: %+v", worker)
	}
	review := orchestrator.ReviewTask("swarm-1", "thread-reviewer", "Review draft", "draft", []string{"dep-2"})
	if review.AgentName != "swarm-reviewer" || review.Contract.InputContext != "draft" {
		t.Fatalf("unexpected review task: %+v", review)
	}
	redirect := orchestrator.RedirectMessage("swarm-1", "thread-supervisor", "thread-worker", "task-1", "redirect", "take another angle", "need broader coverage")
	if redirect.ToThreadID != "thread-worker" || redirect.Kind != string(kswarm.MessageHandoff) {
		t.Fatalf("unexpected redirect message: %+v", redirect)
	}
	if got := kswarm.GovernanceActionFromMetadata(redirect.Metadata); got != kswarm.GovernanceRedirected {
		t.Fatalf("redirect action = %q, want %q", got, kswarm.GovernanceRedirected)
	}
	takeover := orchestrator.TakeoverMessage("swarm-1", "thread-reviewer", "thread-supervisor", "task-1", "takeover", "I will finish this directly", "unsupported claims")
	if got := kswarm.GovernanceActionFromMetadata(takeover.Metadata); got != kswarm.GovernanceTakenOver {
		t.Fatalf("takeover action = %q, want %q", got, kswarm.GovernanceTakenOver)
	}
}
