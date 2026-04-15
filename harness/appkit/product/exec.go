package product

import (
	"context"
	"github.com/mossagents/moss/kernel/io"
	rpolicy "github.com/mossagents/moss/harness/runtime/policy"
	"strings"
	"sync"
)

type ExecOutputEvent struct {
	Type    io.OutputType `json:"type"`
	Content string        `json:"content"`
	IsError bool          `json:"is_error,omitempty"`
}

type ExecReport struct {
	App              string            `json:"app"`
	Goal             string            `json:"goal"`
	Workspace        string            `json:"workspace"`
	Provider         string            `json:"provider"`
	Model            string            `json:"model,omitempty"`
	Trust            string            `json:"trust"`
	ApprovalMode     string            `json:"approval_mode"`
	SessionID        string            `json:"session_id,omitempty"`
	Status           string            `json:"status"`
	Steps            int               `json:"steps,omitempty"`
	PromptTokens     int               `json:"prompt_tokens,omitempty"`
	CompletionTokens int               `json:"completion_tokens,omitempty"`
	Tokens           int               `json:"tokens,omitempty"`
	EstimatedCostUSD float64           `json:"estimated_cost_usd,omitempty"`
	Output           string            `json:"output,omitempty"`
	Error            string            `json:"error,omitempty"`
	Events           []ExecOutputEvent `json:"events,omitempty"`
	Trace            []TraceEvent      `json:"trace,omitempty"`
}

type RecordingIO struct {
	mode   string
	mu     sync.Mutex
	events []ExecOutputEvent
}

func NewRecordingIO(mode string) *RecordingIO {
	return &RecordingIO{mode: rpolicy.NormalizeApprovalMode(mode)}
}

func (r *RecordingIO) Send(_ context.Context, msg io.OutputMessage) error {
	event := ExecOutputEvent{
		Type:    msg.Type,
		Content: strings.TrimSpace(msg.Content),
	}
	if msg.Type == io.OutputToolResult {
		event.IsError, _ = msg.Meta["is_error"].(bool)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

func (r *RecordingIO) Ask(_ context.Context, req io.InputRequest) (io.InputResponse, error) {
	return recordingResponse(r.mode, req), nil
}

func (r *RecordingIO) Events() []ExecOutputEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ExecOutputEvent, len(r.events))
	copy(out, r.events)
	return out
}

func recordingResponse(mode string, req io.InputRequest) io.InputResponse {
	approved := mode == ApprovalModeFullAuto
	switch req.Type {
	case io.InputConfirm:
		resp := io.InputResponse{Approved: approved}
		if req.Approval != nil {
			source := "recording-deny"
			if approved {
				source = "recording-auto"
			}
			resp.Decision = &io.ApprovalDecision{
				RequestID: req.Approval.ID,
				Approved:  approved,
				Source:    source,
			}
		}
		return resp
	case io.InputSelect:
		return io.InputResponse{Selected: 0}
	case io.InputForm:
		form := make(map[string]any, len(req.Fields))
		for _, field := range req.Fields {
			if field.Default != nil {
				form[field.Name] = field.Default
				continue
			}
			switch field.Type {
			case io.InputFieldBoolean:
				form[field.Name] = approved
			case io.InputFieldMultiSelect:
				form[field.Name] = []string{}
			case io.InputFieldSingleSelect:
				if len(field.Options) > 0 {
					form[field.Name] = field.Options[0]
				} else {
					form[field.Name] = ""
				}
			default:
				form[field.Name] = ""
			}
		}
		return io.InputResponse{Form: form}
	default:
		return io.InputResponse{}
	}
}
