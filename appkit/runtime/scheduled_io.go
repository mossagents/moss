package runtime

import (
	"context"
	intr "github.com/mossagents/moss/kernel/io"
	"strings"
	"sync"
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

func (s *ScheduledCaptureIO) Send(_ context.Context, msg intr.OutputMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.Type {
	case intr.OutputStream, intr.OutputText:
		s.stream.WriteString(msg.Content)
		if msg.Type == intr.OutputText {
			s.stream.WriteString("\n")
		}
	case intr.OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			s.results = append(s.results, "error: "+strings.TrimSpace(msg.Content))
		}
	}
	return nil
}

func (s *ScheduledCaptureIO) Ask(_ context.Context, req intr.InputRequest) (intr.InputResponse, error) {
	return (&intr.NoOpIO{}).Ask(context.Background(), req)
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
