package approval

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/events"
)

type Service struct {
	mu       sync.Mutex
	requests map[string]*domain.ApprovalRequest
	eventBus *events.Bus
	reader   io.Reader
	writer   io.Writer
}

func New(bus *events.Bus, reader io.Reader, writer io.Writer) *Service {
	if reader == nil {
		reader = os.Stdin
	}
	if writer == nil {
		writer = os.Stdout
	}
	return &Service{
		requests: make(map[string]*domain.ApprovalRequest),
		eventBus: bus,
		reader:   reader,
		writer:   writer,
	}
}

func (s *Service) Request(ctx context.Context, req *domain.ApprovalRequest) (domain.ApprovalStatus, error) {
	s.mu.Lock()
	s.requests[req.ApprovalID] = req
	s.mu.Unlock()

	s.eventBus.Publish(events.Event{
		EventID:   req.ApprovalID,
		Type:      events.EventApprovalRequested,
		RunID:     req.RunID,
		TaskID:    req.TaskID,
		Timestamp: time.Now(),
		Payload: map[string]any{
			"tool":        req.ToolName,
			"description": req.Description,
		},
	})

	fmt.Fprintf(s.writer, "\n[APPROVAL REQUIRED]\n  Tool: %s\n  Description: %s\n  Approve? (y/n): ", req.ToolName, req.Description)

	scanner := bufio.NewScanner(s.reader)
	var answer string
	if scanner.Scan() {
		answer = strings.TrimSpace(strings.ToLower(scanner.Text()))
	}

	now := time.Now()
	req.ResolvedAt = &now

	var status domain.ApprovalStatus
	var eventType events.EventType

	if answer == "y" || answer == "yes" {
		status = domain.ApprovalStatusApproved
		eventType = events.EventApprovalApproved
	} else {
		status = domain.ApprovalStatusRejected
		eventType = events.EventApprovalRejected
	}

	req.Status = status
	s.eventBus.Publish(events.Event{
		EventID:   req.ApprovalID,
		Type:      eventType,
		RunID:     req.RunID,
		TaskID:    req.TaskID,
		Timestamp: time.Now(),
		Payload:   map[string]any{"tool": req.ToolName, "status": string(status)},
	})

	return status, nil
}

func (s *Service) Get(id string) (*domain.ApprovalRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.requests[id]
	return req, ok
}
