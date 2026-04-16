package declarative

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel/hooks"
)

// promptHook is a placeholder for LLM-based hook evaluation. A full
// implementation requires access to model.LLM, which can be injected at
// compile time. This minimal version always blocks with the configured prompt
// as the reason when block_on_failure is true.
func promptHook(cfg HookConfig) hooks.Hook[hooks.ToolEvent] {
	return func(ctx context.Context, ev *hooks.ToolEvent) error {
		// In a full implementation, this would call the LLM with the prompt
		// and the tool context to get a yes/no decision. For now, we surface
		// the prompt as a warning if block_on_failure is set.
		if cfg.BlockOnFailure {
			return fmt.Errorf("declarative hook %q (prompt): %s [tool: %s]",
				cfg.Name, cfg.Prompt, ev.Tool.Name)
		}
		return nil
	}
}
