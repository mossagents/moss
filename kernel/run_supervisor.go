package kernel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
)

type runKind string

const (
	runKindForeground runKind = "foreground"
	runKindWithUserIO runKind = "with_userio"
	runKindDelegated  runKind = "delegated"
)

type runRecord struct {
	id        string
	sessionID string
	kind      runKind
	startedAt time.Time
	cancel    context.CancelFunc
}

type runSupervisor struct {
	mu      sync.Mutex
	closing bool
	runs    map[string]runRecord
	wg      sync.WaitGroup
	nextID  uint64
}

func newRunSupervisor() *runSupervisor {
	return &runSupervisor{runs: make(map[string]runRecord)}
}

func (s *runSupervisor) begin(parent context.Context, sessionID string, kind runKind) (context.Context, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closing {
		return nil, "", kerrors.New(kerrors.ErrShutdown, "kernel is shutting down")
	}

	id := fmt.Sprintf("run_%d", atomic.AddUint64(&s.nextID, 1))
	runCtx, cancel := context.WithCancel(parent)
	s.runs[id] = runRecord{
		id:        id,
		sessionID: sessionID,
		kind:      kind,
		startedAt: time.Now(),
		cancel:    cancel,
	}
	s.wg.Add(1)

	return runCtx, id, nil
}

func (s *runSupervisor) end(runID string) {
	s.mu.Lock()
	if rec, ok := s.runs[runID]; ok {
		delete(s.runs, runID)
		rec.cancel()
	}
	s.mu.Unlock()
	s.wg.Done()
}

func (s *runSupervisor) beginShutdown() {
	s.mu.Lock()
	s.closing = true
	for _, rec := range s.runs {
		rec.cancel()
	}
	s.mu.Unlock()
}

func (s *runSupervisor) wait(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
}
