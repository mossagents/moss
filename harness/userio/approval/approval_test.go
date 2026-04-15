package approval_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	configpkg "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/harness/userio/approval"
)

// ── BuildDisplay ──────────────────────────────────────────────────────────────

func TestBuildDisplay_NilRequest(t *testing.T) {
	d := approval.BuildDisplay(nil, "sess1")
	if d.Title == "" {
		t.Error("expected non-empty Title")
	}
	if d.Reason == "" {
		t.Error("expected non-empty Reason")
	}
}

func TestBuildDisplay_WithActionLabelAndValue(t *testing.T) {
	req := &io.ApprovalRequest{
		ToolName:    "my_tool",
		ActionLabel: "Label",
		ActionValue: "Value",
	}
	d := approval.BuildDisplay(req, "sess")
	if d.ActionLabel != "Label" {
		t.Errorf("expected ActionLabel=Label, got %q", d.ActionLabel)
	}
	if d.ActionValue != "Value" {
		t.Errorf("expected ActionValue=Value, got %q", d.ActionValue)
	}
}

func TestBuildDisplay_ScopeLabelDefaultsToMatchingRule(t *testing.T) {
	req := &io.ApprovalRequest{
		ActionLabel: "Action",
		ActionValue: "do something",
		CacheKey:    "tool|foo",
	}
	d := approval.BuildDisplay(req, "sess")
	if d.ScopeLabel != "Matching rule" {
		t.Errorf("expected ScopeLabel='Matching rule', got %q", d.ScopeLabel)
	}
}

func TestBuildDisplay_RuleLabelDefaultsToScopeValue(t *testing.T) {
	req := &io.ApprovalRequest{
		ActionLabel: "Action",
		ActionValue: "do something",
		ScopeValue:  "my_scope",
	}
	d := approval.BuildDisplay(req, "sess")
	if d.RuleLabel != "my_scope" {
		t.Errorf("expected RuleLabel=my_scope, got %q", d.RuleLabel)
	}
}

func TestBuildDisplay_RunCommand(t *testing.T) {
	input, _ := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"commit", "-m", "msg"},
	})
	req := &io.ApprovalRequest{
		ToolName: "run_command",
		Input:    input,
	}
	d := approval.BuildDisplay(req, "sess")
	if d.ActionLabel != "Command" {
		t.Errorf("expected ActionLabel=Command, got %q", d.ActionLabel)
	}
	if !strings.Contains(d.ActionValue, "git") {
		t.Errorf("expected git in ActionValue, got %q", d.ActionValue)
	}
	if d.ScopeLabel != "Matching rule" {
		t.Errorf("expected ScopeLabel=Matching rule, got %q", d.ScopeLabel)
	}
}

func TestBuildDisplay_HTTPRequest(t *testing.T) {
	input, _ := json.Marshal(map[string]any{
		"url":    "https://api.example.com/v1/data",
		"method": "POST",
	})
	req := &io.ApprovalRequest{
		ToolName: "http_request",
		Input:    input,
	}
	d := approval.BuildDisplay(req, "sess")
	if d.ActionLabel != "Request" {
		t.Errorf("expected ActionLabel=Request, got %q", d.ActionLabel)
	}
	if !strings.Contains(d.ActionValue, "POST") {
		t.Errorf("expected POST in ActionValue, got %q", d.ActionValue)
	}
	if !strings.Contains(d.RuleKey, "http_request") {
		t.Errorf("expected http_request in RuleKey, got %q", d.RuleKey)
	}
}

func TestBuildDisplay_OtherTool_WithInput(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"path": "/some/file.txt"})
	req := &io.ApprovalRequest{
		ToolName: "write_file",
		Input:    input,
	}
	d := approval.BuildDisplay(req, "sess")
	if d.ActionLabel != "Action" {
		t.Errorf("expected ActionLabel=Action, got %q", d.ActionLabel)
	}
	if d.RuleKey == "" {
		t.Error("expected non-empty RuleKey for named tool")
	}
}

func TestBuildDisplay_OtherTool_NoInput(t *testing.T) {
	req := &io.ApprovalRequest{
		ToolName: "custom_tool",
		Prompt:   "Do something",
	}
	d := approval.BuildDisplay(req, "sess")
	if d.ToolName != "custom_tool" {
		t.Errorf("expected ToolName=custom_tool, got %q", d.ToolName)
	}
}

func TestBuildDisplay_EmptyReason_FallsBack(t *testing.T) {
	req := &io.ApprovalRequest{ToolName: "tool", Reason: ""}
	d := approval.BuildDisplay(req, "sess")
	if d.Reason == "" {
		t.Error("expected default reason when request reason is empty")
	}
}

func TestBuildDisplay_DiffPreview_WriteFile(t *testing.T) {
	input, _ := json.Marshal(map[string]any{
		"path":    "/some/file.go",
		"content": "line1\nline2\nline3",
	})
	req := &io.ApprovalRequest{
		ToolName: "write_file",
		Input:    input,
	}
	d := approval.BuildDisplay(req, "sess")
	if !strings.Contains(d.DiffPreview, "+") {
		t.Errorf("expected diff preview with + prefix, got %q", d.DiffPreview)
	}
}

func TestBuildDisplay_DiffPreview_EditFile(t *testing.T) {
	input, _ := json.Marshal(map[string]any{
		"path":       "/some/file.go",
		"old_string": "old line",
		"new_string": "new line",
	})
	req := &io.ApprovalRequest{
		ToolName: "edit_file",
		Input:    input,
	}
	d := approval.BuildDisplay(req, "sess")
	if !strings.Contains(d.DiffPreview, "- old line") {
		t.Errorf("expected '- old line' in diff preview, got %q", d.DiffPreview)
	}
	if !strings.Contains(d.DiffPreview, "+ new line") {
		t.Errorf("expected '+ new line' in diff preview, got %q", d.DiffPreview)
	}
}

func TestBuildDisplay_DiffPreview_EditFile_Truncation(t *testing.T) {
	// Create many lines to trigger truncation
	oldLines := strings.Repeat("old line\n", 50)
	input, _ := json.Marshal(map[string]any{
		"path":       "/big.go",
		"old_string": oldLines,
		"new_string": "new",
	})
	req := &io.ApprovalRequest{
		ToolName: "edit_file",
		Input:    input,
	}
	d := approval.BuildDisplay(req, "sess")
	if !strings.Contains(d.DiffPreview, "truncated") {
		t.Errorf("expected 'truncated' in long diff preview, got: %q", d.DiffPreview[:100])
	}
}

func TestBuildDisplay_DiffPreview_WriteFile_ManyLines(t *testing.T) {
	// > 30 lines should show truncation marker
	lines := strings.Repeat("line\n", 35)
	input, _ := json.Marshal(map[string]any{
		"path":    "/big.go",
		"content": lines,
	})
	req := &io.ApprovalRequest{
		ToolName: "write_file",
		Input:    input,
	}
	d := approval.BuildDisplay(req, "sess")
	if !strings.Contains(d.DiffPreview, "more lines") {
		t.Errorf("expected 'more lines' in long write preview, got: %q", d.DiffPreview)
	}
}

// ── ParseCommand ──────────────────────────────────────────────────────────────

func TestParseCommand_Nil(t *testing.T) {
	cmd, pattern := approval.ParseCommand(nil)
	if cmd != "" || pattern != "" {
		t.Errorf("expected empty from nil request, got %q %q", cmd, pattern)
	}
}

func TestParseCommand_NoInput(t *testing.T) {
	req := &io.ApprovalRequest{ToolName: "run_command"}
	cmd, pattern := approval.ParseCommand(req)
	if cmd != "" || pattern != "" {
		t.Errorf("expected empty from request without input, got %q %q", cmd, pattern)
	}
}

func TestParseCommand_Simple(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "ls", "args": []string{"-la"}})
	req := &io.ApprovalRequest{Input: input}
	cmd, pattern := approval.ParseCommand(req)
	if !strings.Contains(cmd, "ls") {
		t.Errorf("expected cmd to contain 'ls', got %q", cmd)
	}
	if !strings.Contains(pattern, "ls") {
		t.Errorf("expected pattern to contain 'ls', got %q", pattern)
	}
}

func TestParseCommand_WithSpaces(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "git", "args": []string{"commit", "-m", "my message"}})
	req := &io.ApprovalRequest{Input: input}
	cmd, _ := approval.ParseCommand(req)
	// "my message" should be quoted since it contains a space
	if !strings.Contains(cmd, `"my message"`) {
		t.Errorf("expected quoted arg in cmd, got %q", cmd)
	}
}

func TestParseCommand_EmptyCommand(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "", "args": []string{}})
	req := &io.ApprovalRequest{Input: input}
	cmd, pattern := approval.ParseCommand(req)
	if cmd != "" || pattern != "" {
		t.Errorf("expected empty for blank command, got %q %q", cmd, pattern)
	}
}

func TestParseCommand_NoArgs(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "ls"})
	req := &io.ApprovalRequest{Input: input}
	cmd, pattern := approval.ParseCommand(req)
	if cmd != "ls" {
		t.Errorf("expected 'ls', got %q", cmd)
	}
	_ = pattern
}

// ── ParseRequestTarget ────────────────────────────────────────────────────────

func TestParseRequestTarget_Nil(t *testing.T) {
	line, pattern := approval.ParseRequestTarget(nil)
	if line != "" || pattern != "" {
		t.Errorf("expected empty from nil, got %q %q", line, pattern)
	}
}

func TestParseRequestTarget_NoInput(t *testing.T) {
	req := &io.ApprovalRequest{}
	line, pattern := approval.ParseRequestTarget(req)
	if line != "" || pattern != "" {
		t.Errorf("expected empty from no-input, got %q %q", line, pattern)
	}
}

func TestParseRequestTarget_EmptyURL(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"url": "", "method": "GET"})
	req := &io.ApprovalRequest{Input: input}
	line, pattern := approval.ParseRequestTarget(req)
	if line != "" || pattern != "" {
		t.Errorf("expected empty for empty URL, got %q %q", line, pattern)
	}
}

func TestParseRequestTarget_WithMethod(t *testing.T) {
	input, _ := json.Marshal(map[string]any{
		"url":    "https://api.example.com/v1",
		"method": "POST",
	})
	req := &io.ApprovalRequest{Input: input}
	line, pattern := approval.ParseRequestTarget(req)
	if !strings.HasPrefix(line, "POST ") {
		t.Errorf("expected line starting with 'POST ', got %q", line)
	}
	if !strings.Contains(pattern, "api.example.com") {
		t.Errorf("expected domain in pattern, got %q", pattern)
	}
}

func TestParseRequestTarget_DefaultMethodGET(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"url": "https://example.com/"})
	req := &io.ApprovalRequest{Input: input}
	line, _ := approval.ParseRequestTarget(req)
	if !strings.HasPrefix(line, "GET ") {
		t.Errorf("expected default GET method, got %q", line)
	}
}

func TestParseRequestTarget_InvalidURL(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"url": "not-a-url", "method": "GET"})
	req := &io.ApprovalRequest{Input: input}
	line, pattern := approval.ParseRequestTarget(req)
	if line == "" {
		t.Error("expected non-empty line for invalid URL")
	}
	// Pattern should fall back to just the method
	if pattern != "GET" {
		t.Errorf("expected pattern=GET for invalid URL, got %q", pattern)
	}
}

// ── SessionID ─────────────────────────────────────────────────────────────────

func TestSessionID_FromRequest(t *testing.T) {
	req := &io.ApprovalRequest{SessionID: "req-session"}
	id := approval.SessionID(req, "fallback")
	if id != "req-session" {
		t.Errorf("expected req-session, got %q", id)
	}
}

func TestSessionID_FallsBackToCurrentSession(t *testing.T) {
	req := &io.ApprovalRequest{SessionID: ""}
	id := approval.SessionID(req, "fallback-session")
	if id != "fallback-session" {
		t.Errorf("expected fallback-session, got %q", id)
	}
}

func TestSessionID_NilRequest(t *testing.T) {
	id := approval.SessionID(nil, "current")
	if id != "current" {
		t.Errorf("expected current, got %q", id)
	}
}

// ── MemoryRuleFor ─────────────────────────────────────────────────────────────

func TestMemoryRuleFor_RunCommand(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "git", "args": []string{"status"}})
	req := &io.ApprovalRequest{
		ToolName:  "run_command",
		SessionID: "sess1",
		Input:     input,
	}
	rule, ok := approval.MemoryRuleFor(req, "sess1", approval.RuleScopeSession, time.Now())
	if !ok {
		t.Fatal("expected rule to be created")
	}
	if rule.Scope != approval.RuleScopeSession {
		t.Errorf("expected session scope, got %q", rule.Scope)
	}
	if rule.Key == "" {
		t.Error("expected non-empty rule key")
	}
}

func TestMemoryRuleFor_ProjectScope(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "go", "args": []string{"build"}})
	req := &io.ApprovalRequest{
		ToolName: "run_command",
		Input:    input,
	}
	rule, ok := approval.MemoryRuleFor(req, "", approval.RuleScopeProject, time.Now())
	if !ok {
		t.Fatal("expected project rule to be created")
	}
	if rule.Scope != approval.RuleScopeProject {
		t.Errorf("expected project scope, got %q", rule.Scope)
	}
	if rule.SessionID != "" {
		t.Errorf("expected empty session ID for project scope, got %q", rule.SessionID)
	}
}

func TestMemoryRuleFor_SessionScope_NoSessionID_ReturnsFalse(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "ls"})
	req := &io.ApprovalRequest{
		ToolName: "run_command",
		Input:    input,
	}
	_, ok := approval.MemoryRuleFor(req, "", approval.RuleScopeSession, time.Now())
	if ok {
		t.Fatal("expected false when session scope but no session ID")
	}
}

func TestMemoryRuleFor_NoRuleKey_ReturnsFalse(t *testing.T) {
	// Empty tool name → no rule key generated
	req := &io.ApprovalRequest{
		ToolName: "",
		Prompt:   "do something",
	}
	_, ok := approval.MemoryRuleFor(req, "sess", approval.RuleScopeProject, time.Now())
	if ok {
		t.Fatal("expected false when no rule key can be generated")
	}
}

// ── MemoryRule.Matches ────────────────────────────────────────────────────────

func TestMemoryRule_Matches_NilRequest(t *testing.T) {
	rule := approval.MemoryRule{Key: "run_command|git"}
	if rule.Matches(nil, "sess") {
		t.Fatal("expected false for nil request")
	}
}

func TestMemoryRule_Matches_EmptyKey(t *testing.T) {
	rule := approval.MemoryRule{Key: ""}
	input, _ := json.Marshal(map[string]any{"command": "git"})
	req := &io.ApprovalRequest{ToolName: "run_command", Input: input}
	if rule.Matches(req, "sess") {
		t.Fatal("expected false for empty rule key")
	}
}

func TestMemoryRule_Matches_SameKey_ProjectScope(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "git", "args": []string{"status"}})
	req := &io.ApprovalRequest{ToolName: "run_command", Input: input, SessionID: "sess1"}
	rule, ok := approval.MemoryRuleFor(req, "sess1", approval.RuleScopeProject, time.Now())
	if !ok {
		t.Fatal("setup failed")
	}
	if !rule.Matches(req, "sess1") {
		t.Fatal("expected Matches=true for same request/project rule")
	}
}

func TestMemoryRule_Matches_SameKey_SessionScope(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "git", "args": []string{"log"}})
	req := &io.ApprovalRequest{ToolName: "run_command", Input: input, SessionID: "sess1"}
	rule, ok := approval.MemoryRuleFor(req, "sess1", approval.RuleScopeSession, time.Now())
	if !ok {
		t.Fatal("setup failed")
	}
	if !rule.Matches(req, "sess1") {
		t.Fatal("expected Matches=true for same request/session rule")
	}
}

func TestMemoryRule_Matches_DifferentSession(t *testing.T) {
	// Rule for session "sess1" created from a request with no session ID in req
	input, _ := json.Marshal(map[string]any{"command": "git", "args": []string{"diff"}})
	// Create request with no req.SessionID; session comes from currentSessionID
	req := &io.ApprovalRequest{ToolName: "run_command", Input: input}
	rule, ok := approval.MemoryRuleFor(req, "sess1", approval.RuleScopeSession, time.Now())
	if !ok {
		t.Fatal("setup failed")
	}
	// Matches with same session → should match
	if !rule.Matches(req, "sess1") {
		t.Fatal("expected Matches=true for same session")
	}
	// Matches with different session → should not match
	if rule.Matches(req, "sess2") {
		t.Fatal("expected Matches=false for different session")
	}
}

// ── MemoryRule.ScopeValue ─────────────────────────────────────────────────────

func TestScopeValue_Session(t *testing.T) {
	rule := approval.MemoryRule{Scope: approval.RuleScopeSession, SessionID: "s1"}
	if rule.ScopeValue() != approval.RuleScopeSession {
		t.Errorf("expected session scope, got %q", rule.ScopeValue())
	}
}

func TestScopeValue_Project(t *testing.T) {
	rule := approval.MemoryRule{Scope: approval.RuleScopeProject}
	if rule.ScopeValue() != approval.RuleScopeProject {
		t.Errorf("expected project scope, got %q", rule.ScopeValue())
	}
}

func TestScopeValue_Default_WithSessionID(t *testing.T) {
	rule := approval.MemoryRule{Scope: "", SessionID: "s1"}
	if rule.ScopeValue() != approval.RuleScopeSession {
		t.Errorf("expected session scope from sessionID fallback, got %q", rule.ScopeValue())
	}
}

func TestScopeValue_Default_NoSessionID(t *testing.T) {
	rule := approval.MemoryRule{Scope: "", SessionID: ""}
	if rule.ScopeValue() != approval.RuleScopeProject {
		t.Errorf("expected project scope when no sessionID, got %q", rule.ScopeValue())
	}
}

// ── ProjectRulesFromConfig ────────────────────────────────────────────────────

func TestProjectRulesFromConfig_Empty(t *testing.T) {
	rules := approval.ProjectRulesFromConfig(configpkg.TUIConfig{})
	if rules != nil {
		t.Errorf("expected nil for empty config, got %v", rules)
	}
}

func TestProjectRulesFromConfig_WithRules(t *testing.T) {
	cfg := configpkg.TUIConfig{
		ProjectApprovalRules: []configpkg.ApprovalRuleConfig{
			{Key: "run_command|git", ToolName: "run_command", Label: "git commands"},
			{Key: "http_request|GET api.example.com", ToolName: "http_request"},
		},
	}
	rules := approval.ProjectRulesFromConfig(cfg)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Key != "run_command|git" {
		t.Errorf("unexpected key: %q", rules[0].Key)
	}
}

func TestProjectRulesFromConfig_Deduplication(t *testing.T) {
	cfg := configpkg.TUIConfig{
		ProjectApprovalRules: []configpkg.ApprovalRuleConfig{
			{Key: "dup-key", ToolName: "tool"},
			{Key: "dup-key", ToolName: "tool"}, // duplicate
		},
	}
	rules := approval.ProjectRulesFromConfig(cfg)
	if len(rules) != 1 {
		t.Errorf("expected 1 rule after dedup, got %d", len(rules))
	}
}

func TestProjectRulesFromConfig_SkipsEmptyKey(t *testing.T) {
	cfg := configpkg.TUIConfig{
		ProjectApprovalRules: []configpkg.ApprovalRuleConfig{
			{Key: "", ToolName: "tool"},
			{Key: "valid-key", ToolName: "tool"},
		},
	}
	rules := approval.ProjectRulesFromConfig(cfg)
	if len(rules) != 1 {
		t.Errorf("expected 1 rule (empty key skipped), got %d", len(rules))
	}
}

// ── ProjectRuleConfigs ────────────────────────────────────────────────────────

func TestProjectRuleConfigs_Empty(t *testing.T) {
	cfgs := approval.ProjectRuleConfigs(nil)
	if cfgs != nil {
		t.Errorf("expected nil for nil input, got %v", cfgs)
	}
}

func TestProjectRuleConfigs_OnlyProjectRules(t *testing.T) {
	rules := []approval.MemoryRule{
		{Scope: approval.RuleScopeProject, Key: "run_command|git", ToolName: "run_command", Label: "git"},
		{Scope: approval.RuleScopeSession, Key: "run_command|ls", SessionID: "s1", ToolName: "run_command"},
	}
	cfgs := approval.ProjectRuleConfigs(rules)
	if len(cfgs) != 1 {
		t.Fatalf("expected 1 config (only project rules), got %d", len(cfgs))
	}
	if cfgs[0].Key != "run_command|git" {
		t.Errorf("unexpected key: %q", cfgs[0].Key)
	}
}

func TestProjectRuleConfigs_Deduplication(t *testing.T) {
	rules := []approval.MemoryRule{
		{Scope: approval.RuleScopeProject, Key: "tool|foo"},
		{Scope: approval.RuleScopeProject, Key: "tool|foo"},
	}
	cfgs := approval.ProjectRuleConfigs(rules)
	if len(cfgs) != 1 {
		t.Errorf("expected 1 config after dedup, got %d", len(cfgs))
	}
}

func TestProjectRuleConfigs_SkipsEmptyKey(t *testing.T) {
	rules := []approval.MemoryRule{
		{Scope: approval.RuleScopeProject, Key: ""},
		{Scope: approval.RuleScopeProject, Key: "valid-key"},
	}
	cfgs := approval.ProjectRuleConfigs(rules)
	if len(cfgs) != 1 {
		t.Errorf("expected 1 config (empty key skipped), got %d", len(cfgs))
	}
}
