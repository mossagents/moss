package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mossagents/moss/kernel/port"
)

const (
	memoryPipelineAgentName   = "memory-pipeline"
	memoryPipelinePhase1Item  = "phase1"
	memoryPipelinePhase2Item  = "phase2"
	memoryPipelineJobsPattern = ".moss/pipeline/jobs/*.json"
	memoryPipelineJobsDir     = ".moss/pipeline/jobs"
	memoryRegistryPath        = "MEMORY.md"
	memorySummaryPath         = "memory_summary.md"
	memoryRawMemoriesPath     = "raw_memories.md"
	memoryRolloutGlob         = "rollout_summaries/*.md"
)

type memoryPipelineJob struct {
	JobID             string    `json:"job_id"`
	SourcePath        string    `json:"source_path"`
	Trace             string    `json:"trace"`
	TargetPath        string    `json:"target_path"`
	SnapshotPath      string    `json:"snapshot_path,omitempty"`
	SnapshotSummary   string    `json:"snapshot_summary,omitempty"`
	Tags              []string  `json:"tags,omitempty"`
	Workspace         string    `json:"workspace,omitempty"`
	CWD               string    `json:"cwd,omitempty"`
	GitBranch         string    `json:"git_branch,omitempty"`
	SourceUpdatedAt   time.Time `json:"source_updated_at,omitempty"`
	RequestedAt       time.Time `json:"requested_at"`
	ExternalJobID     string    `json:"external_job_id,omitempty"`
	ExternalItemID    string    `json:"external_item_id,omitempty"`
	ExternalExecutor  string    `json:"external_executor,omitempty"`
	RolloutSummaryRel string    `json:"rollout_summary_path,omitempty"`
}

type memoryPipelineManager struct {
	ws      port.Workspace
	store   port.MemoryStore
	runtime port.TaskRuntime

	executor string
	stopCh   chan struct{}
	wakeCh   chan struct{}
	doneCh   chan struct{}

	startOnce sync.Once
	stopOnce  sync.Once
}

func newMemoryPipelineManager(ws port.Workspace, store port.MemoryStore, runtime port.TaskRuntime) *memoryPipelineManager {
	return &memoryPipelineManager{
		ws:       ws,
		store:    store,
		runtime:  runtime,
		executor: "memory-pipeline-" + uuid.NewString(),
		stopCh:   make(chan struct{}),
		wakeCh:   make(chan struct{}, 1),
		doneCh:   make(chan struct{}),
	}
}

func (m *memoryPipelineManager) Start() {
	if m == nil {
		return
	}
	m.startOnce.Do(func() {
		go m.loop()
		m.Trigger()
	})
}

func (m *memoryPipelineManager) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		close(m.stopCh)
		<-m.doneCh
	})
}

func (m *memoryPipelineManager) Trigger() {
	if m == nil {
		return
	}
	select {
	case m.wakeCh <- struct{}{}:
	default:
	}
}

func (m *memoryPipelineManager) Enqueue(ctx context.Context, job memoryPipelineJob) (*port.AgentJob, error) {
	job.TargetPath = normalizeMemoryPath(job.TargetPath)
	job.SourcePath = strings.TrimSpace(strings.ReplaceAll(job.SourcePath, "\\", "/"))
	job.Workspace = strings.TrimSpace(job.Workspace)
	job.CWD = strings.TrimSpace(job.CWD)
	job.GitBranch = strings.TrimSpace(job.GitBranch)
	job.Tags = normalizeMemoryTags(job.Tags)
	if job.JobID == "" {
		job.JobID = "memjob-" + uuid.NewString()
	}
	if job.RequestedAt.IsZero() {
		job.RequestedAt = time.Now().UTC()
	}
	if strings.TrimSpace(job.TargetPath) == "" {
		return nil, fmt.Errorf("target_path is required")
	}
	if strings.TrimSpace(job.Trace) == "" {
		return nil, fmt.Errorf("trace is required")
	}
	if err := m.saveJob(ctx, job); err != nil {
		return nil, err
	}
	jobRuntime, ok := m.runtime.(port.JobRuntime)
	if !ok {
		return nil, fmt.Errorf("memory pipeline job runtime is unavailable")
	}
	if err := jobRuntime.UpsertJob(ctx, port.AgentJob{
		ID:        job.JobID,
		AgentName: memoryPipelineAgentName,
		Goal:      "process memory pipeline for " + job.TargetPath,
		Status:    port.JobPending,
	}); err != nil {
		return nil, err
	}
	for _, itemID := range []string{memoryPipelinePhase1Item, memoryPipelinePhase2Item} {
		if err := jobRuntime.UpsertJobItem(ctx, port.AgentJobItem{
			JobID:  job.JobID,
			ItemID: itemID,
			Status: port.JobPending,
		}); err != nil {
			return nil, err
		}
	}
	created, err := jobRuntime.GetJob(ctx, job.JobID)
	if err != nil {
		return nil, err
	}
	m.Trigger()
	return created, nil
}

func (m *memoryPipelineManager) loop() {
	defer close(m.doneCh)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
		case <-m.wakeCh:
		}
		if err := m.processAll(context.Background()); err != nil {
			continue
		}
	}
}

func (m *memoryPipelineManager) processAll(ctx context.Context) error {
	jobRuntime, ok := m.runtime.(port.JobRuntime)
	if !ok {
		return fmt.Errorf("memory pipeline job runtime is unavailable")
	}
	jobs, err := collectPipelineJobs(ctx, jobRuntime)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := m.processJob(ctx, jobRuntime, job); err != nil {
			continue
		}
	}
	return nil
}

func collectPipelineJobs(ctx context.Context, runtime port.JobRuntime) ([]port.AgentJob, error) {
	pending, err := runtime.ListJobs(ctx, port.JobQuery{AgentName: memoryPipelineAgentName, Status: port.JobPending})
	if err != nil {
		return nil, err
	}
	running, err := runtime.ListJobs(ctx, port.JobQuery{AgentName: memoryPipelineAgentName, Status: port.JobRunning})
	if err != nil {
		return nil, err
	}
	out := append(pending, running...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (m *memoryPipelineManager) processJob(ctx context.Context, jobRuntime port.JobRuntime, job port.AgentJob) error {
	atomicRuntime, ok := m.runtime.(port.AtomicJobRuntime)
	if !ok {
		return fmt.Errorf("memory pipeline atomic runtime is unavailable")
	}
	payload, err := m.loadJob(ctx, job.ID)
	if err != nil {
		return m.failJob(ctx, jobRuntime, atomicRuntime, job, memoryPipelineJob{}, err)
	}
	if job.Status == port.JobPending {
		if err := jobRuntime.UpsertJob(ctx, port.AgentJob{
			ID:        job.ID,
			AgentName: job.AgentName,
			Goal:      job.Goal,
			Status:    port.JobRunning,
		}); err != nil {
			return err
		}
	}
	items, err := jobRuntime.ListJobItems(ctx, port.JobItemQuery{JobID: job.ID})
	if err != nil {
		return m.failJob(ctx, jobRuntime, atomicRuntime, job, payload, err)
	}
	itemStatus := make(map[string]port.AgentJobItem, len(items))
	for _, item := range items {
		itemStatus[item.ItemID] = item
	}
	if err := m.runPhase(ctx, atomicRuntime, job, payload, itemStatus[memoryPipelinePhase1Item], memoryPipelinePhase1Item, m.runPhase1); err != nil {
		return m.failJob(ctx, jobRuntime, atomicRuntime, job, payload, err)
	}
	payload, err = m.loadJob(ctx, job.ID)
	if err != nil {
		return m.failJob(ctx, jobRuntime, atomicRuntime, job, payload, err)
	}
	items, err = jobRuntime.ListJobItems(ctx, port.JobItemQuery{JobID: job.ID})
	if err != nil {
		return m.failJob(ctx, jobRuntime, atomicRuntime, job, payload, err)
	}
	itemStatus = make(map[string]port.AgentJobItem, len(items))
	for _, item := range items {
		itemStatus[item.ItemID] = item
	}
	if err := m.runPhase(ctx, atomicRuntime, job, payload, itemStatus[memoryPipelinePhase2Item], memoryPipelinePhase2Item, m.runPhase2); err != nil {
		return m.failJob(ctx, jobRuntime, atomicRuntime, job, payload, err)
	}
	if err := jobRuntime.UpsertJob(ctx, port.AgentJob{
		ID:        job.ID,
		AgentName: job.AgentName,
		Goal:      job.Goal,
		Status:    port.JobCompleted,
	}); err != nil {
		return err
	}
	if err := m.reportExternalResult(ctx, atomicRuntime, payload, port.JobCompleted, "memory pipeline completed", ""); err != nil {
		return err
	}
	return nil
}

func (m *memoryPipelineManager) runPhase(
	ctx context.Context,
	runtime port.AtomicJobRuntime,
	job port.AgentJob,
	payload memoryPipelineJob,
	item port.AgentJobItem,
	itemID string,
	run func(context.Context, memoryPipelineJob) (string, error),
) error {
	if item.Status == port.JobCompleted {
		return nil
	}
	if _, err := runtime.MarkJobItemRunning(ctx, job.ID, itemID, m.executor); err != nil {
		return err
	}
	result, err := run(ctx, payload)
	if err != nil {
		if _, reportErr := runtime.ReportJobItemResult(ctx, job.ID, itemID, m.executor, port.JobFailed, "", err.Error()); reportErr != nil {
			return fmt.Errorf("%v (report failed: %w)", err, reportErr)
		}
		return err
	}
	_, err = runtime.ReportJobItemResult(ctx, job.ID, itemID, m.executor, port.JobCompleted, result, "")
	return err
}

func (m *memoryPipelineManager) failJob(ctx context.Context, runtime port.JobRuntime, atomicRuntime port.AtomicJobRuntime, job port.AgentJob, payload memoryPipelineJob, cause error) error {
	_ = m.reportExternalResult(ctx, atomicRuntime, payload, port.JobFailed, "", cause.Error())
	if updateErr := runtime.UpsertJob(ctx, port.AgentJob{
		ID:        job.ID,
		AgentName: job.AgentName,
		Goal:      job.Goal,
		Status:    port.JobFailed,
	}); updateErr != nil {
		return fmt.Errorf("%v (job update failed: %w)", cause, updateErr)
	}
	return cause
}

func (m *memoryPipelineManager) runPhase1(ctx context.Context, payload memoryPipelineJob) (string, error) {
	trace, err := normalizeTrace(payload.Trace)
	if err != nil {
		return "", err
	}
	snapshotPath := payload.SnapshotPath
	if snapshotPath == "" {
		snapshotPath = buildSnapshotMemoryPath(payload.TargetPath, payload.SourcePath, payload.RequestedAt)
	}
	content := buildSnapshotMemoryContent(payload, trace)
	record, err := m.store.Upsert(ctx, port.MemoryRecord{
		Path:            snapshotPath,
		Content:         content,
		Summary:         summarizeMemoryContent(strings.Join(trace.Lines, "\n")),
		Tags:            append(append([]string{}, payload.Tags...), "snapshot"),
		Stage:           port.MemoryStageSnapshot,
		Status:          port.MemoryStatusActive,
		Group:           payload.TargetPath,
		Workspace:       payload.Workspace,
		CWD:             payload.CWD,
		GitBranch:       payload.GitBranch,
		SourceKind:      "trace",
		SourceID:        payload.JobID,
		SourcePath:      payload.SourcePath,
		SourceUpdatedAt: payload.SourceUpdatedAt,
		Citation: port.MemoryCitation{
			Entries: []port.MemoryCitationEntry{
				{
					Path:      payload.SourcePath,
					LineStart: 1,
					LineEnd:   len(strings.Split(strings.TrimSpace(payload.Trace), "\n")),
					Note:      "memory pipeline snapshot",
				},
			},
		},
	})
	if err != nil {
		return "", err
	}
	if err := m.ws.WriteFile(ctx, record.Path, []byte(record.Content)); err != nil {
		return "", err
	}
	payload.SnapshotPath = record.Path
	payload.SnapshotSummary = record.Summary
	payload.RolloutSummaryRel = rolloutSummaryArtifactPath(record.Path)
	if err := m.ws.WriteFile(ctx, payload.RolloutSummaryRel, []byte(buildRolloutSummaryContent(payload, trace, *record))); err != nil {
		return "", err
	}
	if err := m.saveJob(ctx, payload); err != nil {
		return "", err
	}
	return "snapshot " + record.Path, nil
}

func (m *memoryPipelineManager) runPhase2(ctx context.Context, payload memoryPipelineJob) (string, error) {
	snapshots, err := m.store.Search(ctx, port.MemoryQuery{
		Group:    payload.TargetPath,
		Stages:   []port.MemoryStage{port.MemoryStageSnapshot},
		Statuses: []port.MemoryStatus{port.MemoryStatusActive},
		Limit:    12,
	})
	if err != nil {
		return "", err
	}
	if len(snapshots) == 0 {
		return "", fmt.Errorf("no snapshot memories found for %s", payload.TargetPath)
	}
	content, summary, citation, sourceUpdatedAt := buildConsolidatedMemory(payload, snapshots)
	record, err := m.store.Upsert(ctx, port.MemoryRecord{
		Path:            payload.TargetPath,
		Content:         content,
		Summary:         summary,
		Tags:            append(append([]string{}, payload.Tags...), "consolidated"),
		Stage:           port.MemoryStageConsolidated,
		Status:          port.MemoryStatusActive,
		Group:           payload.TargetPath,
		Workspace:       payload.Workspace,
		CWD:             firstNonEmpty(payload.CWD, snapshots[0].CWD),
		GitBranch:       firstNonEmpty(payload.GitBranch, snapshots[0].GitBranch),
		SourceKind:      "consolidation",
		SourceID:        payload.JobID,
		SourcePath:      payload.SourcePath,
		SourceUpdatedAt: sourceUpdatedAt,
		Citation:        citation,
	})
	if err != nil {
		return "", err
	}
	if err := m.ws.WriteFile(ctx, record.Path, []byte(record.Content)); err != nil {
		return "", err
	}
	if len(citation.MemoryPaths) > 0 {
		if err := m.store.RecordUsage(ctx, citation.MemoryPaths, time.Now().UTC()); err != nil {
			return "", err
		}
	}
	if err := m.syncArtifacts(ctx); err != nil {
		return "", err
	}
	return "consolidated " + record.Path, nil
}

func (m *memoryPipelineManager) syncArtifacts(ctx context.Context) error {
	consolidated, err := m.store.Search(ctx, port.MemoryQuery{
		Stages:   []port.MemoryStage{port.MemoryStageConsolidated},
		Statuses: []port.MemoryStatus{port.MemoryStatusActive},
		Limit:    200,
	})
	if err != nil {
		return err
	}
	snapshots, err := m.store.Search(ctx, port.MemoryQuery{
		Stages:   []port.MemoryStage{port.MemoryStageSnapshot},
		Statuses: []port.MemoryStatus{port.MemoryStatusActive},
		Limit:    60,
	})
	if err != nil {
		return err
	}
	if err := m.ws.WriteFile(ctx, memoryRegistryPath, []byte(buildMemoryRegistry(consolidated))); err != nil {
		return err
	}
	if err := m.ws.WriteFile(ctx, memorySummaryPath, []byte(buildMemorySummary(consolidated))); err != nil {
		return err
	}
	if err := m.ws.WriteFile(ctx, memoryRawMemoriesPath, []byte(buildRawMemories(snapshots))); err != nil {
		return err
	}
	if err := m.pruneRolloutSummaries(ctx, snapshots); err != nil {
		return err
	}
	return nil
}

func (m *memoryPipelineManager) pruneRolloutSummaries(ctx context.Context, snapshots []port.MemoryRecord) error {
	files, err := m.ws.ListFiles(ctx, memoryRolloutGlob)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		return err
	}
	keep := make(map[string]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		keep[rolloutSummaryArtifactPath(snapshot.Path)] = struct{}{}
	}
	for _, file := range files {
		if _, ok := keep[file]; ok {
			continue
		}
		if err := m.ws.DeleteFile(ctx, file); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
			return err
		}
	}
	return nil
}

func (m *memoryPipelineManager) reportExternalResult(ctx context.Context, runtime port.AtomicJobRuntime, payload memoryPipelineJob, status port.AgentJobStatus, result string, errMsg string) error {
	if strings.TrimSpace(payload.ExternalJobID) == "" || strings.TrimSpace(payload.ExternalItemID) == "" || strings.TrimSpace(payload.ExternalExecutor) == "" {
		return nil
	}
	_, err := runtime.ReportJobItemResult(ctx, payload.ExternalJobID, payload.ExternalItemID, payload.ExternalExecutor, status, result, errMsg)
	return err
}

func (m *memoryPipelineManager) jobPath(jobID string) string {
	return filepath.ToSlash(filepath.Join(memoryPipelineJobsDir, jobID+".json"))
}

func (m *memoryPipelineManager) saveJob(ctx context.Context, job memoryPipelineJob) error {
	raw, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("encode memory pipeline job: %w", err)
	}
	return m.ws.WriteFile(ctx, m.jobPath(job.JobID), raw)
}

func (m *memoryPipelineManager) loadJob(ctx context.Context, jobID string) (memoryPipelineJob, error) {
	raw, err := m.ws.ReadFile(ctx, m.jobPath(jobID))
	if err != nil {
		return memoryPipelineJob{}, err
	}
	var job memoryPipelineJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return memoryPipelineJob{}, fmt.Errorf("decode memory pipeline job: %w", err)
	}
	return job, nil
}

func buildSnapshotMemoryPath(targetPath, sourcePath string, requestedAt time.Time) string {
	stem := sanitizeMemoryStem(sourcePath)
	if stem == "" {
		stem = sanitizeMemoryStem(targetPath)
	}
	if stem == "" {
		stem = "snapshot"
	}
	return filepath.ToSlash(filepath.Join("snapshots", requestedAt.UTC().Format("20060102-150405")+"-"+stem+".md"))
}

func rolloutSummaryArtifactPath(snapshotPath string) string {
	name := strings.TrimSuffix(filepath.Base(snapshotPath), filepath.Ext(snapshotPath))
	if name == "" {
		name = "snapshot"
	}
	return filepath.ToSlash(filepath.Join("rollout_summaries", name+".md"))
}

func sanitizeMemoryStem(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Trim(value, "/")
	if value == "" {
		return ""
	}
	base := filepath.Base(value)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		base = value
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	return strings.ToLower(out)
}

func buildSnapshotMemoryContent(payload memoryPipelineJob, trace *normalizedTrace) string {
	var b strings.Builder
	title := firstNonEmpty(payload.TargetPath, payload.SourcePath, payload.JobID)
	b.WriteString("# Snapshot Memory\n\n")
	b.WriteString("target_path: " + payload.TargetPath + "\n")
	if payload.SourcePath != "" {
		b.WriteString("source_path: " + payload.SourcePath + "\n")
	}
	if !payload.SourceUpdatedAt.IsZero() {
		b.WriteString("source_updated_at: " + payload.SourceUpdatedAt.UTC().Format(time.RFC3339) + "\n")
	}
	if payload.CWD != "" {
		b.WriteString("cwd: " + payload.CWD + "\n")
	}
	if payload.GitBranch != "" {
		b.WriteString("git_branch: " + payload.GitBranch + "\n")
	}
	b.WriteString("title: " + title + "\n")
	b.WriteString(fmt.Sprintf("trace_items: %d\n", len(trace.Lines)))
	if len(trace.Participant) > 0 {
		b.WriteString("participants: " + strings.Join(trace.Participant, ", ") + "\n")
	}
	b.WriteString("\n## Snapshot Summary\n\n")
	b.WriteString(summarizeMemoryContent(strings.Join(trace.Lines, "\n")))
	b.WriteString("\n\n## Trace Highlights\n\n")
	for _, line := range trimLines(trace.Lines, 24) {
		b.WriteString("- " + line + "\n")
	}
	return b.String()
}

func buildRolloutSummaryContent(payload memoryPipelineJob, trace *normalizedTrace, record port.MemoryRecord) string {
	var b strings.Builder
	b.WriteString("snapshot_path: " + record.Path + "\n")
	b.WriteString("target_path: " + payload.TargetPath + "\n")
	if payload.SourcePath != "" {
		b.WriteString("source_path: " + payload.SourcePath + "\n")
	}
	b.WriteString(fmt.Sprintf("trace_items: %d\n", len(trace.Lines)))
	b.WriteString(fmt.Sprintf("messages: %d\n", trace.MessageCount))
	b.WriteString("\n")
	b.WriteString(record.Summary)
	b.WriteString("\n")
	return b.String()
}

func buildConsolidatedMemory(payload memoryPipelineJob, snapshots []port.MemoryRecord) (string, string, port.MemoryCitation, time.Time) {
	sortMemoryRecords(snapshots, port.MemoryQuery{})
	highlights := make([]string, 0, 16)
	summaries := make([]string, 0, len(snapshots))
	citation := port.MemoryCitation{
		Entries:     make([]port.MemoryCitationEntry, 0, len(snapshots)*2),
		MemoryPaths: make([]string, 0, len(snapshots)),
	}
	var newest time.Time
	seenHighlight := make(map[string]struct{}, 32)
	for _, snapshot := range snapshots {
		if memoryFreshness(snapshot).After(newest) {
			newest = memoryFreshness(snapshot)
		}
		citation.MemoryPaths = append(citation.MemoryPaths, snapshot.Path)
		citation.RolloutIDs = append(citation.RolloutIDs, snapshot.Citation.RolloutIDs...)
		citation.Entries = append(citation.Entries, snapshot.Citation.Entries...)
		if snapshot.Summary != "" {
			summaries = append(summaries, snapshot.Summary)
		}
		for _, line := range trimLines(strings.Split(snapshot.Content, "\n"), 8) {
			line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
			line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if line == "" {
				continue
			}
			key := strings.ToLower(line)
			if _, ok := seenHighlight[key]; ok {
				continue
			}
			seenHighlight[key] = struct{}{}
			highlights = append(highlights, line)
			if len(highlights) == 16 {
				break
			}
		}
		if len(highlights) == 16 {
			break
		}
	}
	citation.MemoryPaths = dedupeStrings(citation.MemoryPaths)
	citation.RolloutIDs = dedupeStrings(citation.RolloutIDs)
	citation = normalizeMemoryCitation(citation)
	summary := summarizeMemoryContent(strings.Join(summaries, "\n"))
	if summary == "" {
		summary = summarizeMemoryContent(strings.Join(highlights, "\n"))
	}
	var b strings.Builder
	b.WriteString("# Consolidated Memory\n\n")
	b.WriteString("target_path: " + payload.TargetPath + "\n")
	if payload.CWD != "" {
		b.WriteString("cwd: " + payload.CWD + "\n")
	}
	if payload.GitBranch != "" {
		b.WriteString("git_branch: " + payload.GitBranch + "\n")
	}
	if !newest.IsZero() {
		b.WriteString("source_updated_at: " + newest.UTC().Format(time.RFC3339) + "\n")
	}
	b.WriteString(fmt.Sprintf("snapshot_count: %d\n\n", len(snapshots)))
	b.WriteString("## Consolidated Summary\n\n")
	b.WriteString(summary)
	b.WriteString("\n\n## Key Facts\n\n")
	for _, line := range highlights {
		b.WriteString("- " + line + "\n")
	}
	b.WriteString("\n## Evidence\n\n")
	for _, snapshot := range snapshots {
		b.WriteString("- " + snapshot.Path)
		if snapshot.SourcePath != "" {
			b.WriteString(" <- " + snapshot.SourcePath)
		}
		if snapshot.Summary != "" {
			b.WriteString(": " + snapshot.Summary)
		}
		b.WriteString("\n")
	}
	return b.String(), summary, citation, newest
}

func buildMemoryRegistry(records []port.MemoryRecord) string {
	var b strings.Builder
	b.WriteString("# Memory Registry\n\n")
	if len(records) == 0 {
		b.WriteString("No consolidated memories yet.\n")
		return b.String()
	}
	for _, record := range records {
		b.WriteString("## " + record.Path + "\n")
		if record.Group != "" {
			b.WriteString("- group: " + record.Group + "\n")
		}
		b.WriteString("- summary: " + firstNonEmpty(record.Summary, summarizeMemoryContent(record.Content)) + "\n")
		b.WriteString(fmt.Sprintf("- usage_count: %d\n", record.UsageCount))
		if !record.LastUsedAt.IsZero() {
			b.WriteString("- last_used_at: " + record.LastUsedAt.UTC().Format(time.RFC3339) + "\n")
		}
		if record.SourcePath != "" {
			b.WriteString("- source_path: " + record.SourcePath + "\n")
		}
		if len(record.Citation.MemoryPaths) > 0 {
			b.WriteString("- snapshot_paths: " + strings.Join(record.Citation.MemoryPaths, ", ") + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func buildMemorySummary(records []port.MemoryRecord) string {
	var b strings.Builder
	b.WriteString("# Memory Summary\n\n")
	if len(records) == 0 {
		b.WriteString("No consolidated memories yet.\n")
		return b.String()
	}
	for _, record := range trimMemoryRecords(records, 12) {
		b.WriteString("- `" + record.Path + "`: " + firstNonEmpty(record.Summary, summarizeMemoryContent(record.Content)) + "\n")
	}
	return b.String()
}

func buildRawMemories(records []port.MemoryRecord) string {
	var b strings.Builder
	b.WriteString("# Raw Memories\n\n")
	if len(records) == 0 {
		b.WriteString("No snapshot memories yet.\n")
		return b.String()
	}
	for _, record := range records {
		b.WriteString("## " + record.Path + "\n")
		if record.SourcePath != "" {
			b.WriteString("source_path: " + record.SourcePath + "\n")
		}
		if !record.SourceUpdatedAt.IsZero() {
			b.WriteString("source_updated_at: " + record.SourceUpdatedAt.UTC().Format(time.RFC3339) + "\n")
		}
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(record.Content))
		b.WriteString("\n\n")
	}
	return b.String()
}

func trimLines(lines []string, limit int) []string {
	out := make([]string, 0, minInt(len(lines), limit))
	for _, line := range lines {
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if line == "" {
			continue
		}
		out = append(out, line)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
