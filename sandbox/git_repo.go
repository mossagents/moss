package sandbox

import (
	"context"
	"fmt"
	kws "github.com/mossagents/moss/kernel/workspace"
	"path/filepath"
	"strings"
	"time"
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

func (c *GitRepoStateCapture) Capture(ctx context.Context) (*kws.RepoState, error) {
	runner := gitRunner{root: c.root, timeout: c.timeout}
	repoRoot, err := runner.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		if isGitRepoError(err) {
			return nil, kws.ErrRepoUnavailable
		}
		return nil, err
	}
	head, err := runner.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve HEAD: %w", err)
	}
	branch, err := runner.run(ctx, "branch", "--show-current")
	if err != nil {
		return nil, fmt.Errorf("resolve branch: %w", err)
	}
	statusOut, err := runner.run(ctx, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching")
	if err != nil {
		return nil, fmt.Errorf("capture status: %w", err)
	}

	state := &kws.RepoState{
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

func parsePorcelain(state *kws.RepoState, raw string) {
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
			state.Staged = append(state.Staged, kws.RepoFileState{
				Path:   path,
				Status: string(code[0]),
			})
		}
		if code[1] != ' ' {
			state.Unstaged = append(state.Unstaged, kws.RepoFileState{
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
