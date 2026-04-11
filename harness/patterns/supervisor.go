package patterns

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

// RoutingStrategy decides which worker agent should handle the current
// invocation. It receives the invocation context and a list of available
// workers, and returns the selected worker or nil if none applies.
type RoutingStrategy func(ctx *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent

type SupervisorStatus string

const (
	SupervisorStatusNoMatch         SupervisorStatus = "no_match"
	SupervisorStatusRunning         SupervisorStatus = "running"
	SupervisorStatusCompleted       SupervisorStatus = "completed"
	SupervisorStatusFailed          SupervisorStatus = "failed"
	SupervisorStatusTimedOut        SupervisorStatus = "timed_out"
	SupervisorStatusBudgetExhausted SupervisorStatus = "budget_exhausted"
	SupervisorStatusEscalated       SupervisorStatus = "escalated"
)

type SupervisorWorkerBudget struct {
	MinRemainingTokens int `json:"min_remaining_tokens,omitempty"`
	MinRemainingSteps  int `json:"min_remaining_steps,omitempty"`
}

type SupervisorWorkerHealth struct {
	SuccessCount        int              `json:"success_count,omitempty"`
	FailureCount        int              `json:"failure_count,omitempty"`
	TimeoutCount        int              `json:"timeout_count,omitempty"`
	ConsecutiveFailures int              `json:"consecutive_failures,omitempty"`
	LastStatus          SupervisorStatus `json:"last_status,omitempty"`
	LastError           string           `json:"last_error,omitempty"`
	LastFinishedAt      time.Time        `json:"last_finished_at,omitempty"`
	SuppressedUntil     time.Time        `json:"suppressed_until,omitempty"`
}

// SupervisorDecision captures the latest routing state for a supervisor invocation.
type SupervisorDecision struct {
	Status                SupervisorStatus                  `json:"status,omitempty"`
	SelectedWorker        string                            `json:"selected_worker,omitempty"`
	AttemptedWorkers      []string                          `json:"attempted_workers,omitempty"`
	FailedWorkers         []string                          `json:"failed_workers,omitempty"`
	TimedOutWorkers       []string                          `json:"timed_out_workers,omitempty"`
	BudgetFilteredWorkers []string                          `json:"budget_filtered_workers,omitempty"`
	AttemptCount          int                               `json:"attempt_count,omitempty"`
	LastError             string                            `json:"last_error,omitempty"`
	Escalated             bool                              `json:"escalated,omitempty"`
	EscalationReason      string                            `json:"escalation_reason,omitempty"`
	WorkerHealth          map[string]SupervisorWorkerHealth `json:"worker_health,omitempty"`
}

// SupervisorAgent dynamically delegates work to specialized worker agents
// using a configurable RoutingStrategy. It models the Leader-Worker pattern
// commonly used in multi-agent systems and can record routing state plus
// optionally fail over to another worker when the selected worker errors.
//
// The supervisor itself does not produce events — it routes to a worker and
// forwards that worker's event stream. If the routing strategy returns nil,
// the supervisor yields no events and records a no-match decision in session
// state when available.
//
// For more sophisticated multi-step delegation (e.g., decomposing a task,
// distributing sub-tasks, and aggregating results), compose SupervisorAgent
// with SequentialAgent, ParallelAgent, and LoopAgent.
type SupervisorAgent struct {
	AgentName                 string
	Desc                      string
	Workers                   []kernel.Agent
	Router                    RoutingStrategy
	StateKey                  string
	MaxAttempts               int
	FailoverOnError           bool
	WorkerTimeout             time.Duration
	MaxConsecutiveFailures    int
	HealthCooldown            time.Duration
	WorkerBudgets             map[string]SupervisorWorkerBudget
	EscalateOnNoMatch         bool
	EscalateOnFailure         bool
	EscalateOnTimeout         bool
	EscalateOnBudgetExhausted bool

	clock func() time.Time
}

var _ kernel.Agent = (*SupervisorAgent)(nil)
var _ kernel.AgentWithDescription = (*SupervisorAgent)(nil)
var _ kernel.AgentWithSubAgents = (*SupervisorAgent)(nil)

func (s *SupervisorAgent) Name() string              { return s.AgentName }
func (s *SupervisorAgent) Description() string       { return s.Desc }
func (s *SupervisorAgent) SubAgents() []kernel.Agent { return s.Workers }

func (s *SupervisorAgent) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		workers := s.activeWorkers()
		state := s.initialDecision(ctx)
		s.storeDecision(ctx, state)
		if len(workers) == 0 || s.Router == nil {
			s.handleNoMatch(ctx, &state, "no workers available", yield)
			return
		}
		maxAttempts := s.allowedAttempts(len(workers))
		if maxAttempts == 0 {
			return
		}

		available := append([]kernel.Agent(nil), workers...)
		for attempt := 0; attempt < maxAttempts && len(available) > 0; attempt++ {
			eligible := s.healthyWorkers(available, state.WorkerHealth)
			if len(eligible) == 0 {
				s.handleNoMatch(ctx, &state, "no healthy workers available", yield)
				return
			}
			eligible, filteredByBudget := s.budgetEligibleWorkers(ctx.Session(), eligible)
			state.BudgetFilteredWorkers = appendUniqueStrings(state.BudgetFilteredWorkers, filteredByBudget...)
			if len(eligible) == 0 {
				s.handleBudgetExhausted(ctx, &state, yield)
				return
			}

			selected := s.Router(ctx, eligible)
			if selected == nil {
				s.handleNoMatch(ctx, &state, "router returned no matching worker", yield)
				return
			}

			state.Status = SupervisorStatusRunning
			state.SelectedWorker = selected.Name()
			state.AttemptedWorkers = append(state.AttemptedWorkers, selected.Name())
			state.AttemptCount = len(state.AttemptedWorkers)
			state.Escalated = false
			state.EscalationReason = ""
			s.storeDecision(ctx, state)

			runCtx := ctx
			cancel := func() {}
			if s.WorkerTimeout > 0 {
				baseCtx := ctx.Context
				if baseCtx == nil {
					baseCtx = context.Background()
				}
				timeoutCtx, timeoutCancel := context.WithTimeout(baseCtx, s.WorkerTimeout)
				runCtx = ctx.WithContext(timeoutCtx)
				cancel = timeoutCancel
			}
			var runErr error
			for event, err := range runCtx.RunChild(selected, kernel.ChildRunConfig{
				Branch: fmt.Sprintf("%s.%s[attempt=%d]", ctx.Branch(), selected.Name(), attempt),
			}) {
				if err != nil {
					runErr = err
					break
				}
				if event != nil && event.Actions.Escalate {
					cancel()
					s.recordWorkerEscalation(&state, selected.Name())
					state.Status = SupervisorStatusEscalated
					state.Escalated = true
					state.EscalationReason = "worker_escalated"
					s.storeDecision(ctx, state)
					yield(event, nil)
					return
				}
				if !yield(event, nil) {
					cancel()
					return
				}
			}
			cancel()
			if runErr == nil {
				s.recordWorkerSuccess(&state, selected.Name())
				state.Status = SupervisorStatusCompleted
				state.Escalated = false
				state.EscalationReason = ""
				s.storeDecision(ctx, state)
				return
			}

			if errors.Is(runErr, context.Canceled) && ctx.Err() != nil {
				yield(nil, runErr)
				return
			}
			available = removeWorkerByName(available, selected.Name())

			if errors.Is(runErr, context.DeadlineExceeded) {
				s.recordWorkerTimeout(&state, selected.Name(), runErr)
				state.Status = SupervisorStatusTimedOut
				state.LastError = runErr.Error()
				state.TimedOutWorkers = appendUniqueStrings(state.TimedOutWorkers, selected.Name())
				s.storeDecision(ctx, state)
				if s.FailoverOnError && attempt+1 < maxAttempts && len(available) > 0 {
					continue
				}
				if s.escalateOnTimeout(ctx, &state, yield) {
					return
				}
				yield(nil, runErr)
				return
			}

			s.recordWorkerFailure(&state, selected.Name(), runErr)
			state.Status = SupervisorStatusFailed
			state.LastError = runErr.Error()
			state.FailedWorkers = append(state.FailedWorkers, selected.Name())
			s.storeDecision(ctx, state)
			if s.FailoverOnError && attempt+1 < maxAttempts && len(available) > 0 {
				continue
			}
			if s.escalateOnFailure(ctx, &state, yield) {
				return
			}
			yield(nil, runErr)
			return
		}
	}
}

// RoundRobinRouter returns a RoutingStrategy that cycles through workers
// based on the iteration state stored in the session.
func RoundRobinRouter(stateKey string) RoutingStrategy {
	return func(ctx *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
		if len(workers) == 0 {
			return nil
		}
		sess := ctx.Session()
		idx := 0
		if sess != nil {
			if v, ok := sess.GetState(stateKey); ok {
				if n, ok := stateInt(v); ok {
					idx = n
				}
			}
		}
		selected := workers[idx%len(workers)]
		if sess != nil {
			sess.SetState(stateKey, (idx+1)%len(workers))
		}
		return selected
	}
}

// FirstMatchRouter returns a RoutingStrategy that delegates to the first
// worker accepted by the given predicate.
func FirstMatchRouter(match func(ctx *kernel.InvocationContext, w kernel.Agent) bool) RoutingStrategy {
	return func(ctx *kernel.InvocationContext, workers []kernel.Agent) kernel.Agent {
		for _, w := range workers {
			if match(ctx, w) {
				return w
			}
		}
		return nil
	}
}

func (s *SupervisorAgent) activeWorkers() []kernel.Agent {
	workers := make([]kernel.Agent, 0, len(s.Workers))
	for _, worker := range s.Workers {
		if worker == nil {
			continue
		}
		workers = append(workers, worker)
	}
	return workers
}

func (s *SupervisorAgent) allowedAttempts(workerCount int) int {
	if workerCount <= 0 {
		return 0
	}
	if s.MaxAttempts > 0 && s.MaxAttempts < workerCount {
		return s.MaxAttempts
	}
	if s.MaxAttempts > 0 {
		return workerCount
	}
	if s.FailoverOnError {
		return workerCount
	}
	return 1
}

func (s *SupervisorAgent) storeDecision(ctx *kernel.InvocationContext, decision SupervisorDecision) {
	sess := ctx.Session()
	if sess == nil {
		return
	}
	decision.WorkerHealth = cloneSupervisorWorkerHealth(decision.WorkerHealth)
	sess.SetState(s.stateKey(), decision)
}

func (s *SupervisorAgent) stateKey() string {
	if key := strings.TrimSpace(s.StateKey); key != "" {
		return key
	}
	if name := strings.TrimSpace(s.AgentName); name != "" {
		return "patterns.supervisor." + name
	}
	return "patterns.supervisor"
}

func removeWorkerByName(workers []kernel.Agent, name string) []kernel.Agent {
	out := make([]kernel.Agent, 0, len(workers))
	removed := false
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		if !removed && worker.Name() == name {
			removed = true
			continue
		}
		out = append(out, worker)
	}
	return out
}

func stateInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func (s *SupervisorAgent) initialDecision(ctx *kernel.InvocationContext) SupervisorDecision {
	decision := SupervisorDecision{Status: SupervisorStatusNoMatch}
	if previous, ok := s.loadDecision(ctx); ok {
		decision.WorkerHealth = cloneSupervisorWorkerHealth(previous.WorkerHealth)
	}
	return decision
}

func (s *SupervisorAgent) loadDecision(ctx *kernel.InvocationContext) (SupervisorDecision, bool) {
	sess := ctx.Session()
	if sess == nil {
		return SupervisorDecision{}, false
	}
	value, ok := sess.GetState(s.stateKey())
	if !ok {
		return SupervisorDecision{}, false
	}
	switch existing := value.(type) {
	case SupervisorDecision:
		existing.WorkerHealth = cloneSupervisorWorkerHealth(existing.WorkerHealth)
		return existing, true
	case *SupervisorDecision:
		if existing != nil {
			copy := *existing
			copy.WorkerHealth = cloneSupervisorWorkerHealth(copy.WorkerHealth)
			return copy, true
		}
	}
	return SupervisorDecision{}, false
}

func (s *SupervisorAgent) healthyWorkers(workers []kernel.Agent, health map[string]SupervisorWorkerHealth) []kernel.Agent {
	if len(workers) == 0 {
		return nil
	}
	now := s.now()
	out := make([]kernel.Agent, 0, len(workers))
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		status, ok := health[worker.Name()]
		if ok && !status.SuppressedUntil.IsZero() && now.Before(status.SuppressedUntil) {
			continue
		}
		out = append(out, worker)
	}
	return out
}

func (s *SupervisorAgent) budgetEligibleWorkers(sess *session.Session, workers []kernel.Agent) ([]kernel.Agent, []string) {
	if len(workers) == 0 {
		return nil, nil
	}
	if len(s.WorkerBudgets) == 0 || sess == nil {
		return append([]kernel.Agent(nil), workers...), nil
	}
	out := make([]kernel.Agent, 0, len(workers))
	var filtered []string
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		if s.workerWithinBudget(sess, worker.Name()) {
			out = append(out, worker)
			continue
		}
		filtered = append(filtered, worker.Name())
	}
	return out, filtered
}

func (s *SupervisorAgent) workerWithinBudget(sess *session.Session, workerName string) bool {
	req, ok := s.WorkerBudgets[workerName]
	if !ok || sess == nil {
		return true
	}
	if req.MinRemainingSteps > 0 && sess.Budget.MaxSteps > 0 {
		if sess.Budget.MaxSteps-sess.Budget.UsedStepsValue() < req.MinRemainingSteps {
			return false
		}
	}
	if req.MinRemainingTokens > 0 && sess.Budget.MaxTokens > 0 {
		if sess.Budget.MaxTokens-sess.Budget.UsedTokensValue() < req.MinRemainingTokens {
			return false
		}
	}
	return true
}

func (s *SupervisorAgent) handleNoMatch(ctx *kernel.InvocationContext, state *SupervisorDecision, reason string, yield func(*session.Event, error) bool) {
	state.Status = SupervisorStatusNoMatch
	state.LastError = reason
	s.storeDecision(ctx, *state)
	if !s.EscalateOnNoMatch {
		return
	}
	state.Escalated = true
	state.EscalationReason = "no_match"
	s.storeDecision(ctx, *state)
	yield(s.escalationEvent(), nil)
}

func (s *SupervisorAgent) handleBudgetExhausted(ctx *kernel.InvocationContext, state *SupervisorDecision, yield func(*session.Event, error) bool) {
	state.Status = SupervisorStatusBudgetExhausted
	state.LastError = "no workers satisfy remaining session budget"
	s.storeDecision(ctx, *state)
	if !s.EscalateOnBudgetExhausted {
		return
	}
	state.Escalated = true
	state.EscalationReason = "budget_exhausted"
	s.storeDecision(ctx, *state)
	yield(s.escalationEvent(), nil)
}

func (s *SupervisorAgent) recordWorkerSuccess(state *SupervisorDecision, workerName string) {
	health := s.workerHealth(state, workerName)
	health.SuccessCount++
	health.ConsecutiveFailures = 0
	health.LastStatus = SupervisorStatusCompleted
	health.LastError = ""
	health.LastFinishedAt = s.now()
	health.SuppressedUntil = time.Time{}
	s.setWorkerHealth(state, workerName, health)
}

func (s *SupervisorAgent) recordWorkerFailure(state *SupervisorDecision, workerName string, err error) {
	health := s.workerHealth(state, workerName)
	health.FailureCount++
	health.ConsecutiveFailures++
	health.LastStatus = SupervisorStatusFailed
	health.LastFinishedAt = s.now()
	if err != nil {
		health.LastError = err.Error()
	}
	s.applyHealthCooldown(&health)
	s.setWorkerHealth(state, workerName, health)
}

func (s *SupervisorAgent) recordWorkerTimeout(state *SupervisorDecision, workerName string, err error) {
	health := s.workerHealth(state, workerName)
	health.TimeoutCount++
	health.ConsecutiveFailures++
	health.LastStatus = SupervisorStatusTimedOut
	health.LastFinishedAt = s.now()
	if err != nil {
		health.LastError = err.Error()
	}
	s.applyHealthCooldown(&health)
	s.setWorkerHealth(state, workerName, health)
}

func (s *SupervisorAgent) recordWorkerEscalation(state *SupervisorDecision, workerName string) {
	health := s.workerHealth(state, workerName)
	health.LastStatus = SupervisorStatusEscalated
	health.LastError = ""
	health.LastFinishedAt = s.now()
	s.setWorkerHealth(state, workerName, health)
}

func (s *SupervisorAgent) applyHealthCooldown(health *SupervisorWorkerHealth) {
	if health == nil {
		return
	}
	if s.MaxConsecutiveFailures <= 0 || s.HealthCooldown <= 0 {
		return
	}
	if health.ConsecutiveFailures < s.MaxConsecutiveFailures {
		return
	}
	health.SuppressedUntil = s.now().Add(s.HealthCooldown)
}

func (s *SupervisorAgent) workerHealth(state *SupervisorDecision, workerName string) SupervisorWorkerHealth {
	if state == nil || workerName == "" {
		return SupervisorWorkerHealth{}
	}
	if state.WorkerHealth == nil {
		state.WorkerHealth = make(map[string]SupervisorWorkerHealth)
	}
	return state.WorkerHealth[workerName]
}

func (s *SupervisorAgent) setWorkerHealth(state *SupervisorDecision, workerName string, health SupervisorWorkerHealth) {
	if state == nil || workerName == "" {
		return
	}
	if state.WorkerHealth == nil {
		state.WorkerHealth = make(map[string]SupervisorWorkerHealth)
	}
	state.WorkerHealth[workerName] = health
}

func (s *SupervisorAgent) escalateOnTimeout(ctx *kernel.InvocationContext, state *SupervisorDecision, yield func(*session.Event, error) bool) bool {
	if !s.EscalateOnTimeout {
		return false
	}
	state.Escalated = true
	state.EscalationReason = "timeout"
	s.storeDecision(ctx, *state)
	yield(s.escalationEvent(), nil)
	return true
}

func (s *SupervisorAgent) escalateOnFailure(ctx *kernel.InvocationContext, state *SupervisorDecision, yield func(*session.Event, error) bool) bool {
	if !s.EscalateOnFailure {
		return false
	}
	state.Escalated = true
	state.EscalationReason = "failure"
	s.storeDecision(ctx, *state)
	yield(s.escalationEvent(), nil)
	return true
}

func (s *SupervisorAgent) escalationEvent() *session.Event {
	return &session.Event{
		Type:      session.EventTypeCustom,
		Author:    s.Name(),
		Timestamp: s.now(),
		Actions: session.EventActions{
			Escalate: true,
		},
	}
}

func (s *SupervisorAgent) now() time.Time {
	if s.clock != nil {
		return s.clock().UTC()
	}
	return time.Now().UTC()
}

func cloneSupervisorWorkerHealth(in map[string]SupervisorWorkerHealth) map[string]SupervisorWorkerHealth {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]SupervisorWorkerHealth, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func appendUniqueStrings(dst []string, items ...string) []string {
	for _, item := range items {
		if item == "" {
			continue
		}
		exists := false
		for _, current := range dst {
			if current == item {
				exists = true
				break
			}
		}
		if !exists {
			dst = append(dst, item)
		}
	}
	return dst
}
