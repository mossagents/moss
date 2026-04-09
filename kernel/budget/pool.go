package budget

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// ErrAllocationNotFound is returned when an operation references a non-existent allocation.
var ErrAllocationNotFound = fmt.Errorf("budget allocation not found")

// Allocation represents a named budget allocation within a BudgetPool.
type Allocation struct {
	Name       string `json:"name"`
	MaxTokens  int64  `json:"max_tokens"`
	MaxSteps   int64  `json:"max_steps"`
	UsedTokens int64  `json:"used_tokens"`
	UsedSteps  int64  `json:"used_steps"`
	Priority   int    `json:"priority"`
}

// AllocationSnapshot is a point-in-time view of an Allocation.
type AllocationSnapshot struct {
	Name       string  `json:"name"`
	MaxTokens  int64   `json:"max_tokens"`
	MaxSteps   int64   `json:"max_steps"`
	UsedTokens int64   `json:"used_tokens"`
	UsedSteps  int64   `json:"used_steps"`
	Priority   int     `json:"priority"`
	TokensPct  float64 `json:"tokens_pct"`
	StepsPct   float64 `json:"steps_pct"`
}

// BudgetPool manages named budget allocations for different subsystems.
// Each allocation has independent token/step limits, tracked within the global Governor.
type BudgetPool struct {
	mu          sync.RWMutex
	allocations map[string]*Allocation
	governor    Governor
}

// NewBudgetPool creates a BudgetPool backed by the given Governor.
func NewBudgetPool(governor Governor) *BudgetPool {
	return &BudgetPool{
		allocations: make(map[string]*Allocation),
		governor:    governor,
	}
}

// Allocate creates or updates a named budget allocation.
func (p *BudgetPool) Allocate(name string, maxTokens, maxSteps int64, priority int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if a, ok := p.allocations[name]; ok {
		a.MaxTokens = maxTokens
		a.MaxSteps = maxSteps
		a.Priority = priority
		return
	}
	p.allocations[name] = &Allocation{
		Name:      name,
		MaxTokens: maxTokens,
		MaxSteps:  maxSteps,
		Priority:  priority,
	}
}

// Record records usage against a named allocation and the underlying Governor.
func (p *BudgetPool) Record(name, sessionID string, tokens, steps int) error {
	p.mu.RLock()
	a, ok := p.allocations[name]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrAllocationNotFound, name)
	}

	atomic.AddInt64(&a.UsedTokens, int64(tokens))
	atomic.AddInt64(&a.UsedSteps, int64(steps))

	p.governor.Record(sessionID, tokens, steps)
	return nil
}

// TryReserve checks if a named allocation has capacity for the requested tokens and steps.
func (p *BudgetPool) TryReserve(name string, tokens, steps int) bool {
	p.mu.RLock()
	a, ok := p.allocations[name]
	p.mu.RUnlock()
	if !ok {
		return false
	}

	usedTokens := atomic.LoadInt64(&a.UsedTokens)
	usedSteps := atomic.LoadInt64(&a.UsedSteps)

	if a.MaxTokens > 0 && usedTokens+int64(tokens) > a.MaxTokens {
		return false
	}
	if a.MaxSteps > 0 && usedSteps+int64(steps) > a.MaxSteps {
		return false
	}
	return true
}

// Snapshot returns snapshots of all allocations.
func (p *BudgetPool) Snapshot() []AllocationSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	snaps := make([]AllocationSnapshot, 0, len(p.allocations))
	for _, a := range p.allocations {
		usedTokens := atomic.LoadInt64(&a.UsedTokens)
		usedSteps := atomic.LoadInt64(&a.UsedSteps)

		var tokensPct, stepsPct float64
		if a.MaxTokens > 0 {
			tokensPct = float64(usedTokens) / float64(a.MaxTokens)
		}
		if a.MaxSteps > 0 {
			stepsPct = float64(usedSteps) / float64(a.MaxSteps)
		}

		snaps = append(snaps, AllocationSnapshot{
			Name:       a.Name,
			MaxTokens:  a.MaxTokens,
			MaxSteps:   a.MaxSteps,
			UsedTokens: usedTokens,
			UsedSteps:  usedSteps,
			Priority:   a.Priority,
			TokensPct:  tokensPct,
			StepsPct:   stepsPct,
		})
	}

	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].Name < snaps[j].Name
	})
	return snaps
}

// Preempt transfers unused budget from lower-priority allocations to the named one.
// Returns the amount of tokens and steps reclaimed.
func (p *BudgetPool) Preempt(name string, needTokens, needSteps int64) (reclaimedTokens, reclaimedSteps int64) {
	p.mu.RLock()
	target, ok := p.allocations[name]
	if !ok {
		p.mu.RUnlock()
		return 0, 0
	}

	// Collect lower-priority allocations sorted by priority ascending (lowest first).
	donors := make([]*Allocation, 0)
	for _, a := range p.allocations {
		if a.Name != name && a.Priority < target.Priority {
			donors = append(donors, a)
		}
	}
	p.mu.RUnlock()

	sort.Slice(donors, func(i, j int) bool {
		return donors[i].Priority < donors[j].Priority
	})

	for _, d := range donors {
		if needTokens <= 0 && needSteps <= 0 {
			break
		}

		// Reclaim unused tokens.
		if needTokens > 0 && d.MaxTokens > 0 {
			usedTokens := atomic.LoadInt64(&d.UsedTokens)
			spare := d.MaxTokens - usedTokens
			if spare > 0 {
				take := spare
				if take > needTokens {
					take = needTokens
				}
				d.MaxTokens -= take
				target.MaxTokens += take
				reclaimedTokens += take
				needTokens -= take
			}
		}

		// Reclaim unused steps.
		if needSteps > 0 && d.MaxSteps > 0 {
			usedSteps := atomic.LoadInt64(&d.UsedSteps)
			spare := d.MaxSteps - usedSteps
			if spare > 0 {
				take := spare
				if take > needSteps {
					take = needSteps
				}
				d.MaxSteps -= take
				target.MaxSteps += take
				reclaimedSteps += take
				needSteps -= take
			}
		}
	}
	return reclaimedTokens, reclaimedSteps
}
