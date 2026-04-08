package eval

import (
	"context"
	mdl "github.com/mossagents/moss/kernel/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuleJudge_AllPass(t *testing.T) {
	judge := NewRuleJudge()
	run := EvalRun{
		CaseID: "test",
		Output: "这是一个包含 golang 和 programming 的回答",
		Steps:  3,
		ToolCalls: []ToolCallLog{
			{Name: "read_file"},
		},
	}
	expect := EvalExpect{
		Contains:   []string{"golang", "programming"},
		ToolCalled: []string{"read_file"},
		MaxSteps:   5,
	}

	score, err := judge.Score(context.Background(), run, expect)
	if err != nil {
		t.Fatal(err)
	}
	if score.Score != 1.0 {
		t.Fatalf("expected score 1.0, got %.2f: %s", score.Score, score.Reasoning)
	}
	if !score.Pass {
		t.Fatal("expected pass")
	}
}

func TestRuleJudge_Failures(t *testing.T) {
	judge := NewRuleJudge()
	run := EvalRun{
		Output: "only golang",
		Steps:  10,
	}
	expect := EvalExpect{
		Contains: []string{"golang", "missing_keyword"},
		MaxSteps: 5,
	}

	score, err := judge.Score(context.Background(), run, expect)
	if err != nil {
		t.Fatal(err)
	}
	if score.Score >= 1.0 {
		t.Fatal("expected score < 1.0")
	}
	if score.Pass {
		t.Fatal("expected fail")
	}
}

func TestEvalRunner_WithMockRun(t *testing.T) {
	cases := []EvalCase{
		{
			ID: "mock-case-1",
			Expect: EvalExpect{
				Contains: []string{"hello"},
				MaxSteps: 5,
			},
			Scoring: ScoringConfig{PassThreshold: 0.8},
		},
	}

	runner := NewRunner(RunnerConfig{
		RunCase: func(_ context.Context, c EvalCase) (EvalRun, error) {
			return EvalRun{
				CaseID:    c.ID,
				Output:    "hello world",
				Steps:     2,
				StartedAt: time.Now(),
				Duration:  time.Millisecond,
			}, nil
		},
		Judges:      []Judge{NewRuleJudge()},
		Parallelism: 1,
	})

	results, err := runner.Run(context.Background(), cases)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Pass {
		t.Fatalf("expected pass, got score=%.2f reason=%s",
			results[0].FinalScore, results[0].Scores[0].Reasoning)
	}
}

func TestLoadCase_YAML(t *testing.T) {
	c, err := LoadCase("cases/simple_greeting.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "simple-greeting" {
		t.Fatalf("expected id=simple-greeting, got %s", c.ID)
	}
	if len(c.Expect.Contains) == 0 {
		t.Fatal("expected contains list to be non-empty")
	}
}

func TestKernelRunFunc(t *testing.T) {
	runFn := KernelRunFunc(func(_ context.Context, msgs []mdl.Message) ([]mdl.Message, []ToolCallLog, int, error) {
		reply := mdl.Message{
			Role:         mdl.RoleAssistant,
			ContentParts: []mdl.ContentPart{mdl.TextPart("我是 Moss 助手，很高兴帮助你")},
		}
		return append(msgs, reply), nil, 1, nil
	})

	c := EvalCase{
		ID: "kernel-run-test",
		Input: EvalInput{
			RawMessages: []RawMessage{{Role: "user", Content: "你好"}},
		},
		Expect: EvalExpect{Contains: []string{"助手"}},
	}

	runner := NewRunner(RunnerConfig{
		RunCase: runFn,
		Judges:  []Judge{NewRuleJudge()},
	})
	results, err := runner.Run(context.Background(), []EvalCase{c})
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].Pass {
		t.Fatalf("expected pass, score=%.2f reason=%s", results[0].FinalScore, results[0].Scores[0].Reasoning)
	}
}

func TestLoadCase_InvalidValidation(t *testing.T) {
	tmpDir := t.TempDir()
	badCasePath := filepath.Join(tmpDir, "bad_case.yaml")

	badCase := `id: bad-case
input:
  messages:
    - role: user
      content: "hello"
expect:
  contains: ["ok"]
scoring:
  weights:
    rule: 0
`
	if err := os.WriteFile(badCasePath, []byte(badCase), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadCase(badCasePath)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), badCasePath) {
		t.Fatalf("expected path-aware error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "scoring.weights[\"rule\"]") {
		t.Fatalf("expected field-aware error, got %q", err.Error())
	}
}

func TestCaseCatalog_CoreCoverage(t *testing.T) {
	cases, err := LoadDir("cases")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) < 20 {
		t.Fatalf("expected at least 20 runnable cases, got %d", len(cases))
	}

	caseIDs := make(map[string]bool, len(cases))
	tagCoverage := map[string]bool{
		"coding":   false,
		"tooling":  false,
		"security": false,
		"long-run": false,
	}
	for _, c := range cases {
		caseIDs[c.ID] = true
		for _, tag := range c.Tags {
			if _, ok := tagCoverage[tag]; ok {
				tagCoverage[tag] = true
			}
		}
	}
	for tag, ok := range tagCoverage {
		if !ok {
			t.Fatalf("missing required tag coverage: %s", tag)
		}
	}

	smokeIDs := mustReadCaseIDs(t, "cases/smoke.txt")
	if len(smokeIDs) < 8 {
		t.Fatalf("expected smoke suite >= 8 cases, got %d", len(smokeIDs))
	}
	fullIDs := mustReadCaseIDs(t, "cases/full.txt")
	if len(fullIDs) < 20 {
		t.Fatalf("expected full suite >= 20 cases, got %d", len(fullIDs))
	}

	for _, id := range append(smokeIDs, fullIDs...) {
		if !caseIDs[id] {
			t.Fatalf("suite references unknown case id: %s", id)
		}
	}
}

func mustReadCaseIDs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	ids := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ids = append(ids, line)
	}
	return ids
}

func TestBaselineRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	baselinePath := filepath.Join(tmp, "baseline.json")

	results := []EvalResult{{
		Run:        EvalRun{CaseID: "case-1"},
		FinalScore: 0.91,
		Pass:       true,
	}}
	if err := WriteBaseline(baselinePath, results); err != nil {
		t.Fatal(err)
	}

	baseline, err := LoadBaseline(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.Cases) != 1 || baseline.Cases[0].CaseID != "case-1" {
		t.Fatalf("unexpected baseline content: %+v", baseline.Cases)
	}
}

func TestCompareBaseline_ThresholdBlocks(t *testing.T) {
	tmp := t.TempDir()
	baselinePath := filepath.Join(tmp, "baseline.json")
	if err := WriteBaseline(baselinePath, []EvalResult{{
		Run:        EvalRun{CaseID: "case-1"},
		FinalScore: 0.95,
		Pass:       true,
	}}); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(RunnerConfig{BaselinePath: baselinePath, GateScoreDrop: 0.05})
	decision, err := r.CompareBaseline([]EvalResult{{
		Run:        EvalRun{CaseID: "case-1"},
		FinalScore: 0.80,
		Pass:       true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Blocked {
		t.Fatalf("expected gate blocked, decision=%+v", decision)
	}
}

func TestCompareBaseline_ReportOnly(t *testing.T) {
	tmp := t.TempDir()
	baselinePath := filepath.Join(tmp, "baseline.json")
	if err := WriteBaseline(baselinePath, []EvalResult{{
		Run:        EvalRun{CaseID: "case-1"},
		FinalScore: 0.95,
		Pass:       true,
	}}); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(RunnerConfig{BaselinePath: baselinePath, GateScoreDrop: 0.05, GateReportOnly: true})
	decision, err := r.CompareBaseline([]EvalResult{{
		Run:        EvalRun{CaseID: "case-1"},
		FinalScore: 0.80,
		Pass:       true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Blocked {
		t.Fatalf("expected report-only gate, decision=%+v", decision)
	}
	if !decision.ReportOnly {
		t.Fatalf("expected report-only mode, decision=%+v", decision)
	}
}

func TestCompareBaseline_MissingBaselineFallback(t *testing.T) {
	tmp := t.TempDir()
	baselinePath := filepath.Join(tmp, "missing-baseline.json")

	r := NewRunner(RunnerConfig{BaselinePath: baselinePath, GateScoreDrop: 0.05})
	decision, err := r.CompareBaseline([]EvalResult{{
		Run:        EvalRun{CaseID: "case-1"},
		FinalScore: 0.80,
		Pass:       true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Blocked {
		t.Fatalf("expected non-blocking fallback, decision=%+v", decision)
	}
	if !decision.ReportOnly {
		t.Fatalf("expected fallback report-only, decision=%+v", decision)
	}
}

