package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
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

func demoFinding(topic string, question plannedQuestion, detail reportDetail, asOf time.Time) string {
	base := fmt.Sprintf(`## %s

- Topic: %s
- Lens: %s
- As of: %s
- Finding 1: Early value appears when the swarm keeps long-running work recoverable and inspectable instead of hiding orchestration inside one prompt.
- Finding 2: Governance becomes practical only when task ownership, artifact publication, and review checkpoints are bound to durable thread/task identifiers.
- Finding 3: The easiest adoption path is a narrow product shell with clear commands for run, resume, inspect, and export.
`, question.Question, topic, question.Slug, asOf.UTC().Format(time.RFC3339))
	switch detail {
	case detailBrief:
		return base
	case detailStandard:
		return base + "- Evidence note: deterministic demo evidence stands in for live retrieval, so conclusions are illustrative rather than current.\n"
	default:
		return base + "- Evidence note: deterministic demo evidence stands in for live retrieval, so conclusions are illustrative rather than current.\n- Counterpoint: without live tools, the demo cannot prove freshness and should never be treated as a current market assessment.\n- Operational note: the durable run/task/artifact model still demonstrates how a production research swarm should preserve decision lineage.\n"
	}
}

func demoSourceSet(question plannedQuestion, asOf time.Time) string {
	return fmt.Sprintf(`[
  {"source":"demo:%s:1","summary":"Recovery-first orchestration lowers operator risk.","published_at":"unknown","retrieved_at":"%s","evidence":"Deterministic run can be resumed and exported."},
  {"source":"demo:%s:2","summary":"Shared artifacts create a durable handoff surface.","published_at":"unknown","retrieved_at":"%s","evidence":"Finding, source-set, confidence-note, and final-report artifacts are persisted separately."}
]`, question.Slug, asOf.UTC().Format(time.RFC3339), question.Slug, asOf.UTC().Format(time.RFC3339))
}

func demoConfidence(question plannedQuestion, asOf time.Time) string {
	return fmt.Sprintf("Confidence for %s: medium. As of %s the example is deterministic and useful for regression, but it does not claim external factual coverage or live-data freshness.", question.Question, asOf.UTC().Format(time.RFC3339))
}

func demoFinalReport(topic string, findings []string, detail reportDetail, asOf time.Time) string {
	report := fmt.Sprintf(`# Final Report

## Topic

%s

## As of

%s

## Synthesis

%s

## Recommendation

Build the example as a narrow swarm product shell: persist all swarm facts, make resume the default recovery path, and expose inspect/export as first-class operational tools.
`, topic, asOf.UTC().Format(time.RFC3339), strings.Join(findings, "\n"))
	if detail == detailComprehensive {
		report += `

## Risks And Limits

- Demo mode proves orchestration shape, not live factual freshness.
- Evidence is synthetic and should be replaced by tool-backed research in real mode.
- Operators should treat this output as a workflow regression artifact, not as a market brief.
`
	}
	return report
}

func demoReview(topic string) string {
	return fmt.Sprintf("Review result for %s: approved. The report is consistent with the recorded findings, captures the main trade-offs, and includes a practical rollout direction.", topic)
}
