package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

// GitRepoStateCapture 是本地 git 仓库的结构化状态捕获实现。
type GitRepoStateCapture struct {
	root    string
	timeout time.Duration
}

func NewGitRepoStateCapture(root string) *GitRepoStateCapture {
	return &GitRepoStateCapture{
		root:    root,
		timeout: 10 * time.Second,
	}
}

func (c *GitRepoStateCapture) Capture(ctx context.Context) (*port.RepoState, error) {
	repoRoot, err := c.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		if isGitRepoError(err) {
			return nil, port.ErrRepoUnavailable
		}
		return nil, err
	}
	head, err := c.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve HEAD: %w", err)
	}
	branch, err := c.git(ctx, "branch", "--show-current")
	if err != nil {
		return nil, fmt.Errorf("resolve branch: %w", err)
	}
	statusOut, err := c.git(ctx, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching")
	if err != nil {
		return nil, fmt.Errorf("capture status: %w", err)
	}

	state := &port.RepoState{
		RepoRoot:   filepath.Clean(strings.TrimSpace(repoRoot)),
		HeadSHA:    strings.TrimSpace(head),
		Branch:     strings.TrimSpace(branch),
		Detached:   strings.TrimSpace(branch) == "",
		CapturedAt: time.Now().UTC(),
	}
	parsePorcelain(state, statusOut)
	state.IsDirty = len(state.Staged) > 0 || len(state.Unstaged) > 0 || len(state.Untracked) > 0
	return state, nil
}

func (c *GitRepoStateCapture) git(ctx context.Context, args ...string) (string, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = c.root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func parsePorcelain(state *port.RepoState, raw string) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 {
			continue
		}
		code := line[:2]
		path := normalizePorcelainPath(line[3:])
		if path == "" {
			continue
		}
		switch code {
		case "??":
			state.Untracked = append(state.Untracked, path)
			continue
		case "!!":
			state.Ignored = append(state.Ignored, path)
			continue
		}
		if code[0] != ' ' {
			state.Staged = append(state.Staged, port.RepoFileState{
				Path:   path,
				Status: string(code[0]),
			})
		}
		if code[1] != ' ' {
			state.Unstaged = append(state.Unstaged, port.RepoFileState{
				Path:   path,
				Status: string(code[1]),
			})
		}
	}
}

func normalizePorcelainPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if idx := strings.LastIndex(path, " -> "); idx >= 0 {
		path = path[idx+4:]
	}
	return filepath.Clean(strings.Trim(path, `"`))
}

func isGitRepoError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not a git repository")
}
