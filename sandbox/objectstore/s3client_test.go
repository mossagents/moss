package objectstore

import (
	"net/url"
	"testing"
)

// ─── objectURL ───────────────────────────────────────────────────────────────

func TestObjectURL_VirtualHostStyle(t *testing.T) {
	c := &S3BlobClient{cfg: S3Config{
		Endpoint: "https://s3.us-east-1.amazonaws.com",
		Bucket:   "my-bucket",
		Region:   "us-east-1",
	}}
	got := c.objectURL("path/to/key")
	want := "https://my-bucket.s3.us-east-1.amazonaws.com/path/to/key"
	if got != want {
		t.Fatalf("objectURL virtual = %q, want %q", got, want)
	}
}

func TestObjectURL_PathStyle(t *testing.T) {
	c := &S3BlobClient{cfg: S3Config{
		Endpoint:  "http://localhost:9000",
		Bucket:    "test-bucket",
		PathStyle: true,
	}}
	got := c.objectURL("some/key")
	want := "http://localhost:9000/test-bucket/some/key"
	if got != want {
		t.Fatalf("objectURL path-style = %q, want %q", got, want)
	}
}

// ─── fullKey ─────────────────────────────────────────────────────────────────

func TestFullKey_NoPrefix(t *testing.T) {
	c := &S3BlobClient{cfg: S3Config{}}
	if c.fullKey("hello.txt") != "hello.txt" {
		t.Fatalf("expected passthrough, got %q", c.fullKey("hello.txt"))
	}
}

func TestFullKey_WithPrefix(t *testing.T) {
	c := &S3BlobClient{cfg: S3Config{RootPrefix: "workspaces/sess-1"}}
	got := c.fullKey("/data.bin")
	want := "workspaces/sess-1/data.bin"
	if got != want {
		t.Fatalf("fullKey = %q, want %q", got, want)
	}
}

// ─── canonicalQueryString ────────────────────────────────────────────────────

func TestCanonicalQueryString_Empty(t *testing.T) {
	if got := canonicalQueryString(url.Values{}); got != "" {
		t.Fatalf("empty query should be empty, got %q", got)
	}
}

func TestCanonicalQueryString_Sorted(t *testing.T) {
	q := url.Values{"z": {"1"}, "a": {"2"}, "m": {"3"}}
	got := canonicalQueryString(q)
	// Should be sorted alphabetically
	if got != "a=2&m=3&z=1" {
		t.Fatalf("canonical query = %q, want a=2&m=3&z=1", got)
	}
}

// ─── sha256Hex / hmacSHA256 ──────────────────────────────────────────────────

func TestSha256Hex_EmptyInput(t *testing.T) {
	// SHA-256 of empty byte slice is a well-known value
	got := sha256Hex([]byte{})
	const expected = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != expected {
		t.Fatalf("sha256('') = %q, want %q", got, expected)
	}
}

func TestSha256Hex_Deterministic(t *testing.T) {
	a := sha256Hex([]byte("hello"))
	b := sha256Hex([]byte("hello"))
	if a != b {
		t.Fatal("sha256Hex should be deterministic")
	}
}

func TestHmacSHA256_Produces32Bytes(t *testing.T) {
	result := hmacSHA256([]byte("key"), []byte("data"))
	if len(result) != 32 {
		t.Fatalf("hmacSHA256 should produce 32 bytes, got %d", len(result))
	}
}

// ─── NewS3BlobClient ─────────────────────────────────────────────────────────

func TestNewS3BlobClient_NoBucket(t *testing.T) {
	_, err := NewS3BlobClient(S3Config{}, nil)
	if err == nil {
		t.Fatal("expected error when bucket is empty")
	}
}

func TestNewS3BlobClient_DefaultRegionAndEndpoint(t *testing.T) {
	c, err := NewS3BlobClient(S3Config{Bucket: "my-bucket"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.cfg.Region != "us-east-1" {
		t.Fatalf("default region should be us-east-1, got %q", c.cfg.Region)
	}
	if c.cfg.Endpoint == "" {
		t.Fatal("endpoint should be auto-derived")
	}
}

func TestNewS3BlobClient_CustomHTTPClient(t *testing.T) {
	c, err := NewS3BlobClient(S3Config{Bucket: "x", Region: "eu-west-1"}, nil)
	if err != nil {
		t.Fatalf("NewS3BlobClient: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.cfg.Region != "eu-west-1" {
		t.Fatalf("region = %q, want eu-west-1", c.cfg.Region)
	}
}
