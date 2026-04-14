package builtins

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/tool"
)

// ── PolicyDeniedError ────────────────────────────────────────────────────────

func TestPolicyDeniedError_Error(t *testing.T) {
	e := &PolicyDeniedError{ToolName: "bash", ReasonCode: "code", Reason: "nope"}
	if e.Error() != ErrDenied.Error() {
		t.Errorf("Error() = %q, want %q", e.Error(), ErrDenied.Error())
	}
	if !errors.Is(e, ErrDenied) {
		t.Error("expected errors.Is(e, ErrDenied)")
	}
}

func TestPolicyDeniedError_AsKernelError(t *testing.T) {
	e := &PolicyDeniedError{
		ToolName:    "run_cmd",
		ReasonCode:  "code-1",
		Reason:      "reason text",
		Enforcement: EnforcementHardBlock,
	}
	ke := e.AsKernelError()
	if ke == nil {
		t.Fatal("expected non-nil kernel error")
	}
	if ke.Meta["tool"] != "run_cmd" {
		t.Errorf("expected tool=run_cmd, got %v", ke.Meta["tool"])
	}
	if ke.Meta["reason_code"] != "code-1" {
		t.Errorf("expected reason_code=code-1, got %v", ke.Meta["reason_code"])
	}
}

// ── copyPolicyMeta ───────────────────────────────────────────────────────────

func TestCopyPolicyMeta(t *testing.T) {
	orig := map[string]any{"a": 1, "b": "hello"}
	copied := copyPolicyMeta(orig)
	if len(copied) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(copied))
	}
	// mutation of copy must not affect original
	copied["c"] = "extra"
	if _, ok := orig["c"]; ok {
		t.Error("copyPolicyMeta should not share reference")
	}
}

// ── effectsToStrings ─────────────────────────────────────────────────────────

func TestEffectsToStrings(t *testing.T) {
	effects := []tool.Effect{"read", "", "write"}
	got := effectsToStrings(effects)
	if len(got) != 2 || got[0] != "read" || got[1] != "write" {
		t.Errorf("expected [read write], got %v", got)
	}
}

func TestEffectsToStrings_Empty(t *testing.T) {
	if got := effectsToStrings(nil); len(got) != 0 {
		t.Errorf("nil input → empty slice, got %v", got)
	}
}

// ── policyToolSemantics ──────────────────────────────────────────────────────

func TestPolicyToolSemantics_Nil(t *testing.T) {
	if got := policyToolSemantics(nil); got != nil {
		t.Errorf("nil spec should return nil, got %v", got)
	}
}

func TestPolicyToolSemantics_Spec(t *testing.T) {
	spec := &tool.ToolSpec{
		Name:     "bash",
		Effects:  []tool.Effect{"execute"},
		Idempotent: false,
	}
	got := policyToolSemantics(spec)
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if _, ok := got["side_effect_class"]; !ok {
		t.Error("expected side_effect_class key")
	}
}

// ── quoteApprovalToken ───────────────────────────────────────────────────────

func TestQuoteApprovalToken(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"git", "git"},
		{"push origin", `"push origin"`},
		{"  trim  ", "trim"},
		{`say "hello"`, `"say \"hello\""`},
	}
	for _, tc := range cases {
		got := quoteApprovalToken(tc.in)
		if got != tc.want {
			t.Errorf("quoteApprovalToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── sanitizeApprovalName ─────────────────────────────────────────────────────

func TestSanitizeApprovalName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "rule"},
		{"Git Push", "git-push"},
		{"git__push", "git-push"},
		{"  my.rule  ", "my-rule"},
		{"path/to/file", "path-to-file"},
		{"RULE:1", "rule-1"},
	}
	for _, tc := range cases {
		got := sanitizeApprovalName(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeApprovalName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── parseApprovalCommand ─────────────────────────────────────────────────────

func TestParseApprovalCommand(t *testing.T) {
	cases := []struct {
		input        []byte
		wantDisplay  string
		wantPattern  string
	}{
		{nil, "", ""},
		{[]byte(`{}`), "", ""},
		{[]byte(`{"command":"git","args":["push","origin"]}`), "git push origin", "git push"},
		{[]byte(`{"command":"git","args":["  "]}`), "git", "git"},
		{[]byte(`{"command":"my cmd","args":["arg one"]}`), `"my cmd" "arg one"`, "my cmd arg one"},
		{[]byte(`invalid`), "", ""},
	}
	for _, tc := range cases {
		gotDisplay, gotPattern := parseApprovalCommand(tc.input)
		if gotDisplay != tc.wantDisplay {
			t.Errorf("parseApprovalCommand(%s) display = %q, want %q", tc.input, gotDisplay, tc.wantDisplay)
		}
		if gotPattern != tc.wantPattern {
			t.Errorf("parseApprovalCommand(%s) pattern = %q, want %q", tc.input, gotPattern, tc.wantPattern)
		}
	}
}

// ── parseApprovalRequestTarget ───────────────────────────────────────────────

func TestParseApprovalRequestTarget(t *testing.T) {
	// empty input
	line, host, method := parseApprovalRequestTarget(nil)
	if line != "" || host != "" || method != "" {
		t.Errorf("nil input: %q %q %q", line, host, method)
	}

	// valid URL
	input, _ := json.Marshal(map[string]string{"url": "https://api.example.com/v1", "method": "POST"})
	line, host, method = parseApprovalRequestTarget(input)
	if host != "api.example.com" {
		t.Errorf("host = %q, want api.example.com", host)
	}
	if method != "POST" {
		t.Errorf("method = %q, want POST", method)
	}
	if line != "POST https://api.example.com/v1" {
		t.Errorf("line = %q", line)
	}

	// no method → defaults to GET
	input2, _ := json.Marshal(map[string]string{"url": "https://example.org"})
	_, _, method2 := parseApprovalRequestTarget(input2)
	if method2 != "GET" {
		t.Errorf("no method → GET, got %q", method2)
	}

	// empty URL → all empty
	input3, _ := json.Marshal(map[string]string{"url": "", "method": "GET"})
	l, h, _ := parseApprovalRequestTarget(input3)
	if l != "" || h != "" {
		t.Errorf("empty URL should return empty, got %q %q", l, h)
	}
}

// ── parseApprovalGenericPreview ──────────────────────────────────────────────

func TestParseApprovalGenericPreview(t *testing.T) {
	if got := parseApprovalGenericPreview(nil); got != "" {
		t.Errorf("nil → empty, got %q", got)
	}
	if got := parseApprovalGenericPreview([]byte("invalid")); got != "" {
		t.Errorf("invalid → empty, got %q", got)
	}
	// normal JSON
	input := []byte(`{"key":"value"}`)
	got := parseApprovalGenericPreview(input)
	if got == "" {
		t.Error("expected non-empty preview for valid JSON")
	}
	// long JSON should be truncated
	longInput, _ := json.Marshal(map[string]string{"data": string(make([]byte, 500))})
	gotLong := parseApprovalGenericPreview(longInput)
	if len(gotLong) > 225 {
		t.Errorf("expected truncated preview, got len=%d", len(gotLong))
	}
}

// ── SummarizeConfig helpers ──────────────────────────────────────────────────

func TestSummarizeConfig_Defaults(t *testing.T) {
	cfg := SummarizeConfig{}
	if cfg.maxContextTokens() != 80000 {
		t.Errorf("maxContextTokens default = %d", cfg.maxContextTokens())
	}
	if cfg.keepRecent() != 20 {
		t.Errorf("keepRecent default = %d", cfg.keepRecent())
	}
	if cfg.maxSummaryTokens() != 800 {
		t.Errorf("maxSummaryTokens default = %d", cfg.maxSummaryTokens())
	}
	if cfg.summaryPrompt() == "" {
		t.Error("summaryPrompt default should be non-empty")
	}
	if !cfg.enabled() {
		t.Error("enabled default should be true (nil Enabled field)")
	}
}

func TestSummarizeConfig_Custom(t *testing.T) {
	f := false
	cfg := SummarizeConfig{
		MaxContextTokens: 1000,
		KeepRecent:       5,
		MaxSummaryTokens: 100,
		SummaryPrompt:    "custom",
		Enabled:          &f,
	}
	if cfg.maxContextTokens() != 1000 {
		t.Errorf("got %d", cfg.maxContextTokens())
	}
	if cfg.keepRecent() != 5 {
		t.Errorf("got %d", cfg.keepRecent())
	}
	if cfg.maxSummaryTokens() != 100 {
		t.Errorf("got %d", cfg.maxSummaryTokens())
	}
	if cfg.summaryPrompt() != "custom" {
		t.Errorf("got %q", cfg.summaryPrompt())
	}
	if cfg.enabled() {
		t.Error("Enabled=false should return false")
	}
}

func TestSummarizeConfig_CountTokens_Counter(t *testing.T) {
	cfg := SummarizeConfig{
		TokenCounter: func(msg model.Message) int { return 42 },
	}
	got := cfg.countTokens(model.Message{Role: "user"})
	if got != 42 {
		t.Errorf("countTokens = %d, want 42", got)
	}
}

// ── RAGConfig helpers ────────────────────────────────────────────────────────

func TestRAGConfig_MaxChars_Default(t *testing.T) {
	cfg := RAGConfig{}
	if cfg.maxChars() != 4000 {
		t.Errorf("maxChars default = %d", cfg.maxChars())
	}
}

func TestRAGConfig_MaxChars_Custom(t *testing.T) {
	cfg := RAGConfig{MaxChars: 1500}
	if cfg.maxChars() != 1500 {
		t.Errorf("maxChars = %d", cfg.maxChars())
	}
}
