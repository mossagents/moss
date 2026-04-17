package sandbox

import (
	"strings"
	"testing"
)

func TestLimitedWriter_WithinLimit(t *testing.T) {
	var buf strings.Builder
	lw := newLimitedWriter(&buf, 100)
	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Fatalf("n = %d, want 5", n)
	}
	if buf.String() != "hello" {
		t.Fatalf("buf = %q, want hello", buf.String())
	}
	if lw.Truncated() {
		t.Fatal("should not be truncated")
	}
}

func TestLimitedWriter_ExceedsLimit(t *testing.T) {
	var buf strings.Builder
	lw := newLimitedWriter(&buf, 5)
	n, err := lw.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Reports all bytes as written to avoid short write errors.
	if n != 11 {
		t.Fatalf("n = %d, want 11", n)
	}
	if buf.String() != "hello" {
		t.Fatalf("buf = %q, want hello", buf.String())
	}
	if !lw.Truncated() {
		t.Fatal("should be truncated")
	}
}

func TestLimitedWriter_MultipleWritesTruncates(t *testing.T) {
	var buf strings.Builder
	lw := newLimitedWriter(&buf, 8)
	lw.Write([]byte("hello"))
	lw.Write([]byte(" world"))
	if buf.String() != "hello wo" {
		t.Fatalf("buf = %q, want 'hello wo'", buf.String())
	}
	if !lw.Truncated() {
		t.Fatal("should be truncated")
	}
	if lw.Written() != 8 {
		t.Fatalf("Written = %d, want 8", lw.Written())
	}
}

func TestLimitedWriter_ZeroLimit(t *testing.T) {
	var buf strings.Builder
	lw := newLimitedWriter(&buf, 0)
	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Fatalf("n = %d, want 5", n)
	}
	if buf.String() != "" {
		t.Fatalf("buf should be empty, got %q", buf.String())
	}
	if !lw.Truncated() {
		t.Fatal("should be truncated at zero limit")
	}
}
