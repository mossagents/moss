package main

import (
	"errors"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

func TestRunLockServiceAcquireAndExpire(t *testing.T) {
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	service := newRunLockService(t.TempDir(), time.Minute, func() time.Time { return now })

	lease, err := service.Acquire("swarm-test")
	if err != nil {
		t.Fatalf("acquire initial lease: %v", err)
	}
	defer func() { _ = lease.Release() }()

	if _, err := service.Acquire("swarm-test"); err == nil {
		t.Fatal("expected second acquire to fail while lease is active")
	} else {
		var locked *ErrRunLocked
		if !errors.As(err, &locked) {
			t.Fatalf("expected ErrRunLocked, got %T (%v)", err, err)
		}
	}

	if err := lease.Release(); err != nil {
		t.Fatalf("release lease: %v", err)
	}

	now = now.Add(2 * time.Minute)
	if _, err := service.Acquire("swarm-test"); err != nil {
		t.Fatalf("acquire after release/expiry: %v", err)
	}
}

func TestComputeRecoverableHonorsTaskStateAndGovernance(t *testing.T) {
	running := computeRecoverable(string(session.StatusRunning), []taskrt.TaskSummary{
		{Handle: taskrt.TaskHandle{ID: "task-1"}, Status: taskrt.TaskPending},
	}, nil)
	if !running {
		t.Fatal("pending task should make run recoverable")
	}

	completed := computeRecoverable(string(session.StatusCompleted), []taskrt.TaskSummary{
		{Handle: taskrt.TaskHandle{ID: "task-1"}, Status: taskrt.TaskRunning},
	}, nil)
	if completed {
		t.Fatal("completed session must not be recoverable")
	}

	redirected := computeRecoverable(string(session.StatusFailed), []taskrt.TaskSummary{
		{Handle: taskrt.TaskHandle{ID: "task-2"}, Status: taskrt.TaskFailed},
	}, []taskrt.TaskMessage{
		{
			TaskID:   "task-2",
			Metadata: kswarm.GovernanceMetadata(kswarm.GovernanceRedirected, "redirect", nil),
		},
	})
	if !redirected {
		t.Fatal("redirected failed task should remain recoverable")
	}
}
