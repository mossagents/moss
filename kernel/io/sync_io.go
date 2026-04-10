package io

import (
	"context"
	"sync"
)

// SyncIO wraps any UserIO implementation with a mutex to serialize
// Send and Ask calls. Use this when the underlying UserIO is not
// goroutine-safe and callers may invoke it from multiple goroutines
// (e.g. parallel tool execution).
type SyncIO struct {
	inner UserIO
	mu    sync.Mutex
}

var _ UserIO = (*SyncIO)(nil)

// NewSyncIO returns a thread-safe wrapper around inner.
// If inner is nil, a NoOpIO is used.
func NewSyncIO(inner UserIO) *SyncIO {
	if inner == nil {
		inner = &NoOpIO{}
	}
	return &SyncIO{inner: inner}
}

func (s *SyncIO) Send(ctx context.Context, msg OutputMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Send(ctx, msg)
}

func (s *SyncIO) Ask(ctx context.Context, req InputRequest) (InputResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Ask(ctx, req)
}

// Unwrap returns the underlying UserIO.
func (s *SyncIO) Unwrap() UserIO {
	return s.inner
}
