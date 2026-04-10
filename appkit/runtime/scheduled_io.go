package runtime

import (
	"context"
	"github.com/mossagents/moss/kernel/io"
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

func (s *ScheduledCaptureIO) Send(_ context.Context, msg io.OutputMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.Type {
	case io.OutputStream, io.OutputText:
		s.stream.WriteString(msg.Content)
		if msg.Type == io.OutputText {
			s.stream.WriteString("\n")
		}
	case io.OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			s.results = append(s.results, "error: "+strings.TrimSpace(msg.Content))
		}
	}
	return nil
}

func (s *ScheduledCaptureIO) Ask(_ context.Context, req io.InputRequest) (io.InputResponse, error) {
	return (&io.NoOpIO{}).Ask(context.Background(), req)
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
