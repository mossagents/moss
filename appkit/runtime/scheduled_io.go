package runtime

import (
	"context"
	"strings"
	"sync"

	"github.com/mossagents/moss/kernel/port"
)

// ScheduledCaptureIO captures scheduled-job output for fallback summaries.
type ScheduledCaptureIO struct {
	mu      sync.Mutex
	stream  strings.Builder
	results []string
}

func NewScheduledCaptureIO() *ScheduledCaptureIO {
	return &ScheduledCaptureIO{}
}

func (s *ScheduledCaptureIO) Send(_ context.Context, msg port.OutputMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.Type {
	case port.OutputStream, port.OutputText:
		s.stream.WriteString(msg.Content)
		if msg.Type == port.OutputText {
			s.stream.WriteString("\n")
		}
	case port.OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			s.results = append(s.results, "error: "+strings.TrimSpace(msg.Content))
		}
	}
	return nil
}

func (s *ScheduledCaptureIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	return (&port.NoOpIO{}).Ask(context.Background(), req)
}

func (s *ScheduledCaptureIO) FinalText() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	text := strings.TrimSpace(s.stream.String())
	if text != "" {
		return text
	}
	return strings.Join(s.results, "\n")
}
