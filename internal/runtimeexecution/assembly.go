package runtimeexecution

import (
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/sandbox"
)

// Install wires local path-scoped execution support services into the kernel.
// Workspace and executor ports remain backend-owned; this installer only adds
// isolation, repo-state capture, patch, and snapshot services.
func Install(k *kernel.Kernel, workspaceRoot, isolationRoot string, isolationEnabled bool) error {
	if k == nil {
		return fmt.Errorf("kernel is nil")
	}
	if k.Workspace() == nil {
		return fmt.Errorf("execution services require a workspace port")
	}
	if k.Executor() == nil {
		return fmt.Errorf("execution services require an executor port")
	}
	root, err := normalizePath(workspaceRoot)
	if err != nil {
		return fmt.Errorf("resolve execution workspace root: %w", err)
	}
	if root == "" {
		return fmt.Errorf("execution workspace root is empty")
	}
	if backendRoot, ok := kernelSandboxRoot(k); ok && !samePath(root, backendRoot) {
		return fmt.Errorf("execution workspace root %q does not match backend root %q", root, backendRoot)
	}

	var opts []kernel.Option
	if isolationEnabled {
		leaseRoot, err := normalizePath(isolationRoot)
		if err != nil {
			return fmt.Errorf("resolve execution isolation root: %w", err)
		}
		if leaseRoot == "" {
			return fmt.Errorf("execution isolation root is empty")
		}
		isolation, err := sandbox.NewLocalWorkspaceIsolation(leaseRoot)
		if err != nil {
			return fmt.Errorf("create workspace isolation: %w", err)
		}
		opts = append(opts, kernel.WithWorkspaceIsolation(isolation))
	}
	opts = append(opts,
		kernel.WithRepoStateCapture(sandbox.NewGitRepoStateCapture(root)),
		kernel.WithPatchApply(sandbox.NewGitPatchApply(root)),
		kernel.WithPatchRevert(sandbox.NewGitPatchRevert(root)),
		kernel.WithWorktreeSnapshots(sandbox.NewGitWorktreeSnapshotStore(root)),
	)
	k.Apply(opts...)
	return nil
}

func kernelSandboxRoot(k *kernel.Kernel) (string, bool) {
	if k == nil || k.Sandbox() == nil {
		return "", false
	}
	root, err := k.Sandbox().ResolvePath(".")
	if err != nil {
		return "", false
	}
	root, err = normalizePath(root)
	if err != nil || root == "" {
		return "", false
	}
	return root, true
}

func normalizePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func samePath(left, right string) bool {
	if goruntime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
