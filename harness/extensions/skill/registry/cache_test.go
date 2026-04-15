package registry

import (
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
)

func TestNewLocalCacheDefaultsToCurrentAppDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	appconfig.SetAppName("mosscode")
	t.Cleanup(func() { appconfig.SetAppName("moss") })

	cache, err := NewLocalCache("")
	if err != nil {
		t.Fatalf("NewLocalCache: %v", err)
	}
	want := filepath.Join(home, ".mosscode", "skills")
	if cache.root != want {
		t.Fatalf("cache.root = %q, want %q", cache.root, want)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Fatalf("expected cache dir %q to exist, err=%v", want, err)
	}
}
