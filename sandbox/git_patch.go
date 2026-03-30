package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

// GitPatchApply 使用 git apply 在本地仓库中应用结构化补丁。
type GitPatchApply struct {
	root    string
	timeout time.Duration
}

func NewGitPatchApply(root string) *GitPatchApply {
	return &GitPatchApply{
		root:    root,
		timeout: 10 * time.Second,
	}
}

func (g *GitPatchApply) Apply(ctx context.Context, req port.PatchApplyRequest) (*port.PatchApplyResult, error) {
	patch := req.Patch
	if strings.TrimSpace(patch) == "" {
		return nil, fmt.Errorf("patch is required")
	}
	repoRoot, _, journal, err := resolveGitRepo(ctx, g.root, g.timeout)
	if err != nil {
		if isGitRepoError(err) {
			return nil, port.ErrPatchApplyUnavailable
		}
		return nil, err
	}
	runner := gitRunner{root: repoRoot, timeout: g.timeout}
	args := []string{"apply"}
	if req.ThreeWay {
		args = append(args, "--3way")
	}
	if req.Cached {
		args = append(args, "--cached")
	}
	args = append(args, "--whitespace=nowarn")

	result := &port.PatchApplyResult{
		PatchID:     patchID(patch),
		TargetFiles: patchTargets(patch),
		Cached:      req.Cached,
		ThreeWay:    req.ThreeWay,
		Source:      req.Source,
		AppliedAt:   time.Now().UTC(),
	}
	if _, err := runner.runInput(ctx, patch, args...); err != nil {
		result.Error = err.Error()
		return result, err
	}
	result.Applied = true
	if err := journal.Save(result.PatchID, patchJournalEntry{
		Patch:       patch,
		TargetFiles: append([]string(nil), result.TargetFiles...),
		ThreeWay:    req.ThreeWay,
		Cached:      req.Cached,
		Source:      req.Source,
		AppliedAt:   result.AppliedAt,
	}); err != nil {
		result.Error = err.Error()
		return result, err
	}
	return result, nil
}

// GitPatchRevert 使用 patch journal 或 repo capture 回滚本地仓库变更。
type GitPatchRevert struct {
	root    string
	timeout time.Duration
}

func NewGitPatchRevert(root string) *GitPatchRevert {
	return &GitPatchRevert{
		root:    root,
		timeout: 10 * time.Second,
	}
}

func (g *GitPatchRevert) Revert(ctx context.Context, req port.PatchRevertRequest) (*port.PatchRevertResult, error) {
	repoRoot, _, journal, err := resolveGitRepo(ctx, g.root, g.timeout)
	if err != nil {
		if isGitRepoError(err) {
			return nil, port.ErrPatchRevertUnavailable
		}
		return nil, err
	}
	if req.Capture != nil {
		return g.revertToCapture(ctx, repoRoot, req.Capture, req)
	}
	patchID := strings.TrimSpace(req.PatchID)
	if patchID == "" {
		return nil, fmt.Errorf("patch_id or capture is required")
	}
	entry, err := journal.Load(patchID)
	if err != nil {
		return nil, err
	}
	runner := gitRunner{root: repoRoot, timeout: g.timeout}
	args := []string{"apply", "-R"}
	if entry.ThreeWay {
		args = append(args, "--3way")
	}
	if entry.Cached {
		args = append(args, "--cached")
	}
	args = append(args, "--whitespace=nowarn")
	result := &port.PatchRevertResult{
		PatchID:     patchID,
		Mode:        "patch",
		TargetFiles: append([]string(nil), entry.TargetFiles...),
		RevertedAt:  time.Now().UTC(),
	}
	if _, err := runner.runInput(ctx, entry.Patch, args...); err != nil {
		result.Error = err.Error()
		return result, err
	}
	result.Reverted = true
	if err := journal.Delete(patchID); err != nil {
		result.Error = err.Error()
		return result, err
	}
	return result, nil
}

func (g *GitPatchRevert) revertToCapture(ctx context.Context, repoRoot string, capture *port.RepoState, req port.PatchRevertRequest) (*port.PatchRevertResult, error) {
	if capture == nil || capture.HeadSHA == "" {
		return nil, fmt.Errorf("capture with head_sha is required")
	}
	runner := gitRunner{root: repoRoot, timeout: g.timeout}
	current, err := NewGitRepoStateCapture(repoRoot).Capture(ctx)
	if err != nil && err != port.ErrRepoUnavailable {
		return nil, err
	}
	restoreTracked := req.RestoreTracked || (!req.RestoreTracked && !req.RestoreUntracked)
	restoreUntracked := req.RestoreUntracked || (!req.RestoreTracked && !req.RestoreUntracked)
	result := &port.PatchRevertResult{
		Mode:       "capture",
		RevertedAt: time.Now().UTC(),
	}
	targets := map[string]bool{}
	if restoreTracked {
		if _, err := runner.run(ctx, "restore", "--source", capture.HeadSHA, "--staged", "--worktree", "."); err != nil {
			result.Error = err.Error()
			return result, err
		}
		if current != nil {
			for _, item := range current.Staged {
				targets[item.Path] = true
			}
			for _, item := range current.Unstaged {
				targets[item.Path] = true
			}
		}
	}
	if restoreUntracked && current != nil {
		allowed := make(map[string]bool, len(capture.Untracked))
		for _, path := range capture.Untracked {
			allowed[path] = true
		}
		for _, path := range current.Untracked {
			if allowed[path] {
				continue
			}
			if err := removeRepoPath(repoRoot, path); err != nil {
				result.Error = err.Error()
				return result, err
			}
			targets[path] = true
		}
	}
	result.TargetFiles = mapKeysSorted(targets)
	result.Reverted = true
	return result, nil
}

func patchID(patch string) string {
	sum := sha256.Sum256([]byte(patch))
	return hex.EncodeToString(sum[:8])
}

func patchTargets(patch string) []string {
	seen := map[string]bool{}
	var files []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "+++ "):
			path := normalizePatchPath(strings.TrimPrefix(line, "+++ "))
			if path != "" && !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
		case strings.HasPrefix(line, "rename to "):
			path := normalizePatchPath(strings.TrimPrefix(line, "rename to "))
			if path != "" && !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
		}
	}
	return files
}

func normalizePatchPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/dev/null" {
		return ""
	}
	path = strings.Trim(path, `"`)
	path = strings.TrimPrefix(path, "b/")
	path = strings.TrimPrefix(path, "a/")
	return filepath.Clean(path)
}

func removeRepoPath(repoRoot, rel string) error {
	target := filepath.Join(repoRoot, rel)
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat path %s: %w", rel, err)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove path %s: %w", rel, err)
	}
	return nil
}

func mapKeysSorted(items map[string]bool) []string {
	out := make([]string, 0, len(items))
	for item := range items {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
