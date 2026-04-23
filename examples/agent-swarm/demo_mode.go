package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type plannedQuestion struct {
	Slug     string `json:"slug"`
	Question string `json:"question"`
}

func demoQuestions(topic string) []plannedQuestion {
	topic = strings.TrimSpace(topic)
	return []plannedQuestion{
		{Slug: "drivers", Question: fmt.Sprintf("What are the main adoption drivers and user value propositions for %s?", topic)},
		{Slug: "risks", Question: fmt.Sprintf("What technical, operational, and governance risks could block %s?", topic)},
		{Slug: "execution", Question: fmt.Sprintf("What practical implementation path and milestones make %s credible?", topic)},
	}
}

func demoPlanFragment(topic string, questions []plannedQuestion) string {
	payload := map[string]any{
		"topic":     topic,
		"questions": questions,
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func demoFinding(topic string, question plannedQuestion) string {
	return fmt.Sprintf(`## %s

- Topic: %s
- Lens: %s
- Finding 1: Early value appears when the swarm keeps long-running work recoverable and inspectable instead of hiding orchestration inside one prompt.
- Finding 2: Governance becomes practical only when task ownership, artifact publication, and review checkpoints are bound to durable thread/task identifiers.
- Finding 3: The easiest adoption path is a narrow product shell with clear commands for run, resume, inspect, and export.
`, question.Question, topic, question.Slug)
}

func demoSourceSet(question plannedQuestion) string {
	return fmt.Sprintf(`[
  {"source":"demo:%s:1","summary":"Recovery-first orchestration lowers operator risk."},
  {"source":"demo:%s:2","summary":"Shared artifacts create a durable handoff surface."}
]`, question.Slug, question.Slug)
}

func demoConfidence(question plannedQuestion) string {
	return fmt.Sprintf("Confidence for %s: medium. The example is deterministic and useful for regression, but it does not claim external factual coverage.", question.Question)
}

func demoFinalReport(topic string, findings []string) string {
	return fmt.Sprintf(`# Final Report

## Topic

%s

## Synthesis

%s

## Recommendation

Build the example as a narrow swarm product shell: persist all swarm facts, make resume the default recovery path, and expose inspect/export as first-class operational tools.
`, topic, strings.Join(findings, "\n"))
}

func demoReview(topic string) string {
	return fmt.Sprintf("Review result for %s: approved. The report is consistent with the recorded findings, captures the main trade-offs, and includes a practical rollout direction.", topic)
}
