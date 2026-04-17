package docker_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dockersandbox "github.com/mossagents/moss/harness/sandbox/docker"
	"github.com/mossagents/moss/kernel/workspace"
)

func TestNew_Validation(t *testing.T) {
	_, err := dockersandbox.New(dockersandbox.DockerConfig{})
	if err == nil {
		t.Fatal("expected error when Image is empty")
	}
}

func TestResolvePath_EscapeRejected(t *testing.T) {
	dir := t.TempDir()
	s, err := dockersandbox.New(dockersandbox.DockerConfig{Image: "ubuntu:22.04", WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ResolvePath("../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path escape")
	}
}

func TestResolvePath_ValidPath(t *testing.T) {
	dir := t.TempDir()
	s, err := dockersandbox.New(dockersandbox.DockerConfig{Image: "ubuntu:22.04", WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ResolvePath("subdir/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "subdir", "file.txt")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadWriteFile(t *testing.T) {
	dir := t.TempDir()
	s, err := dockersandbox.New(dockersandbox.DockerConfig{Image: "ubuntu:22.04", WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	content := []byte("hello docker sandbox")
	if err := s.WriteFile(ctx, "test.txt", content); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadFile(ctx, "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("got %q, want %q", got, content)
	}
}

func TestMaxFileSizeEnforced(t *testing.T) {
	dir := t.TempDir()
	s, err := dockersandbox.New(dockersandbox.DockerConfig{Image: "ubuntu:22.04", WorkDir: dir, MaxFileSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	err = s.WriteFile(context.Background(), "big.txt", make([]byte, 11))
	if err == nil {
		t.Fatal("expected file size limit error")
	}
}

func TestListFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := dockersandbox.New(dockersandbox.DockerConfig{Image: "ubuntu:22.04", WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	files, err := s.ListFiles(context.Background(), "*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
}

func TestExecute_MockSuccess(t *testing.T) {
	dir := t.TempDir()
	s, err := dockersandbox.New(dockersandbox.DockerConfig{
		Image:   "ubuntu:22.04",
		WorkDir: dir,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Inject mock exec function that simulates docker output
	s.SetExecFunc(func(_ context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		return []byte("hello"), nil, 0, nil
	})
	out, err := s.Execute(context.Background(), workspace.ExecRequest{Command: "echo", Args: []string{"hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Stdout != "hello" {
		t.Fatalf("expected 'hello', got %q", out.Stdout)
	}
}

func TestLimits(t *testing.T) {
	dir := t.TempDir()
	s, err := dockersandbox.New(dockersandbox.DockerConfig{
		Image:       "ubuntu:22.04",
		WorkDir:     dir,
		MaxFileSize: 1024,
		Timeout:     10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	limits := s.Limits()
	if limits.MaxFileSize != 1024 {
		t.Fatalf("expected MaxFileSize=1024, got %d", limits.MaxFileSize)
	}
	if limits.CommandTimeout != 10*time.Second {
		t.Fatalf("expected 10s timeout")
	}
}

func TestExecute_DockerArgsContainSecurityFlags(t *testing.T) {
	dir := t.TempDir()
	s, err := dockersandbox.New(dockersandbox.DockerConfig{
		Image:   "ubuntu:22.04",
		WorkDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	var capturedArgs []string
	s.SetExecFunc(func(_ context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		capturedArgs = args
		return nil, nil, 0, nil
	})
	s.Execute(context.Background(), workspace.ExecRequest{Command: "echo", Args: []string{"test"}})

	argStr := strings.Join(capturedArgs, " ")
	for _, flag := range []string{
		"--security-opt no-new-privileges",
		"--cap-drop ALL",
		"--pids-limit 256",
		"--read-only",
		"--tmpfs /tmp:rw,noexec,nosuid,size=64m",
	} {
		if !strings.Contains(argStr, flag) {
			t.Errorf("expected docker args to contain %q, got: %v", flag, capturedArgs)
		}
	}
}
