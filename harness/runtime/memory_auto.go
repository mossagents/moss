package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/hooks/governance"
	memstore "github.com/mossagents/moss/harness/runtime/memory"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/hooks"
	kernelmemory "github.com/mossagents/moss/kernel/memory"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	kplugin "github.com/mossagents/moss/kernel/plugin"
	"github.com/mossagents/moss/kernel/workspace"
)

const (
	autoMemoryConstraintKind      = "environment_constraint"
	autoMemoryPreferenceKind      = "preference"
	autoMemoryToolOutcomeKind     = "tool_outcome"
	autoMemoryExecutionLessonKind = "execution_lesson"
	autoMemoryCorrectionKind      = "correction"

	autoMemoryEnvironmentSource     = "environment.runtime"
	autoMemoryPreferenceSource      = "preference.explicit"
	autoMemoryToolSource            = "execution.tool_completed"
	autoMemoryExecutionLessonSource = "execution.lesson"

	autoMemoryToolContextPlugin = "runtime-memory-tool-context"
	autoMemoryEnvironmentPlugin = "runtime-memory-environment"
	autoMemoryPreferencePlugin  = "runtime-memory-preferences"
	autoMemoryRecallPlugin      = "runtime-memory-recall"
)

var autoMemoryRecallKinds = []string{
	autoMemoryConstraintKind,
	autoMemoryPreferenceKind,
	autoMemoryExecutionLessonKind,
	autoMemoryCorrectionKind,
	autoMemoryToolOutcomeKind,
	"manual_note",
	"trace_consolidated",
	"promoted_fact",
}

type automaticMemoryRuntime struct {
	capture  *automaticMemoryCapture
	injector *structuredMemoryInjector
}

type automaticMemoryCapture struct {
	store         memstore.ExtendedMemoryStore
	pipeline      *memstore.PipelineManager
	workspaceRoot string
	repoID        string
	userID        string

	mu          sync.Mutex
	toolContext map[string]autoToolContext
}

type autoToolContext struct {
	SessionID    string
	CallID       string
	ToolName     string
	Query        string
	Command      string
	Args         []string
	URL          string
	Host         string
	InputPreview string
	CapturedAt   time.Time
}

type toolOutcomeDescriptor struct {
	ToolName        string
	Host            string
	SubjectTerms    []string
	SubjectKey      string
	SubjectDisplay  string
	ProviderFamily  string
	ProviderKey     string
	ProviderLabel   string
	Outcome         string
	ObservationType string
	Strategy        string
	StatusCode      int
	ExitCode        int
	ErrorReason     string
}

type structuredMemoryInjector struct {
	store  memstore.ExtendedMemoryStore
	repoID string
	userID string
}

type explicitPreference struct {
	Key     string
	Summary string
	Content string
	Tags    []string
}

type memoryTopic struct {
	Canonical string
	Display   string
	Aliases   []string
	HostHints []string
}

var autoMemoryTopics = []memoryTopic{
	{
		Canonical: "weather",
		Display:   "weather/天气",
		Aliases:   []string{"weather", "forecast", "temperature", "天气", "天气预报", "预报", "气温"},
		HostHints: []string{"wttr.in", "open-meteo", "weather", "meteo", "forecast"},
	},
}

var genericSubjectStopwords = map[string]struct{}{
	"a": {}, "an": {}, "api": {}, "apis": {}, "ask": {}, "check": {}, "command": {}, "commands": {},
	"error": {}, "errors": {}, "find": {}, "get": {}, "help": {}, "how": {}, "please": {},
	"query": {}, "request": {}, "requests": {}, "show": {}, "the": {}, "tool": {}, "tools": {},
	"一个": {}, "一下": {}, "什么": {}, "使用": {}, "告诉": {}, "如何": {}, "帮我": {}, "怎么": {},
	"当前": {}, "查看": {}, "查询": {}, "获取": {}, "这个": {}, "那个": {},
}

var _ kernelmemory.ContextInjector = (*structuredMemoryInjector)(nil)

func installAutomaticMemoryRuntime(k *kernel.Kernel, st *memoryState) error {
	if k == nil || st == nil || st.store == nil || st.pipeline == nil {
		return nil
	}
	repoRoot := resolveWorkspaceRoot(k.Workspace())
	runtime := &automaticMemoryRuntime{
		capture: &automaticMemoryCapture{
			store:         st.store,
			pipeline:      st.pipeline,
			workspaceRoot: repoRoot,
			repoID:        memstore.NormalizeRepoID(repoRoot),
			userID:        currentMemoryUserID(),
			toolContext:   make(map[string]autoToolContext),
		},
		injector: &structuredMemoryInjector{
			store:  st.store,
			repoID: memstore.NormalizeRepoID(repoRoot),
			userID: currentMemoryUserID(),
		},
	}
	k.SetObserver(observe.JoinObservers(k.Observer(), runtime.capture.observer()))
	if err := k.InstallPlugin(kplugin.ToolLifecycleHook(autoMemoryToolContextPlugin, -50, runtime.capture.captureToolContext)); err != nil {
		return err
	}
	if err := k.InstallPlugin(kplugin.BeforeLLMRequestHook(autoMemoryEnvironmentPlugin, -45, runtime.capture.captureEnvironmentConstraints)); err != nil {
		return err
	}
	if err := k.InstallPlugin(kplugin.BeforeLLMRequestHook(autoMemoryPreferencePlugin, -40, runtime.capture.captureExplicitPreferences)); err != nil {
		return err
	}
	if err := k.InstallPlugin(kplugin.BeforeLLMRequestHook(autoMemoryRecallPlugin, -30, governance.RAG(governance.RAGConfig{
		Manager:   runtime.injector,
		MaxChars:  1600,
		SemanticK: 8,
	}))); err != nil {
		return err
	}
	st.auto = runtime
	return nil
}

func (c *automaticMemoryCapture) observer() observe.Observer {
	if c == nil {
		return nil
	}
	return automaticMemoryObserver{capture: c}
}

type automaticMemoryObserver struct {
	observe.NoOpObserver
	capture *automaticMemoryCapture
}

func (o automaticMemoryObserver) OnExecutionEvent(ctx context.Context, event observe.ExecutionEvent) {
	if o.capture == nil {
		return
	}
	o.capture.captureExecutionEvent(ctx, event)
}

func (c *automaticMemoryCapture) captureToolContext(_ context.Context, ev *hooks.ToolEvent) error {
	if c == nil || ev == nil || ev.Stage != hooks.ToolLifecycleBefore || ev.Session == nil {
		return nil
	}
	callID := strings.TrimSpace(ev.CallID)
	if callID == "" {
		return nil
	}
	query := latestUserMessageText(ev.Session.CopyMessages())
	command, args := firstToolCommand(ev.Input)
	toolURL := firstToolURL(ev.Input)
	host := extractHost(toolURL)
	ctxKey := c.toolContextKey(ev.Session.ID, callID)
	c.mu.Lock()
	c.toolContext[ctxKey] = autoToolContext{
		SessionID:    strings.TrimSpace(ev.Session.ID),
		CallID:       callID,
		ToolName:     strings.TrimSpace(ev.ToolName),
		Query:        query,
		Command:      command,
		Args:         args,
		URL:          toolURL,
		Host:         host,
		InputPreview: compactJSONPreview(ev.Input),
		CapturedAt:   time.Now().UTC(),
	}
	c.pruneLocked(time.Now().UTC().Add(-10 * time.Minute))
	c.mu.Unlock()
	return nil
}

func (c *automaticMemoryCapture) captureEnvironmentConstraints(ctx context.Context, ev *hooks.LLMEvent) error {
	if c == nil || ev == nil || ev.Session == nil || c.store == nil {
		return nil
	}
	for _, record := range c.environmentConstraintRecords(ev.Session.ID) {
		if _, err := c.store.UpsertExtended(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func (c *automaticMemoryCapture) environmentConstraintRecords(sourcePath string) []memstore.ExtendedMemoryRecord {
	templateCtx := appconfig.DefaultTemplateContext(c.workspaceRoot)
	osName := strings.TrimSpace(fmt.Sprint(templateCtx["OS"]))
	shell := strings.TrimSpace(fmt.Sprint(templateCtx["Shell"]))
	if osName == "" {
		return nil
	}
	summary := fmt.Sprintf("Environment constraint: current OS is %s; use %s-compatible commands.", osName, firstNonEmpty(shell, "default"))
	content := fmt.Sprintf("# Operational Constraint\n\nThe current environment is %s. Prefer %s-compatible shell syntax and commands by default.", osName, firstNonEmpty(shell, "the default"))
	tags := []string{"environment", "constraint", strings.ToLower(osName), strings.ToLower(firstNonEmpty(shell, "default")), "shell"}
	if strings.EqualFold(osName, "windows") {
		summary = "Environment constraint: current OS is Windows; prefer PowerShell or cmd; avoid bash-first."
		content = "# Operational Constraint\n\nThe current environment is Windows. Prefer PowerShell or cmd syntax by default. Do not start with bash-style commands unless the user explicitly asks for bash or a POSIX shell."
		tags = append(tags, "powershell", "cmd", "avoid-bash")
	}
	return []memstore.ExtendedMemoryRecord{{
		Path:        filepath.ToSlash(filepath.Join("constraints", "environment-"+sanitizeIdentifierPart(osName)+"-"+sanitizeIdentifierPart(firstNonEmpty(shell, "default"))+".md")),
		Content:     content,
		Summary:     summary,
		Tags:        memstore.DedupeStrings(compactNonEmpty(tags)),
		Scope:       memstore.MemoryScopeUser,
		UserID:      c.userID,
		Kind:        autoMemoryConstraintKind,
		Fingerprint: strings.ToLower(fmt.Sprintf("environment:os:%s:shell:%s", sanitizeIdentifierPart(osName), sanitizeIdentifierPart(firstNonEmpty(shell, "default")))),
		Confidence:  1.0,
		Stage:       memstore.MemoryStagePromoted,
		Status:      memstore.MemoryStatusActive,
		Workspace:   c.workspaceRoot,
		SourceKind:  autoMemoryEnvironmentSource,
		SourcePath:  strings.TrimSpace(sourcePath),
	}}
}

func (c *automaticMemoryCapture) captureExplicitPreferences(ctx context.Context, ev *hooks.LLMEvent) error {
	if c == nil || ev == nil || ev.Session == nil || c.store == nil {
		return nil
	}
	text := latestUserMessageText(ev.Session.CopyMessages())
	if strings.TrimSpace(text) == "" {
		return nil
	}
	for _, pref := range extractExplicitPreferences(text) {
		record := memstore.ExtendedMemoryRecord{
			Path:        filepath.ToSlash(filepath.Join("preferences", pref.Key+".md")),
			Content:     pref.Content,
			Summary:     pref.Summary,
			Tags:        pref.Tags,
			Scope:       memstore.MemoryScopeUser,
			UserID:      c.userID,
			Kind:        autoMemoryPreferenceKind,
			Fingerprint: pref.Key,
			Confidence:  1.0,
			Stage:       memstore.MemoryStagePromoted,
			Status:      memstore.MemoryStatusActive,
			SourceKind:  autoMemoryPreferenceSource,
			SourcePath:  ev.Session.ID,
		}
		if _, err := c.store.UpsertExtended(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func (c *automaticMemoryCapture) captureExecutionEvent(ctx context.Context, event observe.ExecutionEvent) {
	if c == nil || c.store == nil {
		return
	}
	switch event.Type {
	case observe.ExecutionToolCompleted, observe.ExecutionHostedToolCompleted, observe.ExecutionHostedToolFailed:
	default:
		return
	}
	now := eventTimeOrNow(event.Timestamp)
	info := c.popToolContext(strings.TrimSpace(event.SessionID), strings.TrimSpace(event.CallID))
	if !shouldCaptureToolOutcome(event, info) {
		return
	}
	toolName := firstNonEmpty(strings.TrimSpace(event.ToolName), info.ToolName)
	host := extractHost(firstNonEmpty(eventURL(event.Metadata), info.URL))
	topics := detectMemoryTopics(info.Query, eventURL(event.Metadata), extractHost(firstNonEmpty(eventURL(event.Metadata), info.URL)))
	subjectTerms := buildOutcomeSubjectTerms(info.Query, topics)
	note := buildToolOutcomeHint(event, info, subjectTerms, topics)
	if strings.TrimSpace(note) == "" {
		return
	}
	targetPath := buildToolOutcomeTargetPath(toolName, topics, host)
	fingerprint := buildToolOutcomeFingerprint(toolName, subjectTerms, host)
	tags := buildToolOutcomeTags(toolName, subjectTerms, topics, event, info)
	if record, ok := c.buildSessionOutcomeRecord(event, info, now, note, targetPath, fingerprint, tags, subjectTerms); ok {
		_, _ = c.store.UpsertExtended(ctx, record)
	}
	if shouldConsolidateToolOutcome(event, info) {
		if job, ok := c.buildToolOutcomeJob(event, info, now, note, targetPath, tags); ok && c.pipeline != nil {
			_, _ = c.pipeline.Enqueue(ctx, job)
		}
	}
	if lesson, ok := c.buildExecutionLessonRecord(ctx, event, info, now, subjectTerms); ok {
		_, _ = c.store.UpsertExtended(ctx, lesson)
	}
}

func (c *automaticMemoryCapture) buildSessionOutcomeRecord(event observe.ExecutionEvent, info autoToolContext, now time.Time, note, targetPath, fingerprint string, tags []string, subjectTerms []string) (memstore.ExtendedMemoryRecord, bool) {
	if strings.TrimSpace(event.SessionID) == "" || strings.TrimSpace(fingerprint) == "" {
		return memstore.ExtendedMemoryRecord{}, false
	}
	desc := describeToolOutcome(event, info, subjectTerms)
	contentLines := []string{note}
	if strings.TrimSpace(info.Query) != "" {
		contentLines = append(contentLines, "query: "+strings.TrimSpace(info.Query))
	}
	if outcome := buildToolOutcomeObservation(event, info, subjectTerms); outcome != "" {
		contentLines = append(contentLines, outcome)
	}
	metadata := map[string]any{
		"run_id":      strings.TrimSpace(event.RunID),
		"turn_id":     strings.TrimSpace(event.TurnID),
		"call_id":     strings.TrimSpace(event.CallID),
		"tool_name":   firstNonEmpty(strings.TrimSpace(event.ToolName), info.ToolName),
		"url":         firstNonEmpty(eventURL(event.Metadata), info.URL),
		"host":        extractHost(firstNonEmpty(eventURL(event.Metadata), info.URL)),
		"status_code": eventStatusCode(event.Metadata),
		"outcome":     desc.Outcome,
	}
	if desc.SubjectKey != "" {
		metadata["subject_key"] = desc.SubjectKey
	}
	if len(desc.SubjectTerms) > 0 {
		metadata["subject_terms"] = append([]string(nil), desc.SubjectTerms...)
	}
	if desc.ObservationType != "" {
		metadata["observation_type"] = desc.ObservationType
	}
	if desc.ProviderFamily != "" {
		metadata["provider_family"] = desc.ProviderFamily
	}
	if desc.ProviderKey != "" {
		metadata["provider_key"] = desc.ProviderKey
	}
	if desc.ProviderLabel != "" {
		metadata["provider_label"] = desc.ProviderLabel
	}
	if desc.ErrorReason != "" {
		metadata["error_reason"] = desc.ErrorReason
	}
	if desc.ExitCode != 0 {
		metadata["exit_code"] = desc.ExitCode
	}
	if desc.Strategy != "" {
		metadata["command_strategy"] = desc.Strategy
	}
	if strings.TrimSpace(info.Command) != "" {
		metadata["command"] = compactText(strings.TrimSpace(info.Command), 160)
	}
	if len(info.Args) > 0 {
		metadata["args"] = append([]string(nil), info.Args...)
	}
	return memstore.ExtendedMemoryRecord{
		Path:            filepath.ToSlash(filepath.Join("session_observations", sanitizePathSegment(event.SessionID), sanitizePathSegment(event.CallID)+"-"+sanitizePathSegment(fingerprint)+".md")),
		Content:         strings.Join(contentLines, "\n"),
		Summary:         note,
		Tags:            tags,
		Scope:           memstore.MemoryScopeSession,
		SessionID:       strings.TrimSpace(event.SessionID),
		RepoID:          c.repoID,
		Kind:            autoMemoryToolOutcomeKind,
		Fingerprint:     fingerprint,
		Confidence:      0.75,
		ExpiresAt:       now.Add(24 * time.Hour),
		Stage:           memstore.MemoryStageSnapshot,
		Status:          memstore.MemoryStatusActive,
		Group:           targetPath,
		Workspace:       c.workspaceRoot,
		SourceKind:      autoMemoryToolSource,
		SourceID:        firstNonEmpty(strings.TrimSpace(event.EventID), strings.TrimSpace(event.CallID)),
		SourcePath:      buildOutcomeSourcePath(event),
		SourceUpdatedAt: now,
		Metadata:        metadata,
	}, true
}

func (c *automaticMemoryCapture) buildToolOutcomeJob(event observe.ExecutionEvent, info autoToolContext, now time.Time, note, targetPath string, tags []string) (memstore.PipelineJob, bool) {
	if strings.TrimSpace(targetPath) == "" || strings.TrimSpace(note) == "" {
		return memstore.PipelineJob{}, false
	}
	subjectTerms := buildOutcomeSubjectTerms(info.Query, detectMemoryTopics(info.Query, eventURL(event.Metadata), extractHost(firstNonEmpty(eventURL(event.Metadata), info.URL))))
	traceItems := make([]map[string]any, 0, 3)
	traceItems = append(traceItems, map[string]any{
		"type":    "memory_hint",
		"content": note,
	})
	if strings.TrimSpace(info.Query) != "" {
		traceItems = append(traceItems, map[string]any{
			"type":    "message",
			"role":    "user",
			"content": strings.TrimSpace(info.Query),
		})
	}
	if observation := buildToolOutcomeObservation(event, info, subjectTerms); observation != "" {
		traceItems = append(traceItems, map[string]any{
			"type":    "tool_outcome",
			"content": observation,
		})
	}
	raw, err := json.Marshal(traceItems)
	if err != nil {
		return memstore.PipelineJob{}, false
	}
	return memstore.PipelineJob{
		SourcePath:      buildOutcomeSourcePath(event),
		Trace:           string(raw),
		TargetPath:      targetPath,
		Tags:            tags,
		Workspace:       c.workspaceRoot,
		SourceUpdatedAt: now,
		RequestedAt:     now,
	}, true
}

func (i *structuredMemoryInjector) InjectContext(ctx context.Context, cfg kernelmemory.ContextInjectConfig) (string, error) {
	if i == nil || i.store == nil {
		return "", nil
	}
	now := time.Now().UTC()
	query := strings.TrimSpace(cfg.Query)
	terms := recallQueryTerms(query)
	constraintRecords, err := i.store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes:       []memstore.MemoryScope{memstore.MemoryScopeUser},
		UserID:       i.userID,
		Kinds:        []string{autoMemoryConstraintKind, autoMemoryCorrectionKind},
		Statuses:     []memstore.MemoryStatus{memstore.MemoryStatusActive},
		NotExpiredAt: now,
		SortBy:       memstore.MemorySortByUpdatedAt,
		Limit:        8,
	})
	if err != nil {
		return "", err
	}
	preferenceRecords, err := i.store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes:       []memstore.MemoryScope{memstore.MemoryScopeUser},
		UserID:       i.userID,
		Kinds:        []string{autoMemoryPreferenceKind},
		Statuses:     []memstore.MemoryStatus{memstore.MemoryStatusActive},
		NotExpiredAt: now,
		SortBy:       memstore.MemorySortByUpdatedAt,
		Limit:        8,
	})
	if err != nil {
		return "", err
	}
	repoRecords, err := i.store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes:       []memstore.MemoryScope{memstore.MemoryScopeRepo},
		RepoID:       i.repoID,
		Kinds:        []string{autoMemoryExecutionLessonKind, autoMemoryCorrectionKind, "manual_note", "trace_consolidated", "promoted_fact"},
		Stages:       []memstore.MemoryStage{memstore.MemoryStageManual, memstore.MemoryStageConsolidated, memstore.MemoryStagePromoted},
		Statuses:     []memstore.MemoryStatus{memstore.MemoryStatusActive},
		NotExpiredAt: now,
		SortBy:       memstore.MemorySortByUpdatedAt,
		Limit:        24,
	})
	if err != nil {
		return "", err
	}
	sessionRecords, err := i.store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes:       []memstore.MemoryScope{memstore.MemoryScopeSession},
		SessionID:    cfg.SessionID,
		Kinds:        []string{autoMemoryToolOutcomeKind, autoMemoryExecutionLessonKind, autoMemoryCorrectionKind},
		Stages:       []memstore.MemoryStage{memstore.MemoryStageSnapshot, memstore.MemoryStageManual, memstore.MemoryStageConsolidated, memstore.MemoryStagePromoted},
		Statuses:     []memstore.MemoryStatus{memstore.MemoryStatusActive},
		NotExpiredAt: now,
		SortBy:       memstore.MemorySortByLastUsedAt,
		Limit:        24,
	})
	if err != nil {
		return "", err
	}
	constraintRecords = rankRecallRecords(constraintRecords, query, terms, 4, true)
	preferenceRecords = rankRecallRecords(preferenceRecords, query, terms, 4, true)
	repoRecords = rankRecallRecords(repoRecords, query, terms, 6, false)
	sessionRecords = rankRecallRecords(sessionRecords, query, terms, 4, false)
	recordedPaths := append(recallPaths(constraintRecords), recallPaths(preferenceRecords)...)
	recordedPaths = append(recordedPaths, recallPaths(repoRecords)...)
	recordedPaths = append(recordedPaths, recallPaths(sessionRecords)...)
	if len(recordedPaths) > 0 {
		_ = i.store.RecordUsage(ctx, recordedPaths, now)
	}
	return renderStructuredMemoryContext(cfg.MaxChars, constraintRecords, preferenceRecords, repoRecords, sessionRecords), nil
}

func rankRecallRecords(records []memstore.ExtendedMemoryRecord, query string, terms []string, limit int, preferAll bool) []memstore.ExtendedMemoryRecord {
	if len(records) == 0 {
		return nil
	}
	type scoredRecord struct {
		record memstore.ExtendedMemoryRecord
		score  int
	}
	seen := make(map[string]struct{}, len(records))
	scored := make([]scoredRecord, 0, len(records))
	for _, record := range records {
		key := strings.ToLower(firstNonEmpty(record.Fingerprint, record.Path))
		if key == "" {
			key = strings.ToLower(record.Path)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		score := recallScore(record, query, terms)
		if score <= 0 && !preferAll && record.Kind != autoMemoryPreferenceKind {
			continue
		}
		seen[key] = struct{}{}
		scored = append(scored, scoredRecord{record: record, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		leftConfidence := memstore.EffectiveMemoryConfidence(scored[i].record)
		rightConfidence := memstore.EffectiveMemoryConfidence(scored[j].record)
		if leftConfidence != rightConfidence {
			return leftConfidence > rightConfidence
		}
		leftFresh := memstore.MemoryFreshness(scored[i].record)
		rightFresh := memstore.MemoryFreshness(scored[j].record)
		if !leftFresh.Equal(rightFresh) {
			return leftFresh.After(rightFresh)
		}
		return scored[i].record.Path < scored[j].record.Path
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]memstore.ExtendedMemoryRecord, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.record)
	}
	return out
}

func recallScore(record memstore.ExtendedMemoryRecord, query string, terms []string) int {
	score := int(memstore.EffectiveMemoryConfidence(record) * 20)
	switch record.Kind {
	case autoMemoryConstraintKind:
		score += 220
	case autoMemoryCorrectionKind:
		score += 180
	case autoMemoryPreferenceKind:
		score += 120
	case autoMemoryExecutionLessonKind:
		score += 80
	case autoMemoryToolOutcomeKind:
		score += 30
	}
	if strings.TrimSpace(query) == "" {
		return score
	}
	matched := false
	fields := []struct {
		text   string
		weight int
	}{
		{record.Path, 8},
		{record.Kind, 7},
		{record.Fingerprint, 7},
		{record.Summary, 8},
		{record.Content, 4},
		{record.SourcePath, 3},
	}
	query = strings.TrimSpace(query)
	if containsFold(record.Summary, query) || containsFold(record.Content, query) {
		score += 30
		matched = true
	}
	for _, term := range terms {
		termMatched := false
		for _, field := range fields {
			if containsFold(field.text, term) {
				score += field.weight
				termMatched = true
			}
		}
		for _, tag := range record.Tags {
			if containsFold(tag, term) {
				score += 5
				termMatched = true
			}
		}
		matched = matched || termMatched
	}
	if !matched && record.Kind != autoMemoryPreferenceKind && record.Kind != autoMemoryConstraintKind && record.Kind != autoMemoryCorrectionKind {
		return 0
	}
	return score
}

func renderStructuredMemoryContext(maxChars int, constraintRecords, preferenceRecords, repoRecords, sessionRecords []memstore.ExtendedMemoryRecord) string {
	sections := make([]string, 0, 3)
	if len(constraintRecords) > 0 {
		sections = append(sections, "<operational_constraints>\n"+renderRecallList(constraintRecords)+"\n</operational_constraints>")
	}
	if len(preferenceRecords) > 0 {
		sections = append(sections, "<user_preferences>\n"+renderRecallList(preferenceRecords)+"\n</user_preferences>")
	}
	lessons := append([]memstore.ExtendedMemoryRecord{}, repoRecords...)
	lessons = append(lessons, sessionRecords...)
	if len(lessons) > 0 {
		sections = append(sections, "<proven_lessons>\n"+renderRecallList(lessons)+"\n</proven_lessons>")
	}
	if len(sections) == 0 {
		return ""
	}
	full := "<memory_context>\n" + strings.Join(sections, "\n") + "\n</memory_context>"
	if maxChars <= 0 || len(full) <= maxChars {
		return full
	}
	trimmed := strings.TrimSpace(full[:maxChars])
	if !strings.HasSuffix(trimmed, "</memory_context>") {
		trimmed = strings.TrimRight(trimmed, "\n ") + "\n...[truncated]\n</memory_context>"
	}
	return trimmed
}

func renderRecallList(records []memstore.ExtendedMemoryRecord) string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		summary := strings.TrimSpace(firstNonEmpty(record.Summary, compactText(record.Content, 220)))
		if summary == "" {
			continue
		}
		prefix := recallKindLabel(record.Kind)
		if prefix != "" {
			summary = prefix + ": " + summary
		}
		lines = append(lines, "- "+summary)
	}
	return strings.Join(lines, "\n")
}

func recallKindLabel(kind string) string {
	switch strings.TrimSpace(kind) {
	case autoMemoryConstraintKind:
		return "Constraint"
	case autoMemoryPreferenceKind:
		return "Preference"
	case autoMemoryToolOutcomeKind:
		return "Tool outcome"
	case autoMemoryExecutionLessonKind:
		return "Lesson"
	case autoMemoryCorrectionKind:
		return "Correction"
	case "promoted_fact":
		return "Promoted memory"
	case "trace_consolidated":
		return "Prior solution"
	case "manual_note":
		return "Saved note"
	default:
		return ""
	}
}

func extractExplicitPreferences(text string) []explicitPreference {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return nil
	}
	lower := strings.ToLower(raw)
	prefs := make([]explicitPreference, 0, 4)
	if wantsChineseReply(raw, lower) {
		prefs = append(prefs, explicitPreference{
			Key:     "language-zh",
			Summary: "Reply in Chinese by default.",
			Content: "# User Preference\n\nReply in Chinese by default unless the user asks for another language.",
			Tags:    []string{"preference", "language", "zh", "中文"},
		})
	}
	if wantsEnglishReply(raw, lower) {
		prefs = append(prefs, explicitPreference{
			Key:     "language-en",
			Summary: "Reply in English by default.",
			Content: "# User Preference\n\nReply in English by default unless the user asks for another language.",
			Tags:    []string{"preference", "language", "en", "english"},
		})
	}
	if wantsConciseReply(raw, lower) {
		prefs = append(prefs, explicitPreference{
			Key:     "response-style-concise",
			Summary: "Keep responses concise by default.",
			Content: "# User Preference\n\nKeep responses concise unless detail is explicitly requested.",
			Tags:    []string{"preference", "style", "concise", "简洁"},
		})
	}
	if wantsDetailedReply(raw, lower) {
		prefs = append(prefs, explicitPreference{
			Key:     "response-style-detailed",
			Summary: "Provide detailed explanations by default.",
			Content: "# User Preference\n\nProvide detailed explanations by default.",
			Tags:    []string{"preference", "style", "detailed", "详细"},
		})
	}
	return prefs
}

func wantsChineseReply(raw, lower string) bool {
	return (strings.Contains(raw, "中文") || strings.Contains(lower, "chinese")) &&
		(strings.Contains(raw, "回复") || strings.Contains(raw, "回答") || strings.Contains(raw, "交流") || strings.Contains(raw, "都用") || strings.Contains(raw, "以后") || strings.Contains(lower, "reply") || strings.Contains(lower, "respond"))
}

func wantsEnglishReply(raw, lower string) bool {
	return (strings.Contains(raw, "英文") || strings.Contains(lower, "english")) &&
		(strings.Contains(raw, "回复") || strings.Contains(raw, "回答") || strings.Contains(raw, "都用") || strings.Contains(raw, "以后") || strings.Contains(lower, "reply") || strings.Contains(lower, "respond"))
}

func wantsConciseReply(raw, lower string) bool {
	return strings.Contains(raw, "简洁") || strings.Contains(raw, "简短") || strings.Contains(raw, "少说") || strings.Contains(lower, "concise") || strings.Contains(lower, "brief")
}

func wantsDetailedReply(raw, lower string) bool {
	return strings.Contains(raw, "详细") || strings.Contains(raw, "展开") || strings.Contains(lower, "detailed") || strings.Contains(lower, "verbose")
}

func buildToolOutcomeHint(event observe.ExecutionEvent, info autoToolContext, subjectTerms []string, topics []memoryTopic) string {
	desc := describeToolOutcome(event, info, subjectTerms)
	subject := "similar requests"
	if label := buildOutcomeSubjectDisplay(subjectTerms, topics); strings.TrimSpace(label) != "" {
		subject = label + " requests"
	}
	if desc.SubjectDisplay != "" {
		subject = desc.SubjectDisplay + " requests"
	}
	method := firstNonEmpty(strings.TrimSpace(strings.ToUpper(fmt.Sprint(event.Metadata["method"]))), "request")
	resource := firstNonEmpty(desc.ProviderLabel, desc.Host, firstNonEmpty(strings.TrimSpace(event.ToolName), info.ToolName), "tool")
	if strings.EqualFold(desc.ToolName, "run_command") && strings.EqualFold(strings.TrimSpace(fmt.Sprint(appconfig.DefaultTemplateContext("")["OS"])), "windows") && isBashStyleStrategy(desc.Strategy) && desc.Outcome == "failure" {
		reason := firstNonEmpty(desc.ErrorReason, "failed")
		return fmt.Sprintf("Avoid bash-style commands on Windows for %s: run_command failed (%s).", subject, reason)
	}
	if desc.Outcome == "failure" {
		reason := firstNonEmpty(desc.ErrorReason, "failed")
		if desc.StatusCode > 0 {
			return fmt.Sprintf("Avoid %s by default for %s: %s returned %d (%s).", resource, subject, method, desc.StatusCode, reason)
		}
		if desc.ExitCode != 0 {
			return fmt.Sprintf("Avoid %s by default for %s: %s exited with code %d (%s).", resource, subject, method, desc.ExitCode, reason)
		}
		return fmt.Sprintf("Avoid %s by default for %s: %s failed (%s).", resource, subject, method, reason)
	}
	if desc.StatusCode > 0 {
		return fmt.Sprintf("Prefer %s for %s: %s returned %d successfully.", resource, subject, method, desc.StatusCode)
	}
	return fmt.Sprintf("Prefer %s for %s: %s succeeded.", resource, subject, method)
}

func buildToolOutcomeObservation(event observe.ExecutionEvent, info autoToolContext, subjectTerms []string) string {
	desc := describeToolOutcome(event, info, subjectTerms)
	parts := make([]string, 0, 12)
	parts = append(parts, fmt.Sprintf("tool=%s", firstNonEmpty(strings.TrimSpace(event.ToolName), info.ToolName)))
	if urlValue := firstNonEmpty(eventURL(event.Metadata), info.URL); urlValue != "" {
		parts = append(parts, "url="+urlValue)
	}
	if desc.Host != "" {
		parts = append(parts, "host="+desc.Host)
	}
	if desc.SubjectKey != "" {
		parts = append(parts, "subject="+desc.SubjectKey)
	}
	if desc.ProviderFamily != "" {
		parts = append(parts, "provider_family="+desc.ProviderFamily)
	}
	if desc.ProviderKey != "" {
		parts = append(parts, "provider_key="+desc.ProviderKey)
	}
	if desc.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status=%d", desc.StatusCode))
	}
	if desc.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit_code=%d", desc.ExitCode))
	}
	if desc.Strategy != "" {
		parts = append(parts, "command_strategy="+desc.Strategy)
	}
	if desc.Outcome == "failure" {
		parts = append(parts, "outcome=failure")
		if desc.ErrorReason != "" {
			parts = append(parts, "error="+desc.ErrorReason)
		}
	} else {
		parts = append(parts, "outcome=success")
	}
	if info.Query != "" {
		parts = append(parts, "query="+strings.TrimSpace(info.Query))
	}
	return strings.Join(parts, " ")
}

func buildToolOutcomeTargetPath(toolName string, topics []memoryTopic, host string) string {
	parts := []string{sanitizePathSegment(toolName)}
	for _, topic := range topics {
		if topic.Canonical != "" {
			parts = append(parts, sanitizePathSegment(topic.Canonical))
		}
	}
	if host != "" {
		parts = append(parts, sanitizePathSegment(host))
	}
	parts = compactNonEmpty(parts)
	if len(parts) == 0 {
		return ""
	}
	return filepath.ToSlash(filepath.Join("auto", "tool_outcomes", strings.Join(parts, "-")+".md"))
}

func buildToolOutcomeFingerprint(toolName string, subjectTerms []string, host string) string {
	parts := []string{strings.TrimSpace(toolName)}
	if subjectKey := buildOutcomeSubjectKey(subjectTerms); subjectKey != "" {
		parts = append(parts, subjectKey)
	}
	if host != "" {
		parts = append(parts, host)
	}
	parts = compactNonEmpty(parts)
	return strings.Join(parts, ":")
}

func buildToolOutcomeTags(toolName string, subjectTerms []string, topics []memoryTopic, event observe.ExecutionEvent, info autoToolContext) []string {
	tags := []string{"auto", "tool-outcome", sanitizePathSegment(toolName)}
	if host := extractHost(firstNonEmpty(eventURL(event.Metadata), info.URL)); host != "" {
		tags = append(tags, host)
	}
	for _, topic := range topics {
		tags = append(tags, topic.Canonical)
		tags = append(tags, topic.Aliases...)
	}
	if len(topics) == 0 {
		tags = append(tags, subjectTerms...)
	}
	if desc := describeToolOutcome(event, info, subjectTerms); desc.ProviderFamily != "" {
		tags = append(tags, desc.ProviderFamily)
	}
	if desc := describeToolOutcome(event, info, subjectTerms); desc.ProviderKey != "" {
		tags = append(tags, sanitizePathSegment(desc.ProviderKey))
	}
	if strategy := classifyCommandStrategy(info); strategy != "" {
		tags = append(tags, strategy)
	}
	if eventFailed(event, eventStatusCode(event.Metadata)) {
		tags = append(tags, "failure")
	} else {
		tags = append(tags, "success")
	}
	return memstore.DedupeStrings(compactNonEmpty(tags))
}

func shouldCaptureToolOutcome(event observe.ExecutionEvent, info autoToolContext) bool {
	if strings.TrimSpace(event.ToolName) == "" && strings.TrimSpace(info.ToolName) == "" {
		return false
	}
	failed := eventFailed(event, eventStatusCode(event.Metadata))
	if observationType := classifyOutcomeObservation(event, info).ObservationType; observationType != "" && observationType != "tool_outcome" {
		return true
	}
	if firstNonEmpty(eventURL(event.Metadata), info.URL) != "" {
		return true
	}
	switch strings.ToLower(firstNonEmpty(strings.TrimSpace(event.ToolName), info.ToolName)) {
	case "http_request", "fetch_webpage":
		return true
	default:
		return strings.TrimSpace(info.Query) != "" && failed
	}
}

func shouldConsolidateToolOutcome(event observe.ExecutionEvent, info autoToolContext) bool {
	if eventFailed(event, eventStatusCode(event.Metadata)) {
		return false
	}
	switch classifyOutcomeObservation(event, info).ObservationType {
	case "file_search_provider", "package_manager_provider", "database_tool_provider":
		return false
	}
	if firstNonEmpty(eventURL(event.Metadata), info.URL) != "" {
		return true
	}
	switch strings.ToLower(firstNonEmpty(strings.TrimSpace(event.ToolName), info.ToolName)) {
	case "http_request", "fetch_webpage":
		return true
	default:
		return false
	}
}

func detectMemoryTopics(query, rawURL, host string) []memoryTopic {
	query = strings.TrimSpace(query)
	urlText := strings.ToLower(strings.TrimSpace(rawURL))
	host = strings.ToLower(strings.TrimSpace(host))
	out := make([]memoryTopic, 0, 2)
	for _, topic := range autoMemoryTopics {
		if topicMatches(topic, query, urlText, host) {
			out = append(out, topic)
		}
	}
	return out
}

func topicMatches(topic memoryTopic, query, rawURL, host string) bool {
	lowerQuery := strings.ToLower(query)
	for _, alias := range topic.Aliases {
		aliasLower := strings.ToLower(alias)
		if aliasLower != "" && (strings.Contains(lowerQuery, aliasLower) || strings.Contains(query, alias)) {
			return true
		}
	}
	for _, hint := range topic.HostHints {
		hintLower := strings.ToLower(hint)
		if hintLower != "" && (strings.Contains(host, hintLower) || strings.Contains(rawURL, hintLower)) {
			return true
		}
	}
	return false
}

func buildOutcomeSubjectTerms(query string, topics []memoryTopic) []string {
	if len(topics) > 0 {
		terms := make([]string, 0, len(topics))
		seen := make(map[string]struct{}, len(topics))
		for _, topic := range topics {
			term := strings.TrimSpace(topic.Canonical)
			if term == "" {
				continue
			}
			if _, ok := seen[term]; ok {
				continue
			}
			seen[term] = struct{}{}
			terms = append(terms, term)
		}
		if len(terms) > 0 {
			return terms
		}
	}
	return extractGenericSubjectTerms(query, 3)
}

func buildOutcomeSubjectDisplay(subjectTerms []string, topics []memoryTopic) string {
	if len(topics) > 0 {
		display := make([]string, 0, len(topics))
		seen := make(map[string]struct{}, len(topics))
		for _, topic := range topics {
			label := strings.TrimSpace(firstNonEmpty(topic.Display, topic.Canonical))
			if label == "" {
				continue
			}
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			display = append(display, label)
		}
		if len(display) > 0 {
			return strings.Join(display, ", ")
		}
	}
	if len(subjectTerms) == 0 {
		return ""
	}
	return strings.Join(subjectTerms, "/")
}

func extractGenericSubjectTerms(query string, limit int) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	terms := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	add := func(term string) {
		term = strings.TrimSpace(term)
		if !isUsefulSubjectTerm(term) {
			return
		}
		key := strings.ToLower(term)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		terms = append(terms, term)
	}
	for _, run := range extractHanRuns(query) {
		normalized := trimGenericSubjectRun(run)
		if runeLen := len([]rune(normalized)); runeLen >= 2 && runeLen <= 12 {
			add(normalized)
		}
	}
	for _, token := range strings.FieldsFunc(strings.ToLower(query), isQueryDelimiter) {
		add(token)
	}
	if limit > 0 && len(terms) > limit {
		return append([]string(nil), terms[:limit]...)
	}
	return terms
}

func trimGenericSubjectRun(term string) string {
	term = strings.TrimSpace(term)
	for _, prefix := range []string{"请帮我", "帮我", "请", "查询", "查看", "获取", "告诉我", "我想知道", "帮忙", "查一下", "看一下"} {
		term = strings.TrimSpace(strings.TrimPrefix(term, prefix))
	}
	for _, suffix := range []string{"怎么样", "是什么", "是多少", "可以吗", "一下", "吧", "呢"} {
		term = strings.TrimSpace(strings.TrimSuffix(term, suffix))
	}
	return term
}

func isUsefulSubjectTerm(term string) bool {
	term = strings.TrimSpace(term)
	if term == "" {
		return false
	}
	if _, ok := genericSubjectStopwords[strings.ToLower(term)]; ok {
		return false
	}
	if len([]rune(term)) < 2 {
		return false
	}
	allDigits := true
	for _, r := range term {
		if !unicode.IsDigit(r) {
			allDigits = false
			break
		}
	}
	return !allDigits
}

func buildOutcomeSubjectKey(subjectTerms []string) string {
	parts := make([]string, 0, len(subjectTerms))
	for _, term := range subjectTerms {
		if part := sanitizePathSegment(term); part != "" {
			parts = append(parts, part)
		}
	}
	parts = compactNonEmpty(parts)
	if len(parts) > 0 {
		if len(parts) > 3 {
			parts = parts[:3]
		}
		return strings.Join(parts, "-")
	}
	raw := strings.Join(compactNonEmpty(subjectTerms), "-")
	if raw == "" {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(raw)))
	return fmt.Sprintf("topic-%x", h.Sum32())
}

func describeToolOutcome(event observe.ExecutionEvent, info autoToolContext, subjectTerms []string) toolOutcomeDescriptor {
	toolName := strings.ToLower(firstNonEmpty(strings.TrimSpace(event.ToolName), info.ToolName))
	host := extractHost(firstNonEmpty(eventURL(event.Metadata), info.URL))
	statusCode := eventStatusCode(event.Metadata)
	exitCode := eventExitCode(event.Metadata)
	outcome := "success"
	if eventFailed(event, statusCode) {
		outcome = "failure"
	}
	strategy := classifyCommandStrategy(info)
	classified := classifyOutcomeObservation(event, info)
	observationType := classified.ObservationType
	if observationType == "" {
		observationType = "tool_outcome"
	}
	subjectKey := buildOutcomeSubjectKey(subjectTerms)
	subjectDisplay := buildOutcomeSubjectDisplay(subjectTerms, nil)
	if classified.SubjectKey != "" {
		subjectKey = classified.SubjectKey
	}
	if classified.SubjectDisplay != "" {
		subjectDisplay = classified.SubjectDisplay
	}
	return toolOutcomeDescriptor{
		ToolName:        toolName,
		Host:            host,
		SubjectTerms:    append([]string(nil), subjectTerms...),
		SubjectKey:      subjectKey,
		SubjectDisplay:  subjectDisplay,
		ProviderFamily:  classified.ProviderFamily,
		ProviderKey:     classified.ProviderKey,
		ProviderLabel:   classified.ProviderLabel,
		Outcome:         outcome,
		ObservationType: observationType,
		Strategy:        strategy,
		StatusCode:      statusCode,
		ExitCode:        exitCode,
		ErrorReason:     firstNonEmpty(strings.TrimSpace(event.Error), errorReason(event.Metadata)),
	}
}

func classifyOutcomeObservation(event observe.ExecutionEvent, info autoToolContext) toolOutcomeDescriptor {
	toolName := strings.ToLower(firstNonEmpty(strings.TrimSpace(event.ToolName), info.ToolName))
	host := extractHost(firstNonEmpty(eventURL(event.Metadata), info.URL))
	if host != "" && isAPIOutcomeTool(toolName) {
		return toolOutcomeDescriptor{ObservationType: "api_host"}
	}
	if toolName == "run_command" {
		if family, key, label := classifyRunCommandProvider(info); family != "" && key != "" {
			subjectKey, subjectDisplay := providerFamilySubject(family)
			return toolOutcomeDescriptor{
				ObservationType: providerFamilyObservationType(family),
				ProviderFamily:  family,
				ProviderKey:     key,
				ProviderLabel:   label,
				SubjectKey:      subjectKey,
				SubjectDisplay:  subjectDisplay,
			}
		}
		if strategy := classifyCommandStrategy(info); strategy != "" {
			return toolOutcomeDescriptor{ObservationType: "command_strategy"}
		}
	}
	if family, key, label := classifyDirectToolProvider(toolName); family != "" && key != "" {
		subjectKey, subjectDisplay := providerFamilySubject(family)
		return toolOutcomeDescriptor{
			ObservationType: providerFamilyObservationType(family),
			ProviderFamily:  family,
			ProviderKey:     key,
			ProviderLabel:   label,
			SubjectKey:      subjectKey,
			SubjectDisplay:  subjectDisplay,
		}
	}
	return toolOutcomeDescriptor{ObservationType: "tool_outcome"}
}

func providerFamilyObservationType(family string) string {
	switch strings.TrimSpace(family) {
	case "file_search":
		return "file_search_provider"
	case "package_manager":
		return "package_manager_provider"
	case "database_tool":
		return "database_tool_provider"
	default:
		return ""
	}
}

func providerFamilySubject(family string) (string, string) {
	switch strings.TrimSpace(family) {
	case "file_search":
		return "file-search", "file search"
	case "package_manager":
		return "package-management", "package management"
	case "database_tool":
		return "database-tooling", "database tool"
	default:
		return "", ""
	}
}

func classifyDirectToolProvider(toolName string) (string, string, string) {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "glob":
		return "file_search", "glob", "glob"
	case "grep":
		return "file_search", "grep", "grep"
	case "file_search_call":
		return "file_search", "file_search_call", "hosted file search"
	default:
		return "", "", ""
	}
}

func classifyRunCommandProvider(info autoToolContext) (string, string, string) {
	if !strings.EqualFold(strings.TrimSpace(info.ToolName), "run_command") {
		return "", "", ""
	}
	base := strings.ToLower(filepath.Base(strings.TrimSpace(info.Command)))
	if base == "" || isShellWrapperCommand(base) {
		return "", "", ""
	}
	if key := classifyPackageManagerCommand(base, info.Args); key != "" {
		return "package_manager", key, packageManagerLabel(key)
	}
	if key := classifyDatabaseToolBase(base); key != "" {
		return "database_tool", key, databaseToolLabel(key)
	}
	if key := classifyFileSearchBase(base); key != "" {
		return "file_search", key, fileSearchProviderLabel(key)
	}
	return "", "", ""
}

func isShellWrapperCommand(base string) bool {
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "bash", "sh", "zsh", "fish", "ksh", "powershell", "powershell.exe", "pwsh", "pwsh.exe", "cmd", "cmd.exe":
		return true
	default:
		return false
	}
}

func classifyFileSearchBase(base string) string {
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "rg", "ripgrep", "grep", "find", "fd", "dir":
		return strings.ToLower(strings.TrimSpace(base))
	default:
		return ""
	}
}

func classifyPackageManagerCommand(base string, args []string) string {
	base = strings.ToLower(strings.TrimSpace(base))
	args = lowerTrimmedArgs(args)
	first := ""
	second := ""
	if len(args) > 0 {
		first = args[0]
	}
	if len(args) > 1 {
		second = args[1]
	}
	switch base {
	case "uv":
		switch first {
		case "add", "remove", "sync", "lock", "export", "tree":
			return "uv"
		case "pip":
			switch second {
			case "install", "uninstall", "sync":
				return "uv"
			}
		case "tool":
			if second == "install" || second == "uninstall" {
				return "uv"
			}
		case "python":
			if second == "install" {
				return "uv"
			}
		}
	case "pip", "pip3":
		switch first {
		case "install", "uninstall", "download", "wheel", "list", "show", "freeze", "check":
			return "pip"
		}
	case "poetry":
		switch first {
		case "add", "remove", "install", "update", "lock", "show", "export":
			return "poetry"
		}
	case "npm", "pnpm", "yarn", "bun":
		switch first {
		case "install", "add", "remove", "uninstall", "update", "up", "upgrade", "prune", "dedupe":
			return base
		}
	case "go":
		switch first {
		case "get", "install":
			return "go"
		case "mod":
			switch second {
			case "tidy", "download", "vendor":
				return "go"
			}
		}
	case "cargo":
		switch first {
		case "add", "install", "remove", "update", "fetch":
			return "cargo"
		}
	}
	return ""
}

func lowerTrimmedArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if trimmed := strings.ToLower(strings.TrimSpace(arg)); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func classifyDatabaseToolBase(base string) string {
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "prisma", "psql", "sqlite3", "mysql", "mongosh", "redis-cli", "goose", "migrate", "atlas":
		return strings.ToLower(strings.TrimSpace(base))
	default:
		return ""
	}
}

func fileSearchProviderLabel(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "file_search_call":
		return "hosted file search"
	case "rg", "ripgrep":
		return "ripgrep"
	case "fd":
		return "fd"
	default:
		return strings.TrimSpace(key)
	}
}

func packageManagerLabel(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "uv":
		return "uv"
	case "pip", "pip3":
		return "pip"
	case "poetry":
		return "Poetry"
	case "npm":
		return "npm"
	case "pnpm":
		return "pnpm"
	case "yarn":
		return "Yarn"
	case "bun":
		return "Bun"
	case "go":
		return "go toolchain"
	case "cargo":
		return "Cargo"
	default:
		return strings.TrimSpace(key)
	}
}

func databaseToolLabel(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "prisma":
		return "Prisma CLI"
	case "psql":
		return "psql"
	case "sqlite3":
		return "sqlite3"
	case "mysql":
		return "mysql"
	case "mongosh":
		return "mongosh"
	case "redis-cli":
		return "redis-cli"
	case "goose":
		return "goose"
	case "migrate":
		return "migrate"
	case "atlas":
		return "Atlas CLI"
	default:
		return strings.TrimSpace(key)
	}
}

func isAPIOutcomeTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "http_request", "fetch_webpage":
		return true
	default:
		return false
	}
}

func classifyCommandStrategy(info autoToolContext) string {
	if !strings.EqualFold(strings.TrimSpace(info.ToolName), "run_command") {
		return ""
	}
	command := strings.TrimSpace(info.Command)
	if command == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(command))
	switch base {
	case "bash", "sh", "zsh", "fish", "ksh":
		return "bash-style"
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return "powershell"
	case "cmd", "cmd.exe":
		return "cmd"
	}
	lower := strings.ToLower(command)
	if len(info.Args) == 0 {
		if looksLikePowerShellCommand(lower) {
			return "powershell-style"
		}
		if looksLikePOSIXCommand(lower) {
			return "bash-style"
		}
		return "shell-form"
	}
	return "structured"
}

func looksLikePOSIXCommand(command string) bool {
	for _, prefix := range []string{"ls", "pwd", "cat", "grep", "find", "which", "export ", "echo $", "chmod ", "rm ", "mv ", "cp "} {
		if command == strings.TrimSpace(prefix) || strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return strings.Contains(command, "$") || strings.Contains(command, "./")
}

func looksLikePowerShellCommand(command string) bool {
	for _, prefix := range []string{"get-", "set-", "select-", "where-", "write-host", "test-path", "$env:"} {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return strings.Contains(command, "$env:")
}

func isBashStyleStrategy(strategy string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(strategy)), "bash")
}

func (c *automaticMemoryCapture) buildExecutionLessonRecord(ctx context.Context, event observe.ExecutionEvent, info autoToolContext, now time.Time, subjectTerms []string) (memstore.ExtendedMemoryRecord, bool) {
	if c == nil || c.store == nil {
		return memstore.ExtendedMemoryRecord{}, false
	}
	desc := describeToolOutcome(event, info, subjectTerms)
	switch desc.ObservationType {
	case "api_host":
		if desc.SubjectKey == "" || desc.Host == "" {
			return memstore.ExtendedMemoryRecord{}, false
		}
		if desc.Outcome == "success" {
			failedHosts, err := c.relatedFailedAPIHosts(ctx, strings.TrimSpace(event.SessionID), desc, now)
			if err != nil || len(failedHosts) == 0 {
				return memstore.ExtendedMemoryRecord{}, false
			}
			return c.buildPreferredAPILessonRecord(event, now, desc, failedHosts), true
		}
		failureCount, err := c.countRepeatedAPIFailures(ctx, desc, now)
		if err != nil || failureCount < 2 {
			return memstore.ExtendedMemoryRecord{}, false
		}
		return c.buildAvoidAPILessonRecord(event, now, desc, failureCount), true
	case "command_strategy":
		if desc.SubjectKey == "" || desc.Strategy == "" {
			return memstore.ExtendedMemoryRecord{}, false
		}
		if desc.Outcome == "success" {
			failedStrategies, err := c.relatedFailedCommandStrategies(ctx, strings.TrimSpace(event.SessionID), desc, now)
			if err != nil || len(failedStrategies) == 0 {
				return memstore.ExtendedMemoryRecord{}, false
			}
			return c.buildPreferredCommandStrategyLessonRecord(event, now, desc, failedStrategies), true
		}
		failureCount, err := c.countRepeatedCommandStrategyFailures(ctx, desc, now)
		if err != nil || failureCount < 2 {
			return memstore.ExtendedMemoryRecord{}, false
		}
		return c.buildAvoidCommandStrategyLessonRecord(event, now, desc, failureCount), true
	case "file_search_provider", "package_manager_provider", "database_tool_provider":
		if desc.SubjectKey == "" || desc.ProviderFamily == "" || desc.ProviderKey == "" {
			return memstore.ExtendedMemoryRecord{}, false
		}
		if desc.Outcome == "success" {
			failedProviders, err := c.relatedFailedProviders(ctx, strings.TrimSpace(event.SessionID), desc, now)
			if err != nil || len(failedProviders) == 0 {
				return memstore.ExtendedMemoryRecord{}, false
			}
			return c.buildPreferredProviderLessonRecord(event, now, desc, failedProviders), true
		}
		failureCount, err := c.countRepeatedProviderFailures(ctx, desc, now)
		if err != nil || failureCount < 2 {
			return memstore.ExtendedMemoryRecord{}, false
		}
		return c.buildAvoidProviderLessonRecord(event, now, desc, failureCount), true
	default:
		return memstore.ExtendedMemoryRecord{}, false
	}
}

func (c *automaticMemoryCapture) buildPreferredAPILessonRecord(event observe.ExecutionEvent, now time.Time, desc toolOutcomeDescriptor, failedHosts []string) memstore.ExtendedMemoryRecord {
	subject := firstNonEmpty(desc.SubjectDisplay, "similar")
	summary := fmt.Sprintf("For %s requests, prefer %s after %s failed.", subject, desc.Host, strings.Join(failedHosts, ", "))
	content := strings.Join([]string{
		"# Execution Lesson",
		"",
		fmt.Sprintf("Prefer %s for %s requests in this repo.", desc.Host, subject),
		"",
		"Evidence:",
		fmt.Sprintf("- Previous attempts with %s failed.", strings.Join(failedHosts, ", ")),
		fmt.Sprintf("- A later attempt with %s succeeded.", desc.Host),
	}, "\n")
	tags := []string{"auto", "execution-lesson", "api-host", sanitizePathSegment(desc.ToolName), desc.Host, "preferred"}
	tags = append(tags, failedHosts...)
	tags = append(tags, desc.SubjectTerms...)
	return memstore.ExtendedMemoryRecord{
		Path:            filepath.ToSlash(filepath.Join("auto", "execution_lessons", sanitizeIdentifierPart(desc.ToolName)+"-"+desc.SubjectKey+".md")),
		Content:         content,
		Summary:         summary,
		Tags:            memstore.DedupeStrings(compactNonEmpty(tags)),
		Scope:           memstore.MemoryScopeRepo,
		RepoID:          c.repoID,
		Kind:            autoMemoryExecutionLessonKind,
		Fingerprint:     "api_host:" + sanitizeIdentifierPart(desc.ToolName) + ":" + desc.SubjectKey,
		Confidence:      0.95,
		Stage:           memstore.MemoryStagePromoted,
		Status:          memstore.MemoryStatusActive,
		Workspace:       c.workspaceRoot,
		SourceKind:      autoMemoryExecutionLessonSource,
		SourceID:        firstNonEmpty(strings.TrimSpace(event.EventID), strings.TrimSpace(event.CallID)),
		SourcePath:      buildOutcomeSourcePath(event),
		SourceUpdatedAt: now,
		Metadata: map[string]any{
			"observation_type": "api_host",
			"tool_name":        desc.ToolName,
			"subject_key":      desc.SubjectKey,
			"subject_terms":    append([]string(nil), desc.SubjectTerms...),
			"preferred_host":   desc.Host,
			"failed_hosts":     append([]string(nil), failedHosts...),
		},
	}
}

func (c *automaticMemoryCapture) buildAvoidAPILessonRecord(event observe.ExecutionEvent, now time.Time, desc toolOutcomeDescriptor, failureCount int) memstore.ExtendedMemoryRecord {
	subject := firstNonEmpty(desc.SubjectDisplay, "similar")
	summary := fmt.Sprintf("Avoid %s by default for %s requests: repeated failures observed.", desc.Host, subject)
	content := strings.Join([]string{
		"# Execution Lesson",
		"",
		fmt.Sprintf("Avoid %s by default for %s requests in this repo.", desc.Host, subject),
		"",
		fmt.Sprintf("Evidence: %d recent failures were observed for this API host.", failureCount),
	}, "\n")
	tags := []string{"auto", "execution-lesson", "api-host", sanitizePathSegment(desc.ToolName), desc.Host, "avoid"}
	tags = append(tags, desc.SubjectTerms...)
	return memstore.ExtendedMemoryRecord{
		Path:            filepath.ToSlash(filepath.Join("auto", "execution_lessons", sanitizeIdentifierPart(desc.ToolName)+"-"+desc.SubjectKey+".md")),
		Content:         content,
		Summary:         summary,
		Tags:            memstore.DedupeStrings(compactNonEmpty(tags)),
		Scope:           memstore.MemoryScopeRepo,
		RepoID:          c.repoID,
		Kind:            autoMemoryExecutionLessonKind,
		Fingerprint:     "api_host:" + sanitizeIdentifierPart(desc.ToolName) + ":" + desc.SubjectKey,
		Confidence:      0.85,
		Stage:           memstore.MemoryStagePromoted,
		Status:          memstore.MemoryStatusActive,
		Workspace:       c.workspaceRoot,
		SourceKind:      autoMemoryExecutionLessonSource,
		SourceID:        firstNonEmpty(strings.TrimSpace(event.EventID), strings.TrimSpace(event.CallID)),
		SourcePath:      buildOutcomeSourcePath(event),
		SourceUpdatedAt: now,
		Metadata: map[string]any{
			"observation_type": "api_host",
			"tool_name":        desc.ToolName,
			"subject_key":      desc.SubjectKey,
			"subject_terms":    append([]string(nil), desc.SubjectTerms...),
			"avoid_host":       desc.Host,
			"failure_count":    failureCount,
		},
	}
}

func (c *automaticMemoryCapture) relatedFailedAPIHosts(ctx context.Context, sessionID string, desc toolOutcomeDescriptor, now time.Time) ([]string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	records, err := c.searchSessionOutcomeRecords(ctx, sessionID, now, 48)
	if err != nil {
		return nil, err
	}
	failedHosts := make([]string, 0, 4)
	for _, record := range records {
		if metadataStringValue(record.Metadata, "observation_type") != "api_host" {
			continue
		}
		if metadataStringValue(record.Metadata, "subject_key") != desc.SubjectKey {
			continue
		}
		if !strings.EqualFold(metadataStringValue(record.Metadata, "tool_name"), desc.ToolName) {
			continue
		}
		if metadataStringValue(record.Metadata, "outcome") != "failure" {
			continue
		}
		host := strings.ToLower(metadataStringValue(record.Metadata, "host"))
		if host == "" || strings.EqualFold(host, desc.Host) {
			continue
		}
		failedHosts = append(failedHosts, host)
	}
	return memstore.DedupeStrings(failedHosts), nil
}

func (c *automaticMemoryCapture) countRepeatedAPIFailures(ctx context.Context, desc toolOutcomeDescriptor, now time.Time) (int, error) {
	records, err := c.searchSessionOutcomeRecords(ctx, "", now, 128)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, record := range records {
		if metadataStringValue(record.Metadata, "observation_type") != "api_host" {
			continue
		}
		if metadataStringValue(record.Metadata, "subject_key") != desc.SubjectKey {
			continue
		}
		if !strings.EqualFold(metadataStringValue(record.Metadata, "tool_name"), desc.ToolName) {
			continue
		}
		if metadataStringValue(record.Metadata, "outcome") != "failure" {
			continue
		}
		if !strings.EqualFold(metadataStringValue(record.Metadata, "host"), desc.Host) {
			continue
		}
		count++
	}
	return count, nil
}

func (c *automaticMemoryCapture) searchSessionOutcomeRecords(ctx context.Context, sessionID string, now time.Time, limit int) ([]memstore.ExtendedMemoryRecord, error) {
	query := memstore.ExtendedMemoryQuery{
		Scopes:       []memstore.MemoryScope{memstore.MemoryScopeSession},
		Kinds:        []string{autoMemoryToolOutcomeKind},
		Stages:       []memstore.MemoryStage{memstore.MemoryStageSnapshot, memstore.MemoryStageManual, memstore.MemoryStageConsolidated, memstore.MemoryStagePromoted},
		Statuses:     []memstore.MemoryStatus{memstore.MemoryStatusActive},
		NotExpiredAt: now,
		SortBy:       memstore.MemorySortByUpdatedAt,
		Limit:        limit,
	}
	if strings.TrimSpace(sessionID) != "" {
		query.SessionID = strings.TrimSpace(sessionID)
	}
	if strings.TrimSpace(c.repoID) != "" {
		query.RepoID = strings.TrimSpace(c.repoID)
	}
	return c.store.SearchExtended(ctx, query)
}

func (c *automaticMemoryCapture) relatedFailedCommandStrategies(ctx context.Context, sessionID string, desc toolOutcomeDescriptor, now time.Time) ([]string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	records, err := c.searchSessionOutcomeRecords(ctx, sessionID, now, 48)
	if err != nil {
		return nil, err
	}
	failedStrategies := make([]string, 0, 4)
	for _, record := range records {
		if metadataStringValue(record.Metadata, "observation_type") != "command_strategy" {
			continue
		}
		if metadataStringValue(record.Metadata, "subject_key") != desc.SubjectKey {
			continue
		}
		if !strings.EqualFold(metadataStringValue(record.Metadata, "tool_name"), desc.ToolName) {
			continue
		}
		if metadataStringValue(record.Metadata, "outcome") != "failure" {
			continue
		}
		strategy := strings.ToLower(metadataStringValue(record.Metadata, "command_strategy"))
		if strategy == "" || strings.EqualFold(strategy, desc.Strategy) {
			continue
		}
		failedStrategies = append(failedStrategies, strategy)
	}
	return memstore.DedupeStrings(failedStrategies), nil
}

func (c *automaticMemoryCapture) countRepeatedCommandStrategyFailures(ctx context.Context, desc toolOutcomeDescriptor, now time.Time) (int, error) {
	records, err := c.searchSessionOutcomeRecords(ctx, "", now, 128)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, record := range records {
		if metadataStringValue(record.Metadata, "observation_type") != "command_strategy" {
			continue
		}
		if metadataStringValue(record.Metadata, "subject_key") != desc.SubjectKey {
			continue
		}
		if !strings.EqualFold(metadataStringValue(record.Metadata, "tool_name"), desc.ToolName) {
			continue
		}
		if metadataStringValue(record.Metadata, "outcome") != "failure" {
			continue
		}
		if !strings.EqualFold(metadataStringValue(record.Metadata, "command_strategy"), desc.Strategy) {
			continue
		}
		count++
	}
	return count, nil
}

func (c *automaticMemoryCapture) buildPreferredCommandStrategyLessonRecord(event observe.ExecutionEvent, now time.Time, desc toolOutcomeDescriptor, failedStrategies []string) memstore.ExtendedMemoryRecord {
	subject := firstNonEmpty(desc.SubjectDisplay, "similar")
	summary := fmt.Sprintf("For %s requests, prefer %s after %s failed.", subject, commandStrategyLabel(desc.Strategy), strings.Join(commandStrategyLabels(failedStrategies), ", "))
	content := strings.Join([]string{
		"# Execution Lesson",
		"",
		fmt.Sprintf("Prefer %s for %s requests in this environment.", commandStrategyLabel(desc.Strategy), subject),
		"",
		"Evidence:",
		fmt.Sprintf("- Previous attempts using %s failed.", strings.Join(commandStrategyLabels(failedStrategies), ", ")),
		fmt.Sprintf("- A later attempt using %s succeeded.", commandStrategyLabel(desc.Strategy)),
	}, "\n")
	tags := []string{"auto", "execution-lesson", "command-strategy", sanitizePathSegment(desc.ToolName), sanitizePathSegment(desc.Strategy), "preferred"}
	tags = append(tags, failedStrategies...)
	tags = append(tags, desc.SubjectTerms...)
	return memstore.ExtendedMemoryRecord{
		Path:            filepath.ToSlash(filepath.Join("auto", "execution_lessons", sanitizeIdentifierPart(desc.ToolName)+"-"+desc.SubjectKey+"-strategy.md")),
		Content:         content,
		Summary:         summary,
		Tags:            memstore.DedupeStrings(compactNonEmpty(tags)),
		Scope:           memstore.MemoryScopeRepo,
		RepoID:          c.repoID,
		Kind:            autoMemoryExecutionLessonKind,
		Fingerprint:     "command_strategy:" + sanitizeIdentifierPart(desc.ToolName) + ":" + desc.SubjectKey,
		Confidence:      0.95,
		Stage:           memstore.MemoryStagePromoted,
		Status:          memstore.MemoryStatusActive,
		Workspace:       c.workspaceRoot,
		SourceKind:      autoMemoryExecutionLessonSource,
		SourceID:        firstNonEmpty(strings.TrimSpace(event.EventID), strings.TrimSpace(event.CallID)),
		SourcePath:      buildOutcomeSourcePath(event),
		SourceUpdatedAt: now,
		Metadata: map[string]any{
			"observation_type":   "command_strategy",
			"tool_name":          desc.ToolName,
			"subject_key":        desc.SubjectKey,
			"subject_terms":      append([]string(nil), desc.SubjectTerms...),
			"preferred_strategy": desc.Strategy,
			"failed_strategies":  append([]string(nil), failedStrategies...),
		},
	}
}

func (c *automaticMemoryCapture) buildAvoidCommandStrategyLessonRecord(event observe.ExecutionEvent, now time.Time, desc toolOutcomeDescriptor, failureCount int) memstore.ExtendedMemoryRecord {
	subject := firstNonEmpty(desc.SubjectDisplay, "similar")
	summary := fmt.Sprintf("Avoid %s by default for %s requests: repeated failures observed.", commandStrategyLabel(desc.Strategy), subject)
	content := strings.Join([]string{
		"# Execution Lesson",
		"",
		fmt.Sprintf("Avoid %s by default for %s requests in this environment.", commandStrategyLabel(desc.Strategy), subject),
		"",
		fmt.Sprintf("Evidence: %d recent failures were observed for this command strategy.", failureCount),
	}, "\n")
	tags := []string{"auto", "execution-lesson", "command-strategy", sanitizePathSegment(desc.ToolName), sanitizePathSegment(desc.Strategy), "avoid"}
	tags = append(tags, desc.SubjectTerms...)
	return memstore.ExtendedMemoryRecord{
		Path:            filepath.ToSlash(filepath.Join("auto", "execution_lessons", sanitizeIdentifierPart(desc.ToolName)+"-"+desc.SubjectKey+"-strategy.md")),
		Content:         content,
		Summary:         summary,
		Tags:            memstore.DedupeStrings(compactNonEmpty(tags)),
		Scope:           memstore.MemoryScopeRepo,
		RepoID:          c.repoID,
		Kind:            autoMemoryExecutionLessonKind,
		Fingerprint:     "command_strategy:" + sanitizeIdentifierPart(desc.ToolName) + ":" + desc.SubjectKey,
		Confidence:      0.85,
		Stage:           memstore.MemoryStagePromoted,
		Status:          memstore.MemoryStatusActive,
		Workspace:       c.workspaceRoot,
		SourceKind:      autoMemoryExecutionLessonSource,
		SourceID:        firstNonEmpty(strings.TrimSpace(event.EventID), strings.TrimSpace(event.CallID)),
		SourcePath:      buildOutcomeSourcePath(event),
		SourceUpdatedAt: now,
		Metadata: map[string]any{
			"observation_type": "command_strategy",
			"tool_name":        desc.ToolName,
			"subject_key":      desc.SubjectKey,
			"subject_terms":    append([]string(nil), desc.SubjectTerms...),
			"avoid_strategy":   desc.Strategy,
			"failure_count":    failureCount,
		},
	}
}

func (c *automaticMemoryCapture) relatedFailedProviders(ctx context.Context, sessionID string, desc toolOutcomeDescriptor, now time.Time) ([]string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	records, err := c.searchSessionOutcomeRecords(ctx, sessionID, now, 48)
	if err != nil {
		return nil, err
	}
	failedProviders := make([]string, 0, 4)
	for _, record := range records {
		if metadataStringValue(record.Metadata, "observation_type") != desc.ObservationType {
			continue
		}
		if metadataStringValue(record.Metadata, "subject_key") != desc.SubjectKey {
			continue
		}
		if metadataStringValue(record.Metadata, "outcome") != "failure" {
			continue
		}
		providerKey := strings.ToLower(metadataStringValue(record.Metadata, "provider_key"))
		if providerKey == "" || strings.EqualFold(providerKey, desc.ProviderKey) {
			continue
		}
		failedProviders = append(failedProviders, providerKey)
	}
	return memstore.DedupeStrings(failedProviders), nil
}

func (c *automaticMemoryCapture) countRepeatedProviderFailures(ctx context.Context, desc toolOutcomeDescriptor, now time.Time) (int, error) {
	records, err := c.searchSessionOutcomeRecords(ctx, "", now, 128)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, record := range records {
		if metadataStringValue(record.Metadata, "observation_type") != desc.ObservationType {
			continue
		}
		if metadataStringValue(record.Metadata, "subject_key") != desc.SubjectKey {
			continue
		}
		if metadataStringValue(record.Metadata, "outcome") != "failure" {
			continue
		}
		if !strings.EqualFold(metadataStringValue(record.Metadata, "provider_key"), desc.ProviderKey) {
			continue
		}
		count++
	}
	return count, nil
}

func (c *automaticMemoryCapture) buildPreferredProviderLessonRecord(event observe.ExecutionEvent, now time.Time, desc toolOutcomeDescriptor, failedProviders []string) memstore.ExtendedMemoryRecord {
	subject := firstNonEmpty(desc.SubjectDisplay, "similar")
	summary := fmt.Sprintf("For %s requests, prefer %s after %s failed.", subject, desc.ProviderLabel, strings.Join(providerLabels(desc.ProviderFamily, failedProviders), ", "))
	content := strings.Join([]string{
		"# Execution Lesson",
		"",
		fmt.Sprintf("Prefer %s for %s requests in this repo.", desc.ProviderLabel, subject),
		"",
		"Evidence:",
		fmt.Sprintf("- Previous attempts using %s failed.", strings.Join(providerLabels(desc.ProviderFamily, failedProviders), ", ")),
		fmt.Sprintf("- A later attempt using %s succeeded.", desc.ProviderLabel),
	}, "\n")
	tags := []string{"auto", "execution-lesson", sanitizePathSegment(desc.ProviderFamily), sanitizePathSegment(desc.ProviderKey), "preferred"}
	tags = append(tags, failedProviders...)
	return memstore.ExtendedMemoryRecord{
		Path:            filepath.ToSlash(filepath.Join("auto", "execution_lessons", sanitizeIdentifierPart(desc.ObservationType)+"-"+sanitizeIdentifierPart(desc.SubjectKey)+".md")),
		Content:         content,
		Summary:         summary,
		Tags:            memstore.DedupeStrings(compactNonEmpty(tags)),
		Scope:           memstore.MemoryScopeRepo,
		RepoID:          c.repoID,
		Kind:            autoMemoryExecutionLessonKind,
		Fingerprint:     desc.ObservationType + ":" + sanitizeIdentifierPart(desc.SubjectKey),
		Confidence:      0.95,
		Stage:           memstore.MemoryStagePromoted,
		Status:          memstore.MemoryStatusActive,
		Workspace:       c.workspaceRoot,
		SourceKind:      autoMemoryExecutionLessonSource,
		SourceID:        firstNonEmpty(strings.TrimSpace(event.EventID), strings.TrimSpace(event.CallID)),
		SourcePath:      buildOutcomeSourcePath(event),
		SourceUpdatedAt: now,
		Metadata: map[string]any{
			"observation_type":   desc.ObservationType,
			"provider_family":    desc.ProviderFamily,
			"subject_key":        desc.SubjectKey,
			"preferred_provider": desc.ProviderKey,
			"failed_providers":   append([]string(nil), failedProviders...),
		},
	}
}

func (c *automaticMemoryCapture) buildAvoidProviderLessonRecord(event observe.ExecutionEvent, now time.Time, desc toolOutcomeDescriptor, failureCount int) memstore.ExtendedMemoryRecord {
	subject := firstNonEmpty(desc.SubjectDisplay, "similar")
	summary := fmt.Sprintf("Avoid %s by default for %s requests: repeated failures observed.", desc.ProviderLabel, subject)
	content := strings.Join([]string{
		"# Execution Lesson",
		"",
		fmt.Sprintf("Avoid %s by default for %s requests in this repo.", desc.ProviderLabel, subject),
		"",
		fmt.Sprintf("Evidence: %d recent failures were observed for this provider.", failureCount),
	}, "\n")
	tags := []string{"auto", "execution-lesson", sanitizePathSegment(desc.ProviderFamily), sanitizePathSegment(desc.ProviderKey), "avoid"}
	return memstore.ExtendedMemoryRecord{
		Path:            filepath.ToSlash(filepath.Join("auto", "execution_lessons", sanitizeIdentifierPart(desc.ObservationType)+"-"+sanitizeIdentifierPart(desc.SubjectKey)+".md")),
		Content:         content,
		Summary:         summary,
		Tags:            memstore.DedupeStrings(compactNonEmpty(tags)),
		Scope:           memstore.MemoryScopeRepo,
		RepoID:          c.repoID,
		Kind:            autoMemoryExecutionLessonKind,
		Fingerprint:     desc.ObservationType + ":" + sanitizeIdentifierPart(desc.SubjectKey),
		Confidence:      0.85,
		Stage:           memstore.MemoryStagePromoted,
		Status:          memstore.MemoryStatusActive,
		Workspace:       c.workspaceRoot,
		SourceKind:      autoMemoryExecutionLessonSource,
		SourceID:        firstNonEmpty(strings.TrimSpace(event.EventID), strings.TrimSpace(event.CallID)),
		SourcePath:      buildOutcomeSourcePath(event),
		SourceUpdatedAt: now,
		Metadata: map[string]any{
			"observation_type": desc.ObservationType,
			"provider_family":  desc.ProviderFamily,
			"subject_key":      desc.SubjectKey,
			"avoid_provider":   desc.ProviderKey,
			"failure_count":    failureCount,
		},
	}
}

func providerLabels(family string, keys []string) []string {
	labels := make([]string, 0, len(keys))
	for _, key := range keys {
		switch strings.TrimSpace(family) {
		case "file_search":
			labels = append(labels, fileSearchProviderLabel(key))
		case "package_manager":
			labels = append(labels, packageManagerLabel(key))
		case "database_tool":
			labels = append(labels, databaseToolLabel(key))
		default:
			labels = append(labels, strings.TrimSpace(key))
		}
	}
	return labels
}

func commandStrategyLabel(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "bash-style":
		return "bash-style commands"
	case "powershell", "powershell-style":
		return "PowerShell"
	case "cmd":
		return "cmd"
	case "structured":
		return "structured command args"
	case "shell-form":
		return "shell-form commands"
	default:
		if strings.TrimSpace(strategy) == "" {
			return "the current strategy"
		}
		return strings.TrimSpace(strategy)
	}
}

func commandStrategyLabels(strategies []string) []string {
	labels := make([]string, 0, len(strategies))
	for _, strategy := range strategies {
		labels = append(labels, commandStrategyLabel(strategy))
	}
	return labels
}

func recallQueryTerms(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	terms := make([]string, 0, 16)
	seen := make(map[string]struct{}, 16)
	add := func(term string) {
		term = strings.TrimSpace(term)
		if term == "" {
			return
		}
		key := strings.ToLower(term)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		terms = append(terms, term)
	}
	add(strings.ToLower(query))
	for _, token := range strings.FieldsFunc(strings.ToLower(query), isQueryDelimiter) {
		if len([]rune(token)) >= 2 {
			add(token)
		}
	}
	for _, span := range extractCJKTerms(query) {
		add(span)
	}
	for _, topic := range detectMemoryTopics(query, "", "") {
		add(topic.Canonical)
		for _, alias := range topic.Aliases {
			add(alias)
		}
	}
	return terms
}

func extractCJKTerms(text string) []string {
	runs := extractHanRuns(text)
	out := make([]string, 0, 12)
	for _, run := range runs {
		runes := []rune(run)
		if len(runes) >= 2 && len(runes) <= 8 {
			out = append(out, run)
		}
		for i := 0; i+1 < len(runes); i++ {
			out = append(out, string(runes[i:i+2]))
		}
	}
	return out
}

func extractHanRuns(text string) []string {
	runs := make([]string, 0, 4)
	current := make([]rune, 0, len(text))
	flush := func() {
		if len(current) > 0 {
			runs = append(runs, string(current))
			current = current[:0]
		}
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return runs
}

func isQueryDelimiter(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func (c *automaticMemoryCapture) toolContextKey(sessionID, callID string) string {
	return strings.TrimSpace(sessionID) + ":" + strings.TrimSpace(callID)
}

func (c *automaticMemoryCapture) popToolContext(sessionID, callID string) autoToolContext {
	ctxKey := c.toolContextKey(sessionID, callID)
	c.mu.Lock()
	defer c.mu.Unlock()
	info := c.toolContext[ctxKey]
	delete(c.toolContext, ctxKey)
	return info
}

func (c *automaticMemoryCapture) pruneLocked(deadline time.Time) {
	for key, info := range c.toolContext {
		if info.CapturedAt.Before(deadline) {
			delete(c.toolContext, key)
		}
	}
}

func latestUserMessageText(messages []model.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == model.RoleUser {
			return strings.TrimSpace(model.ContentPartsToPlainText(messages[i].ContentParts))
		}
	}
	return ""
}

func resolveWorkspaceRoot(ws workspace.Workspace) string {
	if ws == nil {
		return ""
	}
	root, err := ws.ResolvePath(".")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(root)
}

func currentMemoryUserID() string {
	userID := memstore.NormalizeMemoryIdentity(appconfig.AppDir())
	if userID != "" {
		return userID
	}
	return "default-user"
}

func firstToolURL(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	for _, key := range []string{"url", "uri", "source"} {
		if value, ok := payload[key]; ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				return text
			}
		}
	}
	if value, ok := payload["urls"].([]any); ok {
		for _, item := range value {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstToolCommand(input json.RawMessage) (string, []string) {
	if len(input) == 0 {
		return "", nil
	}
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return "", nil
	}
	command := ""
	if value, ok := payload["command"]; ok {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
			command = text
		}
	}
	var args []string
	if rawArgs, ok := payload["args"].([]any); ok {
		args = make([]string, 0, len(rawArgs))
		for _, arg := range rawArgs {
			if text := strings.TrimSpace(fmt.Sprint(arg)); text != "" && text != "<nil>" {
				args = append(args, text)
			}
		}
	}
	return command, args
}

func compactJSONPreview(input json.RawMessage) string {
	text := strings.TrimSpace(string(input))
	if text == "" {
		return ""
	}
	return compactText(text, 220)
}

func compactText(text string, maxLen int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	return strings.TrimSpace(text[:maxLen]) + "..."
}

func sanitizePathSegment(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func sanitizeIdentifierPart(value string) string {
	if part := sanitizePathSegment(value); part != "" {
		return part
	}
	return "unknown"
}

func compactNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func containsFold(text, term string) bool {
	text = strings.TrimSpace(text)
	term = strings.TrimSpace(term)
	if text == "" || term == "" {
		return false
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(term)) || strings.Contains(text, term)
}

func recallPaths(records []memstore.ExtendedMemoryRecord) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		if strings.TrimSpace(record.Path) != "" {
			out = append(out, record.Path)
		}
	}
	return out
}

func buildOutcomeSourcePath(event observe.ExecutionEvent) string {
	parts := []string{"execution_events"}
	if strings.TrimSpace(event.RunID) != "" {
		parts = append(parts, sanitizePathSegment(event.RunID))
	}
	if strings.TrimSpace(event.TurnID) != "" {
		parts = append(parts, sanitizePathSegment(event.TurnID))
	}
	name := firstNonEmpty(strings.TrimSpace(event.EventID), strings.TrimSpace(event.CallID), string(event.Type))
	parts = append(parts, sanitizePathSegment(name)+".json")
	return filepath.ToSlash(filepath.Join(parts...))
}

func eventURL(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(metadata["url"]))
}

func eventStatusCode(metadata map[string]any) int {
	if len(metadata) == 0 {
		return 0
	}
	switch value := metadata["status_code"].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func eventExitCode(metadata map[string]any) int {
	if len(metadata) == 0 {
		return 0
	}
	switch value := metadata["exit_code"].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func extractHost(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Host != "" {
		return strings.ToLower(parsed.Host)
	}
	return ""
}

func eventFailed(event observe.ExecutionEvent, statusCode int) bool {
	if event.Type == observe.ExecutionHostedToolFailed {
		return true
	}
	if strings.TrimSpace(event.Error) != "" {
		return true
	}
	if exitCode := eventExitCode(event.Metadata); exitCode != 0 {
		return true
	}
	if statusCode >= 400 {
		return true
	}
	if isError, ok := event.Metadata["is_error"].(bool); ok {
		return isError
	}
	if status := strings.ToLower(metadataStringValue(event.Metadata, "status")); status == "failed" || status == "error" || status == "errored" || status == "cancelled" || status == "canceled" || status == "incomplete" {
		return true
	}
	return false
}

func errorReason(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range []string{"reason", "reason_code", "details", "error_code", "stderr_preview"} {
		if value, ok := metadata[key]; ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				return text
			}
		}
	}
	return ""
}

func metadataStringValue(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	if value, ok := metadata[key]; ok {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}

func eventTimeOrNow(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now().UTC()
	}
	return ts.UTC()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
