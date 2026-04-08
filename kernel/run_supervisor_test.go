package kernel

import (
	"context"
	stderrors "errors"
	kerrors "github.com/mossagents/moss/kernel/errors"
	"testing"
	"time"
)

func TestRunSupervisorBeginRejectsAfterShutdown(t *testing.T) {
	s := newRunSupervisor()
	s.beginShutdown()

	_, _, err := s.begin(context.Background(), "s-1", runKindForeground)
	if err == nil {
		t.Fatal("expected begin to fail while shutting down")
	}

	var kerr *kerrors.Error
	if !stderrors.As(err, &kerr) || kerr.Code != kerrors.ErrShutdown {
		t.Fatalf("expected ErrShutdown, got: %v", err)
	}
}

func TestRunSupervisorShutdownCancelsRunContext(t *testing.T) {
	s := newRunSupervisor()

	runCtx, runID, err := s.begin(context.Background(), "s-2", runKindForeground)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	done := make(chan struct{})
	go func() {
		<-runCtx.Done()
		s.end(runID)
		close(done)
	}()

	s.beginShutdown()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("run context was not cancelled by shutdown")
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	s.wait(waitCtx)
}

func TestRunSupervisorWaitReturnsOnContextDone(t *testing.T) {
	s := newRunSupervisor()

	_, runID, err := s.begin(context.Background(), "s-3", runKindForeground)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	start := time.Now()
	waitCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	s.wait(waitCtx)

	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("wait blocked too long after context done: %v", elapsed)
	}

	// Cleanup unfinished run record.
	s.end(runID)
}

func TestRunSupervisorBeginRejectsConcurrentSameSession(t *testing.T) {
	s := newRunSupervisor()

	_, runID, err := s.begin(context.Background(), "same-session", runKindForeground)
	if err != nil {
		t.Fatalf("first begin: %v", err)
	}
	defer s.end(runID)

	_, _, err = s.begin(context.Background(), "same-session", runKindForeground)
	if err == nil {
		t.Fatal("expected second begin to fail for same session")
	}

	var kerr *kerrors.Error
	if !stderrors.As(err, &kerr) || kerr.Code != kerrors.ErrSessionRunning {
		t.Fatalf("expected ErrSessionRunning, got: %v", err)
	}
}

func TestRunSupervisorBeginAllowsDifferentSessions(t *testing.T) {
	s := newRunSupervisor()

	_, runID1, err := s.begin(context.Background(), "s-1", runKindForeground)
	if err != nil {
		t.Fatalf("first begin: %v", err)
	}
	defer s.end(runID1)

	_, runID2, err := s.begin(context.Background(), "s-2", runKindForeground)
	if err != nil {
		t.Fatalf("second begin for different session should succeed: %v", err)
	}
	defer s.end(runID2)
}
