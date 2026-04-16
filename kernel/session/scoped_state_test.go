package session

import (
	"encoding/json"
	"testing"
)

func TestParseScopeKey(t *testing.T) {
	tests := []struct {
		key       string
		wantScope StateScope
		wantKey   string
	}{
		{"foo", StateScopeSession, "foo"},
		{"app:foo", StateScopeApp, "foo"},
		{"user:bar", StateScopeUser, "bar"},
		{"temp:baz", StateScopeTemp, "baz"},
		{"unknown:key", StateScopeSession, "unknown:key"},
		{"app:", StateScopeApp, ""},
		{"", StateScopeSession, ""},
	}
	for _, tt := range tests {
		scope, key := ParseScopeKey(tt.key)
		if scope != tt.wantScope || key != tt.wantKey {
			t.Errorf("ParseScopeKey(%q) = (%q, %q), want (%q, %q)", tt.key, scope, key, tt.wantScope, tt.wantKey)
		}
	}
}

func TestSetState_PrefixRouting(t *testing.T) {
	s := &Session{}

	s.SetState("foo", "session-val")
	s.SetState("app:bar", "app-val")
	s.SetState("user:baz", "user-val")
	s.SetState("temp:qux", "temp-val")

	if v, ok := s.GetState("foo"); !ok || v != "session-val" {
		t.Fatalf("session scope: got %v, %v", v, ok)
	}
	if v, ok := s.GetState("app:bar"); !ok || v != "app-val" {
		t.Fatalf("app scope: got %v, %v", v, ok)
	}
	if v, ok := s.GetState("user:baz"); !ok || v != "user-val" {
		t.Fatalf("user scope: got %v, %v", v, ok)
	}
	if v, ok := s.GetState("temp:qux"); !ok || v != "temp-val" {
		t.Fatalf("temp scope: got %v, %v", v, ok)
	}
}

func TestSetScopedState_ExplicitAPI(t *testing.T) {
	s := &Session{}

	s.SetScopedState(StateScopeApp, "key", 42)
	v, ok := s.GetScopedState(StateScopeApp, "key")
	if !ok || v != 42 {
		t.Fatalf("GetScopedState(App, key) = %v, %v; want 42, true", v, ok)
	}

	s.DeleteScopedState(StateScopeApp, "key")
	_, ok = s.GetScopedState(StateScopeApp, "key")
	if ok {
		t.Fatal("key should be deleted")
	}
}

func TestCopyState_ReturnsSessionScopeOnly(t *testing.T) {
	s := &Session{}
	s.SetState("a", 1)
	s.SetState("app:b", 2)

	cp := s.CopyState()
	if len(cp) != 1 || cp["a"] != 1 {
		t.Fatalf("CopyState should return only session scope, got %v", cp)
	}
}

func TestCopyAllState_ReturnsAllScopes(t *testing.T) {
	s := &Session{}
	s.SetState("a", 1)
	s.SetState("app:b", 2)
	s.SetState("user:c", 3)
	s.SetState("temp:d", 4)

	all := s.CopyAllState()
	if all.Session["a"] != 1 || all.App["b"] != 2 || all.User["c"] != 3 || all.Temp["d"] != 4 {
		t.Fatalf("CopyAllState = %+v", all)
	}

	// Mutation isolation.
	all.Session["a"] = 999
	v, _ := s.GetState("a")
	if v == 999 {
		t.Fatal("CopyAllState should return independent copy")
	}
}

func TestClearTempState(t *testing.T) {
	s := &Session{}
	s.SetState("temp:x", "val")
	s.ClearTempState()
	if _, ok := s.GetState("temp:x"); ok {
		t.Fatal("temp state should be cleared")
	}
}

func TestClone_TempCleared(t *testing.T) {
	s := &Session{ID: "parent"}
	s.SetState("session_key", "v1")
	s.SetState("app:app_key", "v2")
	s.SetState("temp:tmp_key", "v3")

	clone := s.Clone()

	// Session and App should be copied.
	if v, ok := clone.GetState("session_key"); !ok || v != "v1" {
		t.Fatalf("clone session scope: %v, %v", v, ok)
	}
	if v, ok := clone.GetState("app:app_key"); !ok || v != "v2" {
		t.Fatalf("clone app scope: %v, %v", v, ok)
	}
	// Temp should be cleared in clone.
	if _, ok := clone.GetState("temp:tmp_key"); ok {
		t.Fatal("clone temp should be cleared")
	}

	// Mutation isolation.
	clone.SetState("session_key", "mutated")
	v, _ := s.GetState("session_key")
	if v == "mutated" {
		t.Fatal("clone should not affect parent")
	}
}

func TestScopedState_JSONRoundTrip(t *testing.T) {
	original := ScopedState{
		Session: map[string]any{"a": float64(1)},
		App:     map[string]any{"b": "two"},
		User:    map[string]any{"c": true},
		Temp:    map[string]any{"d": float64(4)},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ScopedState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Session["a"] != float64(1) || decoded.App["b"] != "two" ||
		decoded.User["c"] != true || decoded.Temp["d"] != float64(4) {
		t.Fatalf("round-trip mismatch: %+v", decoded)
	}
}

func TestScopedState_JSONLegacyMigration(t *testing.T) {
	// Old format: flat map.
	legacy := `{"foo": "bar", "count": 42}`

	var decoded ScopedState
	if err := json.Unmarshal([]byte(legacy), &decoded); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}

	if decoded.Session["foo"] != "bar" {
		t.Fatalf("legacy migration should put data in Session scope, got %+v", decoded)
	}
	if decoded.App != nil || decoded.User != nil || decoded.Temp != nil {
		t.Fatalf("legacy migration should leave other scopes nil, got %+v", decoded)
	}
}

func TestScopedState_JSONEmpty(t *testing.T) {
	var s ScopedState
	data, _ := json.Marshal(s)
	if string(data) != "null" {
		t.Fatalf("empty ScopedState should marshal to null, got %s", data)
	}

	if err := json.Unmarshal([]byte("null"), &s); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if !s.IsEmpty() {
		t.Fatalf("unmarshal null should be empty")
	}
}
