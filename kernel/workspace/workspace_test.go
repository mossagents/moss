package workspace_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/workspace"
)

// ——— InProcessWorkspaceLock ———

func TestInProcessWorkspaceLock_LockAndUnlock(t *testing.T) {
	l := workspace.NewInProcessWorkspaceLock()

	unlock, err := l.Lock(context.Background(), "/path/a", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	holder, held := l.CurrentHolder("/path/a")
	if !held {
		t.Fatal("expected lock to be held")
	}
	if holder != "agent1" {
		t.Errorf("expected holder=agent1, got %q", holder)
	}
	unlock()
	_, held = l.CurrentHolder("/path/a")
	if held {
		t.Fatal("expected lock to be released after unlock")
	}
}

func TestInProcessWorkspaceLock_TryLock_Success(t *testing.T) {
	l := workspace.NewInProcessWorkspaceLock()

	unlock, ok := l.TryLock(context.Background(), "/path/b", "agent1")
	if !ok {
		t.Fatal("expected TryLock to succeed")
	}
	defer unlock()
}

func TestInProcessWorkspaceLock_TryLock_Blocked(t *testing.T) {
	l := workspace.NewInProcessWorkspaceLock()

	unlock1, ok := l.TryLock(context.Background(), "/path/c", "agent1")
	if !ok {
		t.Fatal("first TryLock should succeed")
	}
	defer unlock1()

	_, ok = l.TryLock(context.Background(), "/path/c", "agent2")
	if ok {
		t.Fatal("second TryLock should fail (lock already held)")
	}
}

func TestInProcessWorkspaceLock_TryLock_AfterUnlock(t *testing.T) {
	l := workspace.NewInProcessWorkspaceLock()

	unlock1, ok := l.TryLock(context.Background(), "/path/d", "agent1")
	if !ok {
		t.Fatal("first TryLock should succeed")
	}
	unlock1()

	unlock2, ok := l.TryLock(context.Background(), "/path/d", "agent2")
	if !ok {
		t.Fatal("TryLock after unlock should succeed")
	}
	defer unlock2()
}

func TestInProcessWorkspaceLock_CurrentHolder_NoLock(t *testing.T) {
	l := workspace.NewInProcessWorkspaceLock()

	_, held := l.CurrentHolder("/no/lock")
	if held {
		t.Fatal("expected not held for never-locked path")
	}
}

func TestInProcessWorkspaceLock_DifferentPaths_Independent(t *testing.T) {
	l := workspace.NewInProcessWorkspaceLock()

	unlock1, ok := l.TryLock(context.Background(), "/path/x", "a1")
	if !ok {
		t.Fatal("lock x should succeed")
	}
	defer unlock1()

	unlock2, ok := l.TryLock(context.Background(), "/path/y", "a2")
	if !ok {
		t.Fatal("lock y should succeed independently")
	}
	defer unlock2()
}

func TestInProcessWorkspaceLock_Lock_ContextCancelled(t *testing.T) {
	l := workspace.NewInProcessWorkspaceLock()

	// Hold the lock
	unlock, _ := l.TryLock(context.Background(), "/path/e", "agent1")
	defer unlock()

	// Try to Lock with an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := l.Lock(ctx, "/path/e", "agent2")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestInProcessWorkspaceLock_ConcurrentLocking(t *testing.T) {
	l := workspace.NewInProcessWorkspaceLock()
	path := "/shared/path"
	const n = 10

	var mu sync.Mutex
	var counter int
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			unlock, err := l.Lock(ctx, path, "agent")
			if err != nil {
				t.Errorf("Lock failed: %v", err)
				return
			}
			mu.Lock()
			counter++
			mu.Unlock()
			unlock()
		}(i)
	}

	wg.Wait()
	if counter != n {
		t.Errorf("expected %d increments, got %d", n, counter)
	}
}

// ——— NoOpWorkspaceLock ———

func TestNoOpWorkspaceLock_Lock(t *testing.T) {
	l := workspace.NoOpWorkspaceLock{}

	unlock, err := l.Lock(context.Background(), "/any", "agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	unlock() // should not panic
}

func TestNoOpWorkspaceLock_TryLock(t *testing.T) {
	l := workspace.NoOpWorkspaceLock{}

	unlock, ok := l.TryLock(context.Background(), "/any", "agent")
	if !ok {
		t.Fatal("expected ok=true for NoOpWorkspaceLock")
	}
	unlock()
}

func TestNoOpWorkspaceLock_CurrentHolder(t *testing.T) {
	l := workspace.NoOpWorkspaceLock{}
	_, held := l.CurrentHolder("/any")
	if held {
		t.Fatal("NoOp lock should never report held")
	}
}

// ——— NoOpExecutor ———

func TestNoOpExecutor_Execute(t *testing.T) {
	ex := workspace.NoOpExecutor{}
	_, err := ex.Execute(context.Background(), workspace.ExecRequest{Command: "ls"})
	if err == nil {
		t.Fatal("expected error from NoOpExecutor")
	}
}
