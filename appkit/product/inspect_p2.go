package product

import (
	"github.com/mossagents/moss/internal/strutil"
	"context"
	"fmt"
	appruntime "github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"sort"
	"strconv"
	"strings"
	"time"
)

type InspectReplayReport struct {
	Selector          string                `json:"selector,omitempty"`
	CheckpointID      string                `json:"checkpoint_id"`
	SessionID         string                `json:"session_id,omitempty"`
	SnapshotID        string                `json:"snapshot_id,omitempty"`
	PatchCount        int                   `json:"patch_count,omitempty"`
	Note              string                `json:"note,omitempty"`
	MetadataKeys      []string              `json:"metadata_keys,omitempty"`
	RecommendedMode   string                `json:"recommended_mode,omitempty"`
	RecommendedAction string                `json:"recommended_action,omitempty"`
	Ready             bool                  `json:"ready"`
	Notes             []string              `json:"notes,omitempty"`
	Thread            *InspectThreadSummary `json:"thread,omitempty"`
	Changes           []InspectStateItem    `json:"changes,omitempty"`
	Tasks             []InspectStateItem    `json:"tasks,omitempty"`
}

type InspectCompareReport struct {
	Left          InspectCompareTarget   `json:"left"`
	Right         InspectCompareTarget   `json:"right"`
	Relationships []string               `json:"relationships,omitempty"`
	Metrics       []InspectCompareMetric `json:"metrics,omitempty"`
}

type InspectCompareTarget struct {
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	SessionID       string `json:"session_id,omitempty"`
	Status          string `json:"status,omitempty"`
	RunID           string `json:"run_id,omitempty"`
	Source          string `json:"source,omitempty"`
	ParentID        string `json:"parent_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	SnapshotID      string `json:"snapshot_id,omitempty"`
	PatchCount      int    `json:"patch_count,omitempty"`
	CheckpointCount int    `json:"checkpoint_count,omitempty"`
	ChangeCount     int    `json:"change_count,omitempty"`
	TaskCount       int    `json:"task_count,omitempty"`
	Recoverable     bool   `json:"recoverable,omitempty"`
	Archived        bool   `json:"archived,omitempty"`
	Preview         string `json:"preview,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type InspectCompareMetric struct {
	Name  string `json:"name"`
	Left  string `json:"left,omitempty"`
	Right string `json:"right,omitempty"`
	Delta string `json:"delta,omitempty"`
}

type InspectGovernanceReport struct {
	EventWindow     int                        `json:"event_window"`
	ChangeWindow    int                        `json:"change_window"`
	Sessions        int                        `json:"sessions"`
	Runs            int                        `json:"runs"`
	Lanes           []InspectGovernanceLane    `json:"lanes,omitempty"`
	Failover        InspectGovernanceFailover  `json:"failover"`
	Approvals       InspectGovernanceApprovals `json:"approvals"`
	Changes         InspectGovernanceChanges   `json:"changes"`
	ApprovalReasons []InspectGovernanceReason  `json:"approval_reasons,omitempty"`
}

type InspectGovernanceLane struct {
	Lane          string  `json:"lane"`
	Turns         int     `json:"turns"`
	FailoverTurns int     `json:"failover_turns"`
	Exhausted     int     `json:"exhausted"`
	StableRate    float64 `json:"stable_rate,omitempty"`
}

type InspectGovernanceFailover struct {
	Attempts          int     `json:"attempts"`
	Switches          int     `json:"switches"`
	Exhausted         int     `json:"exhausted"`
	TurnsWithFailover int     `json:"turns_with_failover"`
	RecoveredTurns    int     `json:"recovered_turns"`
	RecoveryRate      float64 `json:"recovery_rate,omitempty"`
}

type InspectGovernanceApprovals struct {
	Requested int `json:"requested"`
	Resolved  int `json:"resolved"`
	Approved  int `json:"approved"`
	Denied    int `json:"denied"`
}

type InspectGovernanceChanges struct {
	Applied           int     `json:"applied"`
	RolledBack        int     `json:"rolled_back"`
	Inconsistent      int     `json:"inconsistent"`
	RollbackRate      float64 `json:"rollback_rate,omitempty"`
	InconsistencyRate float64 `json:"inconsistency_rate,omitempty"`
}

type InspectGovernanceReason struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type inspectGovernanceTurn struct {
	lane      string
	failover  bool
	exhausted bool
}

func buildInspectReplay(ctx context.Context, workspace string, catalog *appruntime.StateCatalog, target string) (*InspectReplayReport, error) {
	checkpoints, err := checkpoint.NewFileCheckpointStore(CheckpointStoreDir())
	if err != nil {
		return nil, err
	}
	record, err := ResolveCheckpointRecord(ctx, checkpoints, target)
	if err != nil {
		return nil, err
	}
	detail, err := LoadCheckpointWithStore(ctx, checkpoints, record.ID)
	if err != nil {
		return nil, err
	}
	report := &InspectReplayReport{
		Selector:     strings.TrimSpace(target),
		CheckpointID: detail.ID,
		SessionID:    detail.SessionID,
		SnapshotID:   detail.SnapshotID,
		PatchCount:   detail.PatchCount,
		Note:         detail.Note,
		MetadataKeys: append([]string(nil), detail.MetadataKeys...),
		Ready:        true,
	}
	store, err := session.NewFileStore(SessionStoreDir())
	if err == nil {
		if summaries, listErr := store.List(ctx); listErr == nil && strings.TrimSpace(detail.SessionID) != "" {
			for _, summary := range summaries {
				if summary.ID != detail.SessionID {
					continue
				}
				item := inspectThreadSummaryFromSession(summary)
				item.CheckpointCount = checkpointCountsBySession(ctx)[summary.ID]
				item.ChangeCount = countStateEntries(catalog, appruntime.StateKindChange, summary.ID)
				item.TaskCount = countStateEntries(catalog, appruntime.StateKindTask, summary.ID)
				report.Thread = &item
				break
			}
		}
	}
	report.RecommendedMode = "rerun"
	if report.Thread != nil && report.Thread.Recoverable {
		report.RecommendedMode = "resume"
	}
	report.RecommendedAction = fmt.Sprintf("/checkpoint replay %s %s", report.CheckpointID, report.RecommendedMode)
	if strings.TrimSpace(report.SnapshotID) != "" {
		report.RecommendedAction += " restore"
	}
	if report.Thread == nil {
		report.Notes = append(report.Notes, "source thread metadata is unavailable; replay can still use the checkpoint record")
	} else {
		if report.Thread.Archived {
			report.Notes = append(report.Notes, "source thread is archived; prefer rerun if resume is undesirable")
		}
		if !report.Thread.Recoverable {
			report.Notes = append(report.Notes, "source thread is not recoverable; rerun is the safer default")
		}
	}
	if strings.TrimSpace(report.SnapshotID) == "" {
		report.Notes = append(report.Notes, "checkpoint has no worktree snapshot; restore will rely on patch lineage only")
	}
	if catalog != nil && strings.TrimSpace(detail.SessionID) != "" {
		if page, err := catalog.Query(appruntime.StateQuery{Kinds: []appruntime.StateKind{appruntime.StateKindChange}, SessionID: detail.SessionID, Limit: 10}); err == nil {
			report.Changes = inspectStateItems(page.Items)
		}
		if page, err := catalog.Query(appruntime.StateQuery{Kinds: []appruntime.StateKind{appruntime.StateKindTask}, SessionID: detail.SessionID, Limit: 10}); err == nil {
			report.Tasks = inspectStateItems(page.Items)
		}
	}
	return report, nil
}

func buildInspectCompare(ctx context.Context, catalog *appruntime.StateCatalog, leftSelector, rightSelector string) (*InspectCompareReport, error) {
	store, err := session.NewFileStore(SessionStoreDir())
	if err != nil {
		return nil, err
	}
	summaries, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	checkpoints, err := checkpoint.NewFileCheckpointStore(CheckpointStoreDir())
	if err != nil {
		return nil, err
	}
	left, err := resolveInspectCompareTarget(ctx, catalog, summaries, checkpoints, leftSelector)
	if err != nil {
		return nil, err
	}
	right, err := resolveInspectCompareTarget(ctx, catalog, summaries, checkpoints, rightSelector)
	if err != nil {
		return nil, err
	}
	report := &InspectCompareReport{
		Left:  left,
		Right: right,
		Metrics: []InspectCompareMetric{
			{Name: "kind", Left: left.Kind, Right: right.Kind},
			{Name: "session", Left: strutil.FirstNonEmpty(left.SessionID, "(none)"), Right: strutil.FirstNonEmpty(right.SessionID, "(none)")},
			{Name: "status", Left: strutil.FirstNonEmpty(left.Status, "(none)"), Right: strutil.FirstNonEmpty(right.Status, "(none)")},
			{Name: "run", Left: strutil.FirstNonEmpty(left.RunID, "(none)"), Right: strutil.FirstNonEmpty(right.RunID, "(none)")},
			{Name: "checkpoints", Left: strconv.Itoa(left.CheckpointCount), Right: strconv.Itoa(right.CheckpointCount), Delta: diffInt(left.CheckpointCount, right.CheckpointCount)},
			{Name: "changes", Left: strconv.Itoa(left.ChangeCount), Right: strconv.Itoa(right.ChangeCount), Delta: diffInt(left.ChangeCount, right.ChangeCount)},
			{Name: "tasks", Left: strconv.Itoa(left.TaskCount), Right: strconv.Itoa(right.TaskCount), Delta: diffInt(left.TaskCount, right.TaskCount)},
			{Name: "patches", Left: strconv.Itoa(left.PatchCount), Right: strconv.Itoa(right.PatchCount), Delta: diffInt(left.PatchCount, right.PatchCount)},
		},
	}
	switch {
	case left.SessionID != "" && left.SessionID == right.SessionID:
		report.Relationships = append(report.Relationships, "same source thread")
	case left.TaskID != "" && left.TaskID == right.TaskID:
		report.Relationships = append(report.Relationships, "same delegated task lineage")
	}
	switch {
	case left.ID != "" && left.ID == right.ParentID:
		report.Relationships = append(report.Relationships, fmt.Sprintf("%s is parent of %s", left.ID, right.ID))
	case right.ID != "" && right.ID == left.ParentID:
		report.Relationships = append(report.Relationships, fmt.Sprintf("%s is parent of %s", right.ID, left.ID))
	case left.ParentID != "" && left.ParentID == right.ParentID:
		report.Relationships = append(report.Relationships, "same parent thread")
	}
	if left.SnapshotID != "" && left.SnapshotID == right.SnapshotID {
		report.Relationships = append(report.Relationships, "same worktree snapshot")
	}
	return report, nil
}

func buildInspectGovernance(ctx context.Context, workspace string, catalog *appruntime.StateCatalog, limit int) (*InspectGovernanceReport, error) {
	report := &InspectGovernanceReport{}
	if catalog == nil || !catalog.Enabled() {
		return report, nil
	}
	page, err := catalog.Query(appruntime.StateQuery{
		Kinds: []appruntime.StateKind{appruntime.StateKindExecutionEvent},
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	report.EventWindow = len(page.Items)
	turns := map[string]*inspectGovernanceTurn{}
	laneCounts := map[string]*InspectGovernanceLane{}
	reasons := map[string]int{}
	sessions := map[string]struct{}{}
	runs := map[string]struct{}{}
	for _, entry := range page.Items {
		event, ok := decodeInspectTraceEvent(entry)
		if !ok {
			continue
		}
		if id := strings.TrimSpace(event.SessionID); id != "" {
			sessions[id] = struct{}{}
		}
		if id := strings.TrimSpace(event.RunID); id != "" {
			runs[id] = struct{}{}
		}
		key := inspectGovernanceTurnKey(event)
		turn := turns[key]
		if turn == nil {
			turn = &inspectGovernanceTurn{}
			turns[key] = turn
		}
		switch event.Type {
		case "model.route_planned":
			turn.lane = strutil.FirstNonEmpty(stringData(event.Metadata, "lane"), "default")
		case "llm_failover_attempt":
			report.Failover.Attempts++
			turn.failover = true
			if strings.TrimSpace(stringData(event.Metadata, "failover_to")) != "" {
				report.Failover.Switches++
			}
		case "llm_failover_exhausted":
			report.Failover.Exhausted++
			turn.exhausted = true
		case string(observe.ExecutionApprovalRequest):
			report.Approvals.Requested++
			if reason := strings.TrimSpace(stringData(event.Metadata, "reason_code")); reason != "" {
				reasons[reason]++
			}
		case string(observe.ExecutionApprovalResolved):
			report.Approvals.Resolved++
			if boolValue(event.Metadata, "approved") {
				report.Approvals.Approved++
			} else {
				report.Approvals.Denied++
			}
		}
	}
	report.Sessions = len(sessions)
	report.Runs = len(runs)
	for _, turn := range turns {
		lane := strutil.FirstNonEmpty(turn.lane, "default")
		item := laneCounts[lane]
		if item == nil {
			item = &InspectGovernanceLane{Lane: lane}
			laneCounts[lane] = item
		}
		item.Turns++
		if turn.failover {
			item.FailoverTurns++
			report.Failover.TurnsWithFailover++
			if !turn.exhausted {
				report.Failover.RecoveredTurns++
			}
		}
		if turn.exhausted {
			item.Exhausted++
		}
	}
	report.Lanes = make([]InspectGovernanceLane, 0, len(laneCounts))
	for _, item := range laneCounts {
		if item.Turns > 0 {
			item.StableRate = float64(item.Turns-item.FailoverTurns) / float64(item.Turns)
		}
		report.Lanes = append(report.Lanes, *item)
	}
	sort.Slice(report.Lanes, func(i, j int) bool {
		if report.Lanes[i].Turns == report.Lanes[j].Turns {
			return report.Lanes[i].Lane < report.Lanes[j].Lane
		}
		return report.Lanes[i].Turns > report.Lanes[j].Turns
	})
	if report.Failover.TurnsWithFailover > 0 {
		report.Failover.RecoveryRate = float64(report.Failover.RecoveredTurns) / float64(report.Failover.TurnsWithFailover)
	}
	report.ApprovalReasons = topGovernanceReasons(reasons, 5)

	changeStore, err := OpenChangeStore()
	if err == nil {
		items, err := changeStore.List(ctx)
		if err == nil {
			report.ChangeWindow = len(items)
			for _, item := range items {
				switch item.Status {
				case ChangeStatusApplied:
					report.Changes.Applied++
				case ChangeStatusRolledBack:
					report.Changes.RolledBack++
				case ChangeStatusApplyInconsistent, ChangeStatusRollbackInconsistent:
					report.Changes.Inconsistent++
				}
			}
			totalSettled := report.Changes.Applied + report.Changes.RolledBack
			if totalSettled > 0 {
				report.Changes.RollbackRate = float64(report.Changes.RolledBack) / float64(totalSettled)
			}
			totalChanges := report.ChangeWindow
			if totalChanges > 0 {
				report.Changes.InconsistencyRate = float64(report.Changes.Inconsistent) / float64(totalChanges)
			}
		}
	}
	return report, nil
}

func renderInspectReplayReport(b *strings.Builder, report InspectReplayReport) {
	fmt.Fprintf(b, "Replay checkpoint: %s\n", report.CheckpointID)
	fmt.Fprintf(b, "Source session:    %s\n", strutil.FirstNonEmpty(report.SessionID, "(none)"))
	fmt.Fprintf(b, "Snapshot:          %s | patches=%d | mode=%s | ready=%t\n",
		strutil.FirstNonEmpty(report.SnapshotID, "(none)"),
		report.PatchCount,
		strutil.FirstNonEmpty(report.RecommendedMode, "(none)"),
		report.Ready,
	)
	if report.Note != "" {
		fmt.Fprintf(b, "Note:              %s\n", report.Note)
	}
	fmt.Fprintf(b, "Suggested replay:  %s\n", strutil.FirstNonEmpty(report.RecommendedAction, "(none)"))
	if report.Thread != nil {
		fmt.Fprintf(b, "Thread posture:    status=%s recoverable=%t archived=%t task=%s source=%s\n",
			strutil.FirstNonEmpty(report.Thread.Status, "(none)"),
			report.Thread.Recoverable,
			report.Thread.Archived,
			strutil.FirstNonEmpty(report.Thread.TaskID, "(none)"),
			strutil.FirstNonEmpty(report.Thread.Source, "(none)"),
		)
	}
	if len(report.Notes) > 0 {
		b.WriteString("Notes:\n")
		for _, note := range report.Notes {
			fmt.Fprintf(b, "- %s\n", note)
		}
	}
	if len(report.Changes) > 0 {
		b.WriteString("Recent changes:\n")
		for _, item := range report.Changes {
			fmt.Fprintf(b, "- %s | status=%s | title=%s\n", item.RecordID, strutil.FirstNonEmpty(item.Status, "(none)"), strutil.FirstNonEmpty(item.Title, "(none)"))
		}
	}
	if len(report.Tasks) > 0 {
		b.WriteString("Related tasks:\n")
		for _, item := range report.Tasks {
			fmt.Fprintf(b, "- %s | status=%s | title=%s\n", item.RecordID, strutil.FirstNonEmpty(item.Status, "(none)"), strutil.FirstNonEmpty(item.Title, "(none)"))
		}
	}
}

func renderInspectCompareReport(b *strings.Builder, report InspectCompareReport) {
	fmt.Fprintf(b, "Compare left:  %s (%s)\n", report.Left.ID, report.Left.Kind)
	fmt.Fprintf(b, "Compare right: %s (%s)\n", report.Right.ID, report.Right.Kind)
	if len(report.Relationships) > 0 {
		fmt.Fprintf(b, "Relationship:  %s\n", strings.Join(report.Relationships, "; "))
	}
	b.WriteString("Metrics:\n")
	for _, metric := range report.Metrics {
		fmt.Fprintf(b, "- %s | left=%s | right=%s", metric.Name, strutil.FirstNonEmpty(metric.Left, "(none)"), strutil.FirstNonEmpty(metric.Right, "(none)"))
		if metric.Delta != "" {
			fmt.Fprintf(b, " | delta=%s", metric.Delta)
		}
		b.WriteString("\n")
	}
}

func renderInspectGovernanceReport(b *strings.Builder, report InspectGovernanceReport) {
	fmt.Fprintf(b, "Governance window: events=%d changes=%d sessions=%d runs=%d\n",
		report.EventWindow, report.ChangeWindow, report.Sessions, report.Runs)
	if len(report.Lanes) == 0 {
		b.WriteString("Lane stability: none\n")
	} else {
		b.WriteString("Lane stability:\n")
		for _, lane := range report.Lanes {
			fmt.Fprintf(b, "- %s | turns=%d failover=%d exhausted=%d stable_rate=%.0f%%\n",
				strutil.FirstNonEmpty(lane.Lane, "default"),
				lane.Turns,
				lane.FailoverTurns,
				lane.Exhausted,
				lane.StableRate*100,
			)
		}
	}
	fmt.Fprintf(b, "Failover: attempts=%d switches=%d exhausted=%d recovered_turns=%d recovery_rate=%.0f%%\n",
		report.Failover.Attempts,
		report.Failover.Switches,
		report.Failover.Exhausted,
		report.Failover.RecoveredTurns,
		report.Failover.RecoveryRate*100,
	)
	fmt.Fprintf(b, "Approvals: requested=%d resolved=%d approved=%d denied=%d\n",
		report.Approvals.Requested,
		report.Approvals.Resolved,
		report.Approvals.Approved,
		report.Approvals.Denied,
	)
	fmt.Fprintf(b, "Changes: applied=%d rolled_back=%d inconsistent=%d rollback_rate=%.0f%% inconsistency_rate=%.0f%%\n",
		report.Changes.Applied,
		report.Changes.RolledBack,
		report.Changes.Inconsistent,
		report.Changes.RollbackRate*100,
		report.Changes.InconsistencyRate*100,
	)
	if len(report.ApprovalReasons) > 0 {
		b.WriteString("Approval reasons:\n")
		for _, item := range report.ApprovalReasons {
			fmt.Fprintf(b, "- %s=%d\n", item.Reason, item.Count)
		}
	}
}

func resolveInspectCompareTarget(ctx context.Context, catalog *appruntime.StateCatalog, summaries []session.SessionSummary, checkpoints checkpoint.CheckpointStore, selector string) (InspectCompareTarget, error) {
	kind, raw := splitInspectCompareSelector(selector)
	switch kind {
	case "checkpoint":
		return buildInspectCheckpointTarget(ctx, catalog, summaries, checkpoints, raw)
	default:
		return buildInspectThreadTarget(ctx, catalog, summaries, raw)
	}
}

func buildInspectThreadTarget(ctx context.Context, catalog *appruntime.StateCatalog, summaries []session.SessionSummary, target string) (InspectCompareTarget, error) {
	selected, err := resolveInspectSessionSummary(summaries, target)
	if err != nil {
		return InspectCompareTarget{}, err
	}
	item := inspectThreadSummaryFromSession(*selected)
	return InspectCompareTarget{
		Kind:            "thread",
		ID:              item.ID,
		SessionID:       item.ID,
		Status:          item.Status,
		RunID:           latestRunIDForSession(catalog, item.ID),
		Source:          item.Source,
		ParentID:        item.ParentID,
		TaskID:          item.TaskID,
		CheckpointCount: checkpointCountsBySession(ctx)[item.ID],
		ChangeCount:     countStateEntries(catalog, appruntime.StateKindChange, item.ID),
		TaskCount:       countStateEntries(catalog, appruntime.StateKindTask, item.ID),
		Recoverable:     item.Recoverable,
		Archived:        item.Archived,
		Preview:         item.Preview,
		UpdatedAt:       item.UpdatedAt,
	}, nil
}

func buildInspectCheckpointTarget(ctx context.Context, catalog *appruntime.StateCatalog, summaries []session.SessionSummary, checkpoints checkpoint.CheckpointStore, target string) (InspectCompareTarget, error) {
	record, err := ResolveCheckpointRecord(ctx, checkpoints, target)
	if err != nil {
		return InspectCompareTarget{}, err
	}
	detail, err := LoadCheckpointWithStore(ctx, checkpoints, record.ID)
	if err != nil {
		return InspectCompareTarget{}, err
	}
	item := InspectCompareTarget{
		Kind:       "checkpoint",
		ID:         detail.ID,
		SessionID:  detail.SessionID,
		RunID:      latestRunIDForSession(catalog, detail.SessionID),
		SnapshotID: detail.SnapshotID,
		PatchCount: detail.PatchCount,
		UpdatedAt:  detail.CreatedAt.UTC().Format(time.RFC3339),
	}
	for _, summary := range summaries {
		if summary.ID != detail.SessionID {
			continue
		}
		thread := inspectThreadSummaryFromSession(summary)
		item.Status = thread.Status
		item.Source = thread.Source
		item.ParentID = thread.ParentID
		item.TaskID = thread.TaskID
		item.Recoverable = thread.Recoverable
		item.Archived = thread.Archived
		item.Preview = thread.Preview
		item.CheckpointCount = checkpointCountsBySession(ctx)[thread.ID]
		item.ChangeCount = countStateEntries(catalog, appruntime.StateKindChange, thread.ID)
		item.TaskCount = countStateEntries(catalog, appruntime.StateKindTask, thread.ID)
		break
	}
	return item, nil
}

func splitInspectCompareSelector(selector string) (string, string) {
	selector = strings.TrimSpace(selector)
	switch {
	case strings.EqualFold(selector, "latest-checkpoint"):
		return "checkpoint", "latest"
	case strings.HasPrefix(strings.ToLower(selector), "checkpoint:"):
		return "checkpoint", strings.TrimSpace(selector[len("checkpoint:"):])
	case strings.HasPrefix(strings.ToLower(selector), "thread:"):
		return "thread", strings.TrimSpace(selector[len("thread:"):])
	case strings.HasPrefix(strings.ToLower(selector), "session:"):
		return "thread", strings.TrimSpace(selector[len("session:"):])
	default:
		return "thread", selector
	}
}

func latestRunIDForSession(catalog *appruntime.StateCatalog, sessionID string) string {
	if catalog == nil || !catalog.Enabled() || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	page, err := catalog.Query(appruntime.StateQuery{
		Kinds:     []appruntime.StateKind{appruntime.StateKindExecutionEvent},
		SessionID: sessionID,
		Limit:     1,
	})
	if err != nil || len(page.Items) == 0 {
		return ""
	}
	event, ok := decodeInspectTraceEvent(page.Items[0])
	if !ok {
		return ""
	}
	return strings.TrimSpace(event.RunID)
}

func inspectGovernanceTurnKey(event TraceEvent) string {
	return strings.TrimSpace(strings.Join([]string{
		strutil.FirstNonEmpty(event.SessionID, "-"),
		strutil.FirstNonEmpty(event.RunID, "-"),
		strutil.FirstNonEmpty(event.TurnID, "-"),
	}, "|"))
}

func topGovernanceReasons(counts map[string]int, limit int) []InspectGovernanceReason {
	out := make([]InspectGovernanceReason, 0, len(counts))
	for reason, count := range counts {
		out = append(out, InspectGovernanceReason{Reason: reason, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Reason < out[j].Reason
		}
		return out[i].Count > out[j].Count
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func diffInt(left, right int) string {
	delta := right - left
	switch {
	case delta > 0:
		return fmt.Sprintf("+%d", delta)
	case delta < 0:
		return strconv.Itoa(delta)
	default:
		return "0"
	}
}
