package declarative

import (
	"bytes"
	"context"
	"os/exec"

	"github.com/mossagents/moss/kernel/hooks"
)

// commandHook executes a shell command and blocks the tool call if the command
// exits with a non-zero status (when block_on_failure is true).
func commandHook(cfg HookConfig) hooks.Hook[hooks.ToolEvent] {
	return func(ctx context.Context, ev *hooks.ToolEvent) error {
		timeout := hookTimeout(cfg)
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "sh", "-c", cfg.Command)
		cmd.Env = append(cmd.Environ(), toolEnv(ev)...)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			reason := "exit non-zero"
			if stderr.Len() > 0 {
				reason = stderr.String()
			}
			return hookError(cfg.Name, reason, cfg.BlockOnFailure)
		}
		return nil
	}
}
