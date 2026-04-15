package distributed_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mossagents/moss/harness/distributed"
)

func newLockTestServer() (*httptest.Server, *distributed.TokenLock) {
	s := distributed.NewLockServer()
	ts := httptest.NewServer(s.Handler())
	lk := distributed.NewTokenLock(ts.URL)
	return ts, lk
}

func TestTokenLock_AcquireReleaseRefresh(t *testing.T) {
	ts, lk := newLockTestServer()
	defer ts.Close()

	ctx := context.Background()

	// Acquire
	token, err := lk.Acquire(ctx, "file.go", 5*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Double-acquire should fail
	_, err2 := lk.Acquire(ctx, "file.go", 5*time.Second)
	if err2 == nil {
		t.Fatal("expected conflict error on double-acquire")
	}

	// Refresh the TTL
	if err := lk.Refresh(ctx, "file.go", token, 10*time.Second); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Release
	if err := lk.Release(ctx, "file.go", token); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Can acquire again after release
	token2, err := lk.Acquire(ctx, "file.go", 5*time.Second)
	if err != nil {
		t.Fatalf("re-Acquire after release: %v", err)
	}
	_ = lk.Release(ctx, "file.go", token2)
}

func TestTokenLock_ReleaseWrongToken(t *testing.T) {
	ts, lk := newLockTestServer()
	defer ts.Close()

	ctx := context.Background()
	_, err := lk.Acquire(ctx, "res2", 5*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Release with wrong token → error
	err2 := lk.Release(ctx, "res2", "wrong-token")
	if err2 == nil {
		t.Fatal("expected error when releasing with wrong token")
	}
}

func TestTokenLock_RefreshWrongToken(t *testing.T) {
	ts, lk := newLockTestServer()
	defer ts.Close()

	ctx := context.Background()
	_, err := lk.Acquire(ctx, "res3", 5*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	err2 := lk.Refresh(ctx, "res3", "bad-token", 5*time.Second)
	if err2 == nil {
		t.Fatal("expected error when refreshing with wrong token")
	}
}

func TestLockServer_UnknownRoute(t *testing.T) {
	s := distributed.NewLockServer()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// PUT with only 1 path segment → not matched → 404
	lk := distributed.NewTokenLock(ts.URL)
	ctx := context.Background()
	// Refresh needs 2 path segments; calling with resource only (1 segment) via PUT
	// This is exercised indirectly by calling Release with an unheld resource
	err := lk.Release(ctx, "nope", "no-token")
	if err == nil {
		t.Fatal("expected error releasing unheld lock")
	}
}
