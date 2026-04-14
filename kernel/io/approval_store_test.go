package io

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryApprovalStore_SaveAndGet(t *testing.T) {
	store := NewMemoryApprovalStore()
	ctx := context.Background()

	record := ApprovalRecord{
		Request:   ApprovalRequest{ID: "req-1", SessionID: "s1"},
		Status:    ApprovalStatusPending,
		CreatedAt: time.Now(),
	}
	if err := store.Save(ctx, record); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := store.Get(ctx, "req-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Request.ID != "req-1" || got.Status != ApprovalStatusPending {
		t.Fatalf("unexpected record: %+v", got)
	}
}

func TestMemoryApprovalStore_SaveRequiresID(t *testing.T) {
	store := NewMemoryApprovalStore()
	err := store.Save(context.Background(), ApprovalRecord{})
	if err == nil {
		t.Fatal("expected error for missing request ID")
	}
}

func TestMemoryApprovalStore_GetNotFound(t *testing.T) {
	store := NewMemoryApprovalStore()
	_, err := store.Get(context.Background(), "nonexistent")
	if !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("expected ErrApprovalNotFound, got %v", err)
	}
}

func TestMemoryApprovalStore_SaveOverwrites(t *testing.T) {
	store := NewMemoryApprovalStore()
	ctx := context.Background()
	base := ApprovalRecord{Request: ApprovalRequest{ID: "req-2"}, Status: ApprovalStatusPending, CreatedAt: time.Now()}
	_ = store.Save(ctx, base)

	base.Status = ApprovalStatusApproved
	_ = store.Save(ctx, base)

	got, _ := store.Get(ctx, "req-2")
	if got.Status != ApprovalStatusApproved {
		t.Fatalf("expected overwrite, got %s", got.Status)
	}
}

func TestMemoryApprovalStore_ListBySession(t *testing.T) {
	store := NewMemoryApprovalStore()
	ctx := context.Background()
	now := time.Now()

	records := []ApprovalRecord{
		{Request: ApprovalRequest{ID: "r1", SessionID: "sess-A"}, Status: ApprovalStatusPending, CreatedAt: now.Add(-2 * time.Second)},
		{Request: ApprovalRequest{ID: "r2", SessionID: "sess-A"}, Status: ApprovalStatusApproved, CreatedAt: now.Add(-1 * time.Second)},
		{Request: ApprovalRequest{ID: "r3", SessionID: "sess-B"}, Status: ApprovalStatusDenied, CreatedAt: now},
	}
	for _, r := range records {
		_ = store.Save(ctx, r)
	}

	list, err := store.List(ctx, "sess-A")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 records for sess-A, got %d", len(list))
	}
	if list[0].Request.ID != "r1" || list[1].Request.ID != "r2" {
		t.Fatalf("unexpected order: %v %v", list[0].Request.ID, list[1].Request.ID)
	}
}

func TestMemoryApprovalStore_ListAllWithEmptySession(t *testing.T) {
	store := NewMemoryApprovalStore()
	ctx := context.Background()
	_ = store.Save(ctx, ApprovalRecord{Request: ApprovalRequest{ID: "x1", SessionID: "s1"}, Status: ApprovalStatusPending, CreatedAt: time.Now()})
	_ = store.Save(ctx, ApprovalRecord{Request: ApprovalRequest{ID: "x2", SessionID: "s2"}, Status: ApprovalStatusPending, CreatedAt: time.Now()})

	list, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 total records, got %d", len(list))
	}
}
