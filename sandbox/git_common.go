package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type gitRunner struct {
	root    string
	timeout time.Duration
}

func (g gitRunner) run(ctx context.Context, args ...string) (string, error) {
	return g.runInput(ctx, "", args...)
}

func (g gitRunner) runInput(ctx context.Context, input string, args ...string) (string, error) {
	if g.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.root
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func isGitRepoError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not a git repository")
}
