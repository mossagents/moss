package distributed

import (
	"context"
	"testing"
	"time"
)

func TestSingleNodeElection(t *testing.T) {
	e := NewInProcessElector(WithTTL(2 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := e.Campaign(ctx, "test-group")
	if err != nil {
		t.Fatalf("Campaign failed: %v", err)
	}
	defer session.Resign()

	if session.ID() == "" {
		t.Fatal("session ID should not be empty")
	}
	if session.ID() != e.NodeID() {
		t.Errorf("session ID = %q, want %q", session.ID(), e.NodeID())
	}

	// Done channel should not be closed while we hold leadership.
	select {
	case <-session.Done():
		t.Fatal("Done() channel should not be closed while leader")
	default:
	}
}

func TestResignAndReElection(t *testing.T) {
	e := NewInProcessElector(WithTTL(2 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First election.
	session1, err := e.Campaign(ctx, "test-group")
	if err != nil {
		t.Fatalf("first Campaign failed: %v", err)
	}

	// Resign leadership.
	if err := session1.Resign(); err != nil {
		t.Fatalf("Resign failed: %v", err)
	}

	// Done channel should be closed after resign.
	select {
	case <-session1.Done():
		// expected
	default:
		t.Fatal("Done() channel should be closed after Resign")
	}

	// Re-election should succeed.
	session2, err := e.Campaign(ctx, "test-group")
	if err != nil {
		t.Fatalf("second Campaign failed: %v", err)
	}
	defer session2.Resign()

	if session2.ID() == "" {
		t.Fatal("re-elected session ID should not be empty")
	}
}

func TestDoneClosesOnContextCancel(t *testing.T) {
	e := NewInProcessElector(WithTTL(2 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	session, err := e.Campaign(ctx, "test-group")
	if err != nil {
		t.Fatalf("Campaign failed: %v", err)
	}

	// Cancel the context to simulate leadership loss.
	cancel()

	select {
	case <-session.Done():
		// expected: Done closed after context cancelled
	case <-time.After(3 * time.Second):
		t.Fatal("Done() channel was not closed after context cancel")
	}
}

func TestLeaderReturnsCorrectID(t *testing.T) {
	e := NewInProcessElector(
		WithTTL(2*time.Second),
		WithNodeID("node-alpha"),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Before election, no leader.
	leader, err := e.Leader(ctx, "test-group")
	if err != nil {
		t.Fatalf("Leader failed: %v", err)
	}
	if leader != "" {
		t.Errorf("expected no leader before election, got %q", leader)
	}

	session, err := e.Campaign(ctx, "test-group")
	if err != nil {
		t.Fatalf("Campaign failed: %v", err)
	}

	// After election, leader should be our node.
	leader, err = e.Leader(ctx, "test-group")
	if err != nil {
		t.Fatalf("Leader failed: %v", err)
	}
	if leader != "node-alpha" {
		t.Errorf("Leader() = %q, want %q", leader, "node-alpha")
	}

	// After resign, leader should be cleared.
	if err := session.Resign(); err != nil {
		t.Fatalf("Resign failed: %v", err)
	}
	leader, err = e.Leader(ctx, "test-group")
	if err != nil {
		t.Fatalf("Leader failed: %v", err)
	}
	if leader != "" {
		t.Errorf("expected no leader after resign, got %q", leader)
	}
}

func TestMultipleGroupsIndependent(t *testing.T) {
	e := NewInProcessElector(WithTTL(2 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s1, err := e.Campaign(ctx, "group-a")
	if err != nil {
		t.Fatalf("Campaign group-a failed: %v", err)
	}
	defer s1.Resign()

	s2, err := e.Campaign(ctx, "group-b")
	if err != nil {
		t.Fatalf("Campaign group-b failed: %v", err)
	}
	defer s2.Resign()

	leaderA, _ := e.Leader(ctx, "group-a")
	leaderB, _ := e.Leader(ctx, "group-b")
	if leaderA == "" || leaderB == "" {
		t.Errorf("both groups should have leaders: a=%q b=%q", leaderA, leaderB)
	}
}

func TestCampaignBlocksUntilLockReleased(t *testing.T) {
	e := NewInProcessElector(WithTTL(2 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First node takes leadership.
	session1, err := e.Campaign(ctx, "test-group")
	if err != nil {
		t.Fatalf("first Campaign failed: %v", err)
	}

	// Second campaign in background; should block until session1 resigns.
	elected := make(chan LeaderSession, 1)
	go func() {
		s, err := e.Campaign(ctx, "test-group")
		if err == nil {
			elected <- s
		}
	}()

	// Give the goroutine time to start retrying.
	time.Sleep(300 * time.Millisecond)

	select {
	case <-elected:
		t.Fatal("second Campaign should not succeed while first holds the lock")
	default:
	}

	// Resign first session; second should eventually succeed.
	session1.Resign()

	select {
	case s := <-elected:
		defer s.Resign()
	case <-time.After(5 * time.Second):
		t.Fatal("second Campaign did not succeed after first resigned")
	}
}
