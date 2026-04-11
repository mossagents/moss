package session

import (
	"fmt"
	"sync/atomic"
)

var materializationDomainCounter atomic.Uint64

func nextMaterializationDomain() string {
	return fmt.Sprintf("session-domain-%d", materializationDomainCounter.Add(1))
}

// MaterializationDomain returns the runtime-only domain identifier used to
// deduplicate event materialization for this session. Cloned sessions always
// receive a distinct domain.
func (s *Session) MaterializationDomain() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.materializationDomain == "" {
		s.materializationDomain = nextMaterializationDomain()
	}
	return s.materializationDomain
}
