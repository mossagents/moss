package model

import (
	"encoding/json"
	"testing"
)

func TestRepairToolCallArguments_RepairsTruncatedJSON(t *testing.T) {
	got := RepairToolCallArguments(json.RawMessage(`{"path":"README.md"`))
	if !json.Valid(got) {
		t.Fatalf("expected valid json, got %s", got)
	}
	var decoded map[string]string
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal repaired args: %v", err)
	}
	if decoded["path"] != "README.md" {
		t.Fatalf("path = %q, want README.md", decoded["path"])
	}
}

func TestNormalizeToolCallArguments_QuotesPlainText(t *testing.T) {
	got := NormalizeToolCallArguments(json.RawMessage(`D:/Codes/qiulin/moss/apps/mosscode/main.go`))
	if !json.Valid(got) {
		t.Fatalf("expected valid json, got %s", got)
	}
	var decoded string
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal normalized args: %v", err)
	}
	if decoded != `D:/Codes/qiulin/moss/apps/mosscode/main.go` {
		t.Fatalf("decoded = %q", decoded)
	}
}

func TestRepairToolCallArguments_PreservesDanglingBackslash(t *testing.T) {
	got := RepairToolCallArguments(json.RawMessage(`{"msg":"test\`))
	if !json.Valid(got) {
		t.Fatalf("expected valid json, got %s", got)
	}
	var decoded map[string]string
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal repaired args: %v", err)
	}
	if decoded["msg"] != `test\` {
		t.Fatalf("msg = %q, want %q", decoded["msg"], `test\`)
	}
}

func TestNormalizeToolCalls_NilEmpty(t *testing.T) {
	if NormalizeToolCalls(nil) != nil {
		t.Fatal("nil input should return nil")
	}
	if NormalizeToolCalls([]ToolCall{}) != nil {
		t.Fatal("empty input should return nil")
	}
}

func TestNormalizeToolCalls_NormalizesArguments(t *testing.T) {
	calls := []ToolCall{
		{Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"`)},
		{Name: "no_args", Arguments: nil},
	}
	out := NormalizeToolCalls(calls)
	if len(out) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(out))
	}
	if !json.Valid(out[0].Arguments) {
		t.Fatalf("first call arguments should be valid JSON: %s", out[0].Arguments)
	}
	if out[0].Name != "read_file" {
		t.Fatalf("name should be preserved, got %q", out[0].Name)
	}
	// nil arguments should not be mutated
	if out[1].Arguments != nil {
		t.Fatalf("nil arguments should remain nil, got %s", out[1].Arguments)
	}
}

func TestNormalizeToolCalls_PreservesOtherFields(t *testing.T) {
	calls := []ToolCall{
		{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"cmd":"ls"}`)},
	}
	out := NormalizeToolCalls(calls)
	if out[0].ID != "call-1" || out[0].Name != "bash" {
		t.Fatalf("ID and Name should be preserved: %+v", out[0])
	}
}
