package product

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/observe"
)

type InspectAuditSummary struct {
	Source       string                      `json:"source,omitempty"`
	SessionID    string                      `json:"session_id,omitempty"`
	RunID        string                      `json:"run_id,omitempty"`
	EventCount   int                         `json:"event_count,omitempty"`
	FirstEventAt string                      `json:"first_event_at,omitempty"`
	LastEventAt  string                      `json:"last_event_at,omitempty"`
	Context      InspectContextAuditSummary  `json:"context,omitempty"`
	Guardian     InspectGuardianAuditSummary `json:"guardian,omitempty"`
}

type InspectContextAuditSummary struct {
	Compactions            int `json:"compactions,omitempty"`
	TokensReclaimed        int `json:"tokens_reclaimed,omitempty"`
	TrimRetries            int `json:"trim_retries,omitempty"`
	TrimmedMessages        int `json:"trimmed_messages,omitempty"`
	Normalizations         int `json:"normalizations,omitempty"`
	DroppedToolResults     int `json:"dropped_tool_results,omitempty"`
	SynthesizedToolResults int `json:"synthesized_tool_results,omitempty"`
}

type InspectGuardianAuditSummary struct {
	Reviews      int `json:"reviews,omitempty"`
	AutoApproved int `json:"auto_approved,omitempty"`
	Fallbacks    int `json:"fallbacks,omitempty"`
	Errors       int `json:"errors,omitempty"`
}

type inspectAuditEntry struct {
	Timestamp string         `json:"timestamp"`
	Type      string         `json:"type"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

func buildInspectAuditSummary(sessionID, runID string) *InspectAuditSummary {
	path := AuditLogPath()
	if strings.TrimSpace(path) == "" {
		return nil
	}
	entries, err := loadInspectAuditEntries(path)
	if err != nil || len(entries) == 0 {
		return nil
	}
	return summarizeInspectAuditEntries(entries, sessionID, runID)
}

func loadInspectAuditEntries(path string) ([]inspectAuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	entries := make([]inspectAuditEntry, 0, 64)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry inspectAuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func summarizeInspectAuditEntries(entries []inspectAuditEntry, sessionID, runID string) *InspectAuditSummary {
	sessionID = strings.TrimSpace(sessionID)
	runID = strings.TrimSpace(runID)
	if sessionID == "" {
		return nil
	}
	summary := &InspectAuditSummary{
		Source:    "audit_log",
		SessionID: sessionID,
		RunID:     runID,
	}
	var firstSeen time.Time
	var lastSeen time.Time
	for _, entry := range entries {
		if entry.Type != "execution_event" || strings.TrimSpace(entry.SessionID) != sessionID {
			continue
		}
		if runID != "" && stringData(entry.Data, "run_id") != runID {
			continue
		}
		eventType := stringData(entry.Data, "type")
		tracked := true
		switch observe.ExecutionEventType(eventType) {
		case observe.ExecutionContextCompacted:
			summary.Context.Compactions++
			summary.Context.TokensReclaimed += reclaimedTokens(entry.Data)
		case observe.ExecutionContextTrimRetry:
			summary.Context.TrimRetries++
			summary.Context.TrimmedMessages += intValue(entry.Data, "messages_removed")
		case observe.ExecutionContextNormalized:
			summary.Context.Normalizations++
			summary.Context.DroppedToolResults += intValue(entry.Data, "dropped_orphan_tool_results")
			summary.Context.SynthesizedToolResults += intValue(entry.Data, "synthesized_missing_tool_results")
		case observe.ExecutionGuardianReviewed:
			summary.Guardian.Reviews++
			outcome := strings.ToLower(stringData(entry.Data, "outcome"))
			switch outcome {
			case "auto_approved":
				summary.Guardian.AutoApproved++
			case "fallback", "fallback_nil", "fallback_error":
				summary.Guardian.Fallbacks++
			}
			if strings.Contains(outcome, "error") {
				summary.Guardian.Errors++
			}
		default:
			tracked = false
		}
		if !tracked {
			continue
		}
		summary.EventCount++
		if ts, ok := parseAuditTimestamp(entry.Timestamp); ok {
			if firstSeen.IsZero() || ts.Before(firstSeen) {
				firstSeen = ts
			}
			if lastSeen.IsZero() || ts.After(lastSeen) {
				lastSeen = ts
			}
		}
	}
	if summary.EventCount == 0 {
		return nil
	}
	if !firstSeen.IsZero() {
		summary.FirstEventAt = firstSeen.UTC().Format(time.RFC3339)
	}
	if !lastSeen.IsZero() {
		summary.LastEventAt = lastSeen.UTC().Format(time.RFC3339)
	}
	return summary
}

func parseAuditTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return ts.UTC(), true
}

func reclaimedTokens(data map[string]any) int {
	before := intValue(data, "tokens_before")
	after := intValue(data, "tokens_after")
	if before <= after {
		return 0
	}
	return before - after
}

func renderInspectAuditSummary(b *strings.Builder, label string, summary *InspectAuditSummary) {
	if summary == nil {
		fmt.Fprintf(b, "%s: none\n", label)
		return
	}
	fmt.Fprintf(b, "%s: source=%s events=%d first=%s last=%s\n",
		label,
		stringOr(summary.Source, "(none)"),
		summary.EventCount,
		stringOr(summary.FirstEventAt, "(none)"),
		stringOr(summary.LastEventAt, "(none)"),
	)
	fmt.Fprintf(b, "Context audit: compactions=%d reclaimed=%d trim_retries=%d trimmed_messages=%d normalizations=%d dropped=%d synthesized=%d\n",
		summary.Context.Compactions,
		summary.Context.TokensReclaimed,
		summary.Context.TrimRetries,
		summary.Context.TrimmedMessages,
		summary.Context.Normalizations,
		summary.Context.DroppedToolResults,
		summary.Context.SynthesizedToolResults,
	)
	fmt.Fprintf(b, "Guardian audit: reviews=%d auto_approved=%d fallbacks=%d errors=%d\n",
		summary.Guardian.Reviews,
		summary.Guardian.AutoApproved,
		summary.Guardian.Fallbacks,
		summary.Guardian.Errors,
	)
}

func stringOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
