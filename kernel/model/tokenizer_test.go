package model

import (
	"testing"
)

func TestSimpleTokenizer_CountString(t *testing.T) {
	tok := SimpleTokenizer{}

	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 1},           // < 4 chars → 1
		{"abcd", 1},        // exactly 4 chars → 1
		{"abcde", 1},       // 5 chars → 1
		{"12345678", 2},    // 8 chars → 2
		{"hello world", 2}, // 11 chars → 2
	}
	for _, tt := range tests {
		got := tok.CountString(tt.input)
		if got != tt.want {
			t.Errorf("CountString(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestSimpleTokenizer_CountMessage_Empty(t *testing.T) {
	tok := SimpleTokenizer{}
	got := tok.CountMessage(Message{Role: RoleUser})
	if got != 0 {
		t.Errorf("empty message should have 0 tokens, got %d", got)
	}
}

func TestSimpleTokenizer_CountMessage_Text(t *testing.T) {
	tok := SimpleTokenizer{}
	msg := Message{
		Role:         RoleUser,
		ContentParts: []ContentPart{TextPart("hello world!")}, // 12 chars → 3
	}
	got := tok.CountMessage(msg)
	if got != 3 {
		t.Errorf("expected 3, got %d", got)
	}
}

func TestSimpleTokenizer_CountMessage_MinOne(t *testing.T) {
	tok := SimpleTokenizer{}
	// single char → len/4 = 0, but should return 1
	msg := Message{
		Role:         RoleUser,
		ContentParts: []ContentPart{TextPart("x")},
	}
	got := tok.CountMessage(msg)
	if got < 1 {
		t.Errorf("non-empty message should return at least 1 token, got %d", got)
	}
}

func TestSimpleTokenizer_CountMessages(t *testing.T) {
	tok := SimpleTokenizer{}
	msgs := []Message{
		{Role: RoleSystem, ContentParts: []ContentPart{TextPart("system")}},    // 6 chars → 1
		{Role: RoleUser, ContentParts: []ContentPart{TextPart("hello world")}}, // 11 chars → 2
	}
	got := tok.CountMessages(msgs)
	if got < 2 {
		t.Errorf("CountMessages expected >= 2, got %d", got)
	}
}

func TestFuncTokenizer_Nil(t *testing.T) {
	tok := FuncTokenizer{Fn: nil}
	// should fall back to SimpleTokenizer
	msg := Message{Role: RoleUser, ContentParts: []ContentPart{TextPart("test")}}
	got := tok.CountMessage(msg)
	want := (SimpleTokenizer{}).CountMessage(msg)
	if got != want {
		t.Errorf("nil FuncTokenizer should fall back to SimpleTokenizer: got %d, want %d", got, want)
	}
}

func TestFuncTokenizer_Custom(t *testing.T) {
	tok := FuncTokenizer{Fn: func(msg Message) int { return 42 }}
	msg := Message{Role: RoleUser, ContentParts: []ContentPart{TextPart("anything")}}
	if got := tok.CountMessage(msg); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

func TestFuncTokenizer_CountString(t *testing.T) {
	tok := FuncTokenizer{Fn: func(msg Message) int {
		return len(ContentPartsToPlainText(msg.ContentParts))
	}}
	got := tok.CountString("hello")
	if got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}

func TestHashMessages_Deterministic(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, ContentParts: []ContentPart{TextPart("hello")}},
		{Role: RoleAssistant, ContentParts: []ContentPart{TextPart("world")}},
	}
	h1 := HashMessages(msgs)
	h2 := HashMessages(msgs)
	if h1 != h2 {
		t.Error("HashMessages should be deterministic")
	}
}

func TestHashMessages_Distinct(t *testing.T) {
	msgs1 := []Message{{Role: RoleUser, ContentParts: []ContentPart{TextPart("hello")}}}
	msgs2 := []Message{{Role: RoleUser, ContentParts: []ContentPart{TextPart("world")}}}
	if HashMessages(msgs1) == HashMessages(msgs2) {
		t.Error("different messages should produce different hashes")
	}
}

func TestHashMessages_Empty(t *testing.T) {
	// should not panic
	h := HashMessages(nil)
	_ = h
}
