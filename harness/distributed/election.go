package distributed

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/ids"
)

// LeaderElector manages leader election for a named group.
type LeaderElector interface {
	// Campaign starts a campaign for leadership of group.
	// Blocks until elected or context cancelled. Returns a LeaderSession.
	Campaign(ctx context.Context, group string) (LeaderSession, error)
	// Leader returns the current leader ID for a group, or empty if none.
	Leader(ctx context.Context, group string) (string, error)
}

// LeaderSession represents an active leadership tenure.
type LeaderSession interface {
	// ID returns this leader's unique identifier.
	ID() string
	// Done returns a channel closed when leadership is lost.
	Done() <-chan struct{}
	// Resign voluntarily gives up leadership.
	Resign() error
}

// ElectorOption configures a LockBasedElector.
type ElectorOption func(*LockBasedElector)

// WithTTL sets the lock TTL for leader election (default: 15s).
func WithTTL(ttl time.Duration) ElectorOption {
	return func(e *LockBasedElector) { e.ttl = ttl }
}

// WithNodeID sets a custom node ID instead of a generated UUID.
func WithNodeID(id string) ElectorOption {
	return func(e *LockBasedElector) { e.nodeID = id }
}

const (
	defaultTTL          = 15 * time.Second
	minRetryInterval    = 500 * time.Millisecond
	maxRetryInterval    = 10 * time.Second
	retryBackoffFactor  = 2.0
	retryJitterFraction = 0.3
)

// LockBasedElector implements LeaderElector using a DistributedLock.
type LockBasedElector struct {
	lock   DistributedLock
	nodeID string
	ttl    time.Duration

	mu      sync.RWMutex
	leaders map[string]string // group → leader node ID
}

// NewLockBasedElector creates a LeaderElector backed by the given lock.
func NewLockBasedElector(lock DistributedLock, opts ...ElectorOption) *LockBasedElector {
	e := &LockBasedElector{
		lock:    lock,
		nodeID:  ids.New(),
		ttl:     defaultTTL,
		leaders: make(map[string]string),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Campaign blocks until this node becomes the leader for group or ctx is cancelled.
func (e *LockBasedElector) Campaign(ctx context.Context, group string) (LeaderSession, error) {
	resource := "election/" + group
	retryInterval := minRetryInterval

	for {
		token, err := e.lock.Acquire(ctx, resource, e.ttl)
		if err == nil {
			// Won the election.
			e.setLeader(group, e.nodeID)
			return e.newSession(ctx, group, resource, token), nil
		}

		// Exponential backoff with jitter.
		jitter := time.Duration(float64(retryInterval) * retryJitterFraction * (rand.Float64()*2 - 1))
		wait := retryInterval + jitter
		if wait < 0 {
			wait = minRetryInterval
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}

		retryInterval = time.Duration(float64(retryInterval) * retryBackoffFactor)
		if retryInterval > maxRetryInterval {
			retryInterval = maxRetryInterval
		}
	}
}

// Leader returns the current leader node ID for group, or "" if none is known.
func (e *LockBasedElector) Leader(_ context.Context, group string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.leaders[group], nil
}

func (e *LockBasedElector) setLeader(group, nodeID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.leaders[group] = nodeID
}

func (e *LockBasedElector) clearLeader(group string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.leaders, group)
}

func (e *LockBasedElector) newSession(ctx context.Context, group, resource, token string) *lockBasedSession {
	s := &lockBasedSession{
		elector:  e,
		id:       e.nodeID,
		group:    group,
		resource: resource,
		token:    token,
		done:     make(chan struct{}),
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	go s.refreshLoop(sessionCtx)
	return s
}

// lockBasedSession implements LeaderSession.
type lockBasedSession struct {
	elector  *LockBasedElector
	id       string
	group    string
	resource string
	token    string

	done   chan struct{}
	cancel context.CancelFunc

	closeOnce sync.Once
}

func (s *lockBasedSession) ID() string            { return s.id }
func (s *lockBasedSession) Done() <-chan struct{} { return s.done }

// Resign voluntarily gives up leadership.
func (s *lockBasedSession) Resign() error {
	s.cancel()
	s.closeDone()
	s.elector.clearLeader(s.group)

	// Best-effort release; use a background context since session ctx is cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.elector.lock.Release(ctx, s.resource, s.token)
}

// refreshLoop periodically refreshes the lock TTL.
// When the refresh fails or the context is cancelled, it closes the Done channel.
func (s *lockBasedSession) refreshLoop(ctx context.Context) {
	refreshInterval := s.elector.ttl / 3
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.closeDone()
			s.elector.clearLeader(s.group)
			return
		case <-ticker.C:
			if err := s.elector.lock.Refresh(ctx, s.resource, s.token, s.elector.ttl); err != nil {
				s.closeDone()
				s.elector.clearLeader(s.group)
				return
			}
		}
	}
}

func (s *lockBasedSession) closeDone() {
	s.closeOnce.Do(func() { close(s.done) })
}

// compile-time interface checks
var _ LeaderElector = (*LockBasedElector)(nil)
var _ LeaderSession = (*lockBasedSession)(nil)

// ---- InProcessElector (single-process, for tests) -------------------------

// InProcessElector is a simple in-memory leader elector for testing and
// single-process scenarios. It uses an InProcessLock internally.
type InProcessElector struct {
	inner *LockBasedElector
}

// NewInProcessElector creates an InProcessElector with the given options.
func NewInProcessElector(opts ...ElectorOption) *InProcessElector {
	return &InProcessElector{
		inner: NewLockBasedElector(NewInProcessLock(), opts...),
	}
}

// Campaign delegates to the underlying LockBasedElector.
func (e *InProcessElector) Campaign(ctx context.Context, group string) (LeaderSession, error) {
	return e.inner.Campaign(ctx, group)
}

// Leader delegates to the underlying LockBasedElector.
func (e *InProcessElector) Leader(ctx context.Context, group string) (string, error) {
	return e.inner.Leader(ctx, group)
}

// NodeID returns the underlying elector's node identifier.
func (e *InProcessElector) NodeID() string {
	return e.inner.nodeID
}

// compile-time interface check
var _ LeaderElector = (*InProcessElector)(nil)

// ---- errors ---------------------------------------------------------------

// ErrNotLeader is returned when an operation requires leadership but the node
// is not the current leader.
var ErrNotLeader = fmt.Errorf("distributed: not the leader")
