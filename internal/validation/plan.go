package validation

import (
	"context"
	"time"

	"github.com/mossagi/moss/internal/workspace"
)

type ValidationType string

const (
	ValidationTypeTest  ValidationType = "test"
	ValidationTypeLint  ValidationType = "lint"
	ValidationTypeBuild ValidationType = "build"
)

type ValidationStep struct {
	Type    ValidationType
	Command string
	Args    []string
	Timeout time.Duration // zero means default (60s)
}

type ValidationPlan struct {
	Steps []ValidationStep
}

type ValidationResult struct {
	Step    ValidationStep
	Success bool
	Output  string
	Error   string
}

func (p *ValidationPlan) Execute(ctx context.Context, ws *workspace.Manager) ([]ValidationResult, error) {
	var results []ValidationResult
	for _, step := range p.Steps {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}
		timeout := step.Timeout
		if timeout == 0 {
			timeout = 60 * time.Second
		}
		out, exitCode, err := ws.RunCommand(step.Command, step.Args, timeout)
		result := ValidationResult{
			Step:   step,
			Output: out,
		}
		if err != nil {
			result.Success = false
			result.Error = err.Error()
		} else {
			result.Success = exitCode == 0
			if exitCode != 0 {
				result.Error = "command exited with non-zero status"
			}
		}
		results = append(results, result)
	}
	return results, nil
}
