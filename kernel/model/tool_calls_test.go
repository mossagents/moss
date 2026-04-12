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
