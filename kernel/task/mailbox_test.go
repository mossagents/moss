package task

import (
	"context"
	"testing"
)

func TestMemoryMailbox_SendRead(t *testing.T) {
	mb := NewMemoryMailbox()
	ctx := context.Background()

	id1, err := mb.Send(ctx, MailMessage{From: "a", To: "b", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if id1 == "" {
		t.Fatal("expected generated message id")
	}
	if _, err := mb.Send(ctx, MailMessage{ID: "fixed", From: "a", To: "b", Content: "world"}); err != nil {
		t.Fatal(err)
	}

	msgs, err := mb.Read(ctx, "b", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("unexpected first read: %+v", msgs)
	}

	msgs, err = mb.Read(ctx, "b", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ID != "fixed" {
		t.Fatalf("unexpected second read: %+v", msgs)
	}
}
