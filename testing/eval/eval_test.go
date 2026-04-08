package eval

import (
	"context"
	mdl "github.com/mossagents/moss/kernel/model"
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
