package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
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
	runner := gitRunner{root: g.root, timeout: g.timeout}
	if _, err := runner.run(ctx, "rev-parse", "--show-toplevel"); err != nil {
		if isGitRepoError(err) {
			return nil, port.ErrPatchApplyUnavailable
		}
		return nil, err
	}
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
		AppliedAt:   time.Now().UTC(),
	}
	if _, err := runner.runInput(ctx, patch, args...); err != nil {
		result.Error = err.Error()
		return result, err
	}
	result.Applied = true
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
