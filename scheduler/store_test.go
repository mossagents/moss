package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileJobStoreSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	store, err := NewFileJobStore(path)
	if err != nil {
		t.Fatalf("NewFileJobStore: %v", err)
	}

	jobs := []Job{
		{ID: "job1", Schedule: "@every 1h", Goal: "test1"},
		{ID: "job2", Schedule: "@once", Goal: "test2"},
	}

	ctx := context.Background()
	if err := store.SaveJobs(ctx, jobs); err != nil {
		t.Fatalf("SaveJobs: %v", err)
	}

	loaded, err := store.LoadJobs(ctx)
	if err != nil {
		t.Fatalf("LoadJobs: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("loaded %d jobs, want 2", len(loaded))
	}
	if loaded[0].ID != "job1" || loaded[1].ID != "job2" {
		t.Errorf("unexpected job IDs: %v, %v", loaded[0].ID, loaded[1].ID)
	}
}

func TestFileJobStoreLoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	store, err := NewFileJobStore(path)
	if err != nil {
		t.Fatalf("NewFileJobStore: %v", err)
	}

	jobs, err := store.LoadJobs(context.Background())
	if err != nil {
		t.Fatalf("LoadJobs: %v", err)
	}
	if jobs != nil {
		t.Errorf("expected nil for nonexistent file, got %v", jobs)
	}
}

func TestSchedulerWithPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	store, err := NewFileJobStore(path)
	if err != nil {
		t.Fatalf("NewFileJobStore: %v", err)
	}

	// Create scheduler with persistence, add a job
	s1 := New(WithPersistence(store))
	if err := s1.AddJob(Job{ID: "persist-test", Schedule: "@every 1h", Goal: "test"}); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	// Verify file was written
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("jobs file should exist after AddJob")
	}

	// Create a new scheduler with the same store — it should recover the job on Start
	s2 := New(WithPersistence(store))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s2.Start(ctx, func(_ context.Context, _ Job) {})
	defer s2.Stop()

	jobs := s2.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 recovered job, got %d", len(jobs))
	}
	if jobs[0].ID != "persist-test" {
		t.Errorf("recovered job ID = %q, want %q", jobs[0].ID, "persist-test")
	}
}
