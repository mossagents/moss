package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigureDebugFileWhenEnabled_DisabledByDefault(t *testing.T) {
	t.Setenv("MOSS_DEBUG", "0")
	enabled, path, closer, err := ConfigureDebugFileWhenEnabled(t.TempDir())
	if err != nil {
		t.Fatalf("ConfigureDebugFileWhenEnabled: %v", err)
	}
	if enabled {
		t.Fatal("expected disabled when MOSS_DEBUG!=1")
	}
	if path != "" || closer != nil {
		t.Fatalf("unexpected debug setup: path=%q closer=%v", path, closer)
	}
}

func TestConfigureDebugFileWhenEnabled_EnabledWritesFile(t *testing.T) {
	t.Setenv("MOSS_DEBUG", "1")
	dir := t.TempDir()
	enabled, path, closer, err := ConfigureDebugFileWhenEnabled(dir)
	if err != nil {
		t.Fatalf("ConfigureDebugFileWhenEnabled: %v", err)
	}
	if !enabled {
		t.Fatal("expected enabled when MOSS_DEBUG=1")
	}
	if path != filepath.Join(dir, "debug.log") {
		t.Fatalf("unexpected path %q", path)
	}
	if closer == nil {
		t.Fatal("expected closer")
	}
	GetLogger().Debug("debug-test-line", slog.String("k", "v"))
	if err := closer.Close(); err != nil {
		t.Fatalf("close debug file: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected debug log content")
	}
}
