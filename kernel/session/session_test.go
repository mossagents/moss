package session

import (
	"context"
	mdl "github.com/mossagents/moss/kernel/model"
	"sync/atomic"
	"testing"
)

func TestBudgetExhausted(t *testing.T) {
	b := Budget{MaxTokens: 100, MaxSteps: 5}
	if b.Exhausted() {
		t.Fatal("should not be exhausted initially")
	}

	b.Record(50, 2)
	if b.Exhausted() {
		t.Fatal("should not be exhausted after partial use")
	}

	b.Record(50, 1)
	if !b.Exhausted() {
		t.Fatal("should be exhausted after reaching max tokens")
	}
}

func TestBudgetExhaustedBySteps(t *testing.T) {
	b := Budget{MaxSteps: 3}
	b.Record(0, 3)
	if !b.Exhausted() {
		t.Fatal("should be exhausted after reaching max steps")
	}
}

func TestBudgetTryConsumeAtomicBoundaries(t *testing.T) {
	b := Budget{MaxTokens: 10, MaxSteps: 3}
	if ok := b.TryConsume(5, 1); !ok {
		t.Fatal("expected first consume to succeed")
	}
	if ok := b.TryConsume(5, 1); !ok {
		t.Fatal("expected second consume to succeed at boundary")
	}
	if ok := b.TryConsume(1, 0); ok {
		t.Fatal("expected token over-consume to fail")
	}
	if ok := b.TryConsume(0, 1); !ok {
		t.Fatal("expected step consume to max boundary to succeed")
	}
	if ok := b.TryConsume(0, 1); ok {
		t.Fatal("expected step over-consume to fail")
	}
}

func TestSessionAppendAndTruncate(t *testing.T) {
	s := &Session{
		ID:       "test",
		Messages: make([]mdl.Message, 0),
	}

	for i := 0; i < 10; i++ {
		s.AppendMessage(mdl.Message{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("msg")}})
	}
	if len(s.Messages) != 10 {
		t.Fatalf("len = %d, want 10", len(s.Messages))
	}

	// 每条消息 1 token，最多保留 5 token
	s.TruncateMessages(5, func(m mdl.Message) int { return 1 })
	if len(s.Messages) != 5 {
		t.Fatalf("after truncate len = %d, want 5", len(s.Messages))
	}
}

func TestSessionState(t *testing.T) {
	s := &Session{ID: "test"}
	s.SetState("key", "value")
	v, ok := s.GetState("key")
	if !ok || v != "value" {
		t.Fatalf("GetState = %v, %v; want value, true", v, ok)
	}

	_, ok = s.GetState("missing")
	if ok {
		t.Fatal("expected not found for missing key")
	}
}

func TestManagerCreateAndGet(t *testing.T) {
	m := NewManager()
	s, err := m.Create(context.Background(), SessionConfig{Goal: "test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.Status != StatusCreated {
		t.Fatalf("Status = %q, want %q", s.Status, StatusCreated)
	}

	got, ok := m.Get(s.ID)
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.Config.Goal != "test" {
		t.Fatalf("Goal = %q, want %q", got.Config.Goal, "test")
	}
}

func TestManagerCancel(t *testing.T) {
	m := NewManager()
	s, _ := m.Create(context.Background(), SessionConfig{Goal: "test"})
	if err := m.Cancel(s.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got, _ := m.Get(s.ID)
	if got.Status != StatusCancelled {
		t.Fatalf("Status = %q, want %q", got.Status, StatusCancelled)
	}
}

func TestManagerList(t *testing.T) {
	m := NewManager()
	m.Create(context.Background(), SessionConfig{Goal: "a"})
	m.Create(context.Background(), SessionConfig{Goal: "b"})
	list := m.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
}

func TestManagerNotify(t *testing.T) {
	m := NewManager()
	s, _ := m.Create(context.Background(), SessionConfig{Goal: "test"})
	msg := mdl.Message{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hello")}}
	if err := m.Notify(s.ID, msg); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	got, _ := m.Get(s.ID)
	if len(got.Messages) != 1 || mdl.ContentPartsToPlainText(got.Messages[0].ContentParts) != "hello" {
		t.Fatalf("Messages = %v, want 1 message with 'hello'", got.Messages)
	}
}

type dummyManager struct {
	cancelCount int32
}

func (m *dummyManager) Create(_ context.Context, _ SessionConfig) (*Session, error) {
	return &Session{ID: "dummy"}, nil
}

func (m *dummyManager) Get(_ string) (*Session, bool) { return &Session{ID: "dummy"}, true }
func (m *dummyManager) List() []*Session              { return []*Session{} }
func (m *dummyManager) Notify(_ string, _ mdl.Message) error {
	return nil
}
func (m *dummyManager) Cancel(_ string) error {
	atomic.AddInt32(&m.cancelCount, 1)
	return nil
}

func TestWithCancelHook_WrapsNonAwareManager(t *testing.T) {
	base := &dummyManager{}
	var hooked int32
	m := WithCancelHook(base, func(string) { atomic.AddInt32(&hooked, 1) })

	if err := m.Cancel("sess_x"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got := atomic.LoadInt32(&base.cancelCount); got != 1 {
		t.Fatalf("base cancel count = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&hooked); got != 1 {
		t.Fatalf("hook called = %d, want 1", got)
	}
}

func TestWithCancelHook_UsesAwareManager(t *testing.T) {
	base := NewManager()
	sess, err := base.Create(context.Background(), SessionConfig{Goal: "x"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var hooked int32
	m := WithCancelHook(base, func(string) { atomic.AddInt32(&hooked, 1) })
	if m != base {
		t.Fatal("aware manager should be returned as-is")
	}

	if err := m.Cancel(sess.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got := atomic.LoadInt32(&hooked); got != 1 {
		t.Fatalf("hook called = %d, want 1", got)
	}
}
