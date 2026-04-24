package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	hswarm "github.com/mossagents/moss/harness/swarm"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

type runResult struct {
	RunID            string `json:"run_id"`
	RootSessionID    string `json:"root_session_id"`
	Status           string `json:"status"`
	FinalArtifact    string `json:"final_artifact,omitempty"`
	ThreadCount      int    `json:"thread_count"`
	TaskCount        int    `json:"task_count"`
	ArtifactCount    int    `json:"artifact_count"`
	ReviewAction     string `json:"review_action,omitempty"`
	ResolutionSource string `json:"resolution_source,omitempty"`
}

type roleRunResult struct {
	Output      string
	ToolCalls   int
	ToolResults int
}

type plannedQuestion struct {
	Slug     string `json:"slug"`
	Question string `json:"question"`
}

func startRunWorkflow(ctx context.Context, env *runtimeEnv, cfg *runCommandConfig) (*runResult, error) {
	if env == nil || env.Kernel == nil || env.Orchestrator == nil {
		return nil, fmt.Errorf("execution runtime is not initialized")
	}
	root, err := env.Kernel.NewSession(ctx, session.SessionConfig{
		Goal:     strings.TrimSpace(cfg.Topic),
		Mode:     "swarm",
		MaxSteps: 1,
	})
	if err != nil {
		return nil, err
	}
	seed, err := env.Orchestrator.Seed(hswarm.ResearchRunSeed{
		RunID:         strings.TrimSpace(cfg.RunID),
		Goal:          strings.TrimSpace(cfg.Topic),
		RootSessionID: root.ID,
		WorkspaceID:   strings.TrimSpace(cfg.AppFlags.Workspace),
	})
	if err != nil {
		return nil, err
	}
	root.Status = session.StatusRunning
	root.SetTitle("swarm: " + strings.TrimSpace(cfg.Topic))
	session.SetThreadSource(root, threadSourceExample)
	session.SetThreadSwarmRunID(root, seed.Run.ID)
	session.SetThreadRole(root, string(kswarm.RoleSupervisor))
	session.SetThreadPreview(root, strings.TrimSpace(cfg.Topic))
	root.SetMetadataBatch(map[string]any{
		metaRunStatus:     string(kswarm.RunRunning),
		metaEventsPartial: false,
		metaDegraded:      false,
		metaReportDetail:  string(cfg.Detail),
		metaReportAsOf:    cfg.AsOf.UTC().Format(time.RFC3339),
		metaWorkerCount:   cfg.Workers,
		metaLang:          cfg.Lang,
	})
	if err := env.SessionStore.Save(ctx, root); err != nil {
		return nil, err
	}
	roleSessions, err := createFixedRoleSessions(ctx, env, root, seed.Run.ID, strings.TrimSpace(cfg.Topic))
	if err != nil {
		return nil, err
	}
	root.SetMetadataBatch(map[string]any{
		metaPlannerSessionID:  roleSessions[string(kswarm.RolePlanner)].ID,
		metaSynthSessionID:    roleSessions[string(kswarm.RoleSynthesizer)].ID,
		metaReviewerSessionID: roleSessions[string(kswarm.RoleReviewer)].ID,
	})
	if err := env.SessionStore.Save(ctx, root); err != nil {
		return nil, err
	}
	initialTasks, err := remapSeedTasks(seed, env, root.ID, roleSessions, strings.TrimSpace(cfg.AppFlags.Workspace))
	if err != nil {
		return nil, err
	}
	for _, task := range initialTasks {
		if err := env.TaskWriter.UpsertTask(ctx, task); err != nil {
			return nil, err
		}
	}
	snapshot, err := env.Recovery.Load(ctx, resolvedTarget{
		RootSessionID:    root.ID,
		SwarmRunID:       seed.Run.ID,
		ResolutionSource: "run",
	})
	if err != nil {
		return nil, err
	}
	return continueRunWorkflow(ctx, env, snapshot, strings.TrimSpace(cfg.Topic), false)
}

func resumeRunWorkflow(ctx context.Context, env *runtimeEnv, target resolvedTarget, snapshot *RecoveredRunSnapshot, cfg *resumeCommandConfig) (*runResult, error) {
	if env == nil || snapshot == nil {
		return nil, fmt.Errorf("resume runtime is not initialized")
	}
	if strings.EqualFold(snapshot.Status, string(session.StatusCompleted)) {
		return nil, fmt.Errorf("swarm run %q is already completed; use inspect or export", snapshot.RunID)
	}
	if snapshot.Degraded && !cfg.ForceDegradedResume {
		return nil, fmt.Errorf("swarm run %q is degraded; rerun with --force-degraded-resume to continue", snapshot.RunID)
	}
	return continueRunWorkflow(ctx, env, snapshot, loadRunTopic(ctx, env, snapshot.RootSessionID), true)
}

func continueRunWorkflow(ctx context.Context, env *runtimeEnv, snapshot *RecoveredRunSnapshot, topic string, resumed bool) (*runResult, error) {
	root, err := loadWritableSession(ctx, env, snapshot.RootSessionID)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("root session %q not found", snapshot.RootSessionID)
	}
	root.Status = session.StatusRunning
	root.SetMetadata(metaRunStatus, string(kswarm.RunRunning))
	if err := env.SessionStore.Save(ctx, root); err != nil {
		return nil, err
	}
	detail := reportDetailFromSession(root)
	asOf := reportAsOfFromSession(root)
	workers := workerCountFromSession(root)
	lang := langFromSession(root)
	for {
		next, err := nextReadyTask(ctx, env, snapshot.RunID)
		if err != nil {
			return nil, err
		}
		if next == nil {
			break
		}
		next.Status = taskrt.TaskRunning
		next.ClaimedBy = "research-swarm-example"
		if err := env.TaskWriter.UpsertTask(ctx, *next); err != nil {
			return nil, err
		}
		if err := executeTask(ctx, env, root, next, topic, detail, asOf, workers, lang); err != nil {
			next.Status = taskrt.TaskFailed
			next.Error = err.Error()
			_ = env.TaskWriter.UpsertTask(ctx, *next)
			root.Status = session.StatusFailed
			root.SetMetadata(metaRunStatus, string(kswarm.RunFailed))
			_ = env.SessionStore.Save(ctx, root)
			return nil, err
		}
	}
	updated, err := env.Recovery.Load(ctx, resolvedTarget{
		RootSessionID:    root.ID,
		SwarmRunID:       snapshot.RunID,
		ResolutionSource: "workflow",
	})
	if err != nil {
		return nil, err
	}
	if updated.FinalArtifactName != "" && !strings.EqualFold(string(root.Status), string(session.StatusCompleted)) {
		root.Status = session.StatusCompleted
		root.SetMetadata(metaRunStatus, string(kswarm.RunCompleted))
		if err := env.SessionStore.Save(ctx, root); err != nil {
			return nil, err
		}
		updated.Status = string(session.StatusCompleted)
	}
	return &runResult{
		RunID:            updated.RunID,
		RootSessionID:    updated.RootSessionID,
		Status:           updated.Status,
		FinalArtifact:    updated.FinalArtifactName,
		ThreadCount:      len(updated.Snapshot.Threads),
		TaskCount:        len(updated.Snapshot.Tasks),
		ArtifactCount:    len(updated.Snapshot.Artifacts),
		ReviewAction:     string(latestGovernanceActions(updated.Snapshot.Messages)[metadataString(root, metaReviewTaskID, "")]),
		ResolutionSource: boolSource(resumed, "resume", "run"),
	}, nil
}

func executeTask(ctx context.Context, env *runtimeEnv, root *session.Session, task *taskrt.TaskRecord, topic string, detail reportDetail, asOf time.Time, workers int, lang string) error {
	role := roleForTask(env, task.AgentName)
	switch role {
	case kswarm.RolePlanner:
		return executePlannerTask(ctx, env, root, task, topic, detail, asOf, workers, lang)
	case kswarm.RoleSupervisor:
		return executeSupervisorTask(ctx, env, task, topic, asOf)
	case kswarm.RoleWorker:
		return executeWorkerTask(ctx, env, root, task, topic, detail, asOf, lang)
	case kswarm.RoleSynthesizer:
		return executeSynthTask(ctx, env, root, task, topic, detail, asOf, lang)
	case kswarm.RoleReviewer:
		return executeReviewTask(ctx, env, root, task, topic, detail, asOf, lang)
	default:
		task.Status = taskrt.TaskCompleted
		task.Result = "noop"
		return env.TaskWriter.UpsertTask(ctx, *task)
	}
}

func executePlannerTask(ctx context.Context, env *runtimeEnv, root *session.Session, task *taskrt.TaskRecord, topic string, detail reportDetail, asOf time.Time, workers int, lang string) error {
	thread, err := loadWritableSession(ctx, env, task.ThreadID)
	if err != nil {
		return err
	}
	if thread == nil {
		return fmt.Errorf("planner session %q not found", task.ThreadID)
	}
	questions, err := planQuestions(ctx, env, topic, detail, asOf, workers, lang)
	if err != nil {
		return err
	}
	planJSON, err := json.MarshalIndent(map[string]any{"topic": topic, "questions": questions}, "", "  ")
	if err != nil {
		return err
	}
	planText := string(planJSON)
	if _, err := publishArtifact(ctx, env, task.SwarmRunID, thread.ID, kswarm.ArtifactPlanFragment, task.ID, "plan-fragment.json", "application/json", []byte(planText), "Planner output"); err != nil {
		return err
	}
	synthID := metadataString(root, metaSynthSessionID, "")
	reviewerID := metadataString(root, metaReviewerSessionID, "")
	var workerTaskIDs []string
	for _, question := range questions {
		workerSession, err := createThreadSession(ctx, env, root, task.SwarmRunID, kswarm.RoleWorker, question.Question)
		if err != nil {
			return err
		}
		workerTask := env.Orchestrator.WorkerTask(task.SwarmRunID, workerSession.ID, question.Question, question.Question, nil)
		workerTask.SessionID = workerSession.ID
		workerTask.ParentSessionID = root.ID
		workerTask.WorkspaceID = task.WorkspaceID
		if err := env.TaskWriter.UpsertTask(ctx, workerTask); err != nil {
			return err
		}
		if _, err := env.MessageWriter.EnqueueTaskMessage(ctx, taskrt.TaskMessage{
			TaskID:       workerTask.ID,
			SwarmRunID:   task.SwarmRunID,
			ThreadID:     workerSession.ID,
			FromThreadID: task.ThreadID,
			ToThreadID:   workerSession.ID,
			Kind:         string(kswarm.MessageAssignment),
			Subject:      question.Question,
			Content:      question.Question,
			CreatedAt:    time.Now().UTC(),
		}); err != nil {
			return err
		}
		workerTaskIDs = append(workerTaskIDs, workerTask.ID)
	}
	synthTask := env.Orchestrator.SynthesisTask(task.SwarmRunID, synthID, "Produce the final report", topic, workerTaskIDs)
	synthTask.SessionID = synthID
	synthTask.ParentSessionID = root.ID
	synthTask.WorkspaceID = task.WorkspaceID
	if err := env.TaskWriter.UpsertTask(ctx, synthTask); err != nil {
		return err
	}
	reviewTask := env.Orchestrator.ReviewTask(task.SwarmRunID, reviewerID, "Review the final report", topic, []string{synthTask.ID})
	reviewTask.SessionID = reviewerID
	reviewTask.ParentSessionID = root.ID
	reviewTask.WorkspaceID = task.WorkspaceID
	if err := env.TaskWriter.UpsertTask(ctx, reviewTask); err != nil {
		return err
	}
	root.SetMetadata(metaSynthTaskID, synthTask.ID)
	root.SetMetadata(metaReviewTaskID, reviewTask.ID)
	if err := env.SessionStore.Save(ctx, root); err != nil {
		return err
	}
	task.Status = taskrt.TaskCompleted
	task.Result = fmt.Sprintf("planned %d worker tasks", len(workerTaskIDs))
	if err := completeThreadSession(ctx, env, thread); err != nil {
		return err
	}
	return env.TaskWriter.UpsertTask(ctx, *task)
}

func executeSupervisorTask(ctx context.Context, env *runtimeEnv, task *taskrt.TaskRecord, topic string, asOf time.Time) error {
	thread, err := loadWritableSession(ctx, env, task.ThreadID)
	if err != nil {
		return err
	}
	if thread == nil {
		return fmt.Errorf("supervisor session %q not found", task.ThreadID)
	}
	text := fmt.Sprintf("Supervisor checkpoint: run %s is executing topic %q as of %s with persisted swarm facts and resumable tasks.", task.SwarmRunID, topic, asOf.UTC().Format(time.RFC3339))
	if _, err := publishArtifact(ctx, env, task.SwarmRunID, thread.ID, kswarm.ArtifactSummary, task.ID, "supervisor-summary.md", "text/markdown", []byte(text), "Supervisor summary"); err != nil {
		return err
	}
	task.Status = taskrt.TaskCompleted
	task.Result = "supervisor summary published"
	if err := completeThreadSession(ctx, env, thread); err != nil {
		return err
	}
	return env.TaskWriter.UpsertTask(ctx, *task)
}

func executeWorkerTask(ctx context.Context, env *runtimeEnv, root *session.Session, task *taskrt.TaskRecord, topic string, detail reportDetail, asOf time.Time, lang string) error {
	thread, err := loadWritableSession(ctx, env, task.ThreadID)
	if err != nil {
		return err
	}
	if thread == nil {
		return fmt.Errorf("worker session %q not found", task.ThreadID)
	}
	question := task.Goal
	finding, sourceSet, confidence, err := buildWorkerOutput(ctx, env, thread, topic, question, detail, asOf, lang)
	if err != nil {
		return err
	}
	if _, err := publishArtifact(ctx, env, task.SwarmRunID, thread.ID, kswarm.ArtifactFinding, task.ID, artifactFileName("finding", task.ID, "md"), "text/markdown", []byte(finding), "Worker finding"); err != nil {
		return err
	}
	if _, err := publishArtifact(ctx, env, task.SwarmRunID, thread.ID, kswarm.ArtifactSourceSet, task.ID, artifactFileName("source-set", task.ID, "json"), "application/json", []byte(sourceSet), "Worker source set"); err != nil {
		return err
	}
	if _, err := publishArtifact(ctx, env, task.SwarmRunID, thread.ID, kswarm.ArtifactConfidenceNote, task.ID, artifactFileName("confidence", task.ID, "md"), "text/markdown", []byte(confidence), "Worker confidence note"); err != nil {
		return err
	}
	synthTaskID := metadataString(root, metaSynthTaskID, "")
	if synthTaskID != "" {
		if _, err := env.MessageWriter.EnqueueTaskMessage(ctx, taskrt.TaskMessage{
			TaskID:       synthTaskID,
			SwarmRunID:   task.SwarmRunID,
			ThreadID:     metadataString(root, metaSynthSessionID, ""),
			FromThreadID: task.ThreadID,
			ToThreadID:   metadataString(root, metaSynthSessionID, ""),
			Kind:         string(kswarm.MessageStatus),
			Subject:      question,
			Content:      finding,
			CreatedAt:    time.Now().UTC(),
		}); err != nil {
			return err
		}
	}
	task.Status = taskrt.TaskCompleted
	task.Result = "worker findings published"
	if err := completeThreadSession(ctx, env, thread); err != nil {
		return err
	}
	return env.TaskWriter.UpsertTask(ctx, *task)
}

func executeSynthTask(ctx context.Context, env *runtimeEnv, root *session.Session, task *taskrt.TaskRecord, topic string, detail reportDetail, asOf time.Time, lang string) error {
	thread, err := loadWritableSession(ctx, env, task.ThreadID)
	if err != nil {
		return err
	}
	if thread == nil {
		return fmt.Errorf("synthesizer session %q not found", task.ThreadID)
	}
	snapshot, err := env.Recovery.Load(ctx, resolvedTarget{
		RootSessionID:    root.ID,
		SwarmRunID:       task.SwarmRunID,
		ResolutionSource: "synth",
	})
	if err != nil {
		return err
	}
	findings := collectArtifactTexts(ctx, env, snapshot.Snapshot, kswarm.ArtifactFinding)
	sourceSets := collectArtifactTexts(ctx, env, snapshot.Snapshot, kswarm.ArtifactSourceSet)
	report, err := buildFinalReport(ctx, env, thread, topic, findings, sourceSets, detail, asOf, lang)
	if err != nil {
		return err
	}
	if _, err := publishArtifact(ctx, env, task.SwarmRunID, thread.ID, kswarm.ArtifactSynthesisDraft, task.ID, "synthesis-draft.md", "text/markdown", []byte(report), "Synthesis draft"); err != nil {
		return err
	}
	if _, err := publishArtifact(ctx, env, task.SwarmRunID, thread.ID, kswarm.ArtifactSummary, task.ID, "final-report.md", "text/markdown", []byte(report), "Final report"); err != nil {
		return err
	}
	root.SetMetadata(metaFinalArtifactName, "final-report.md")
	root.SetMetadata(metaFinalArtifactThread, thread.ID)
	if err := env.SessionStore.Save(ctx, root); err != nil {
		return err
	}
	reviewTaskID := metadataString(root, metaReviewTaskID, "")
	reviewerThreadID := metadataString(root, metaReviewerSessionID, "")
	if reviewTaskID != "" {
		message := env.Orchestrator.ReviewRequestMessage(task.SwarmRunID, task.ThreadID, reviewerThreadID, reviewTaskID, "Review final report", "Final report ready for review", "synthesis complete")
		if _, err := env.MessageWriter.EnqueueTaskMessage(ctx, message); err != nil {
			return err
		}
	}
	task.Status = taskrt.TaskCompleted
	task.Result = "final report published"
	if err := completeThreadSession(ctx, env, thread); err != nil {
		return err
	}
	return env.TaskWriter.UpsertTask(ctx, *task)
}

func executeReviewTask(ctx context.Context, env *runtimeEnv, root *session.Session, task *taskrt.TaskRecord, topic string, detail reportDetail, asOf time.Time, lang string) error {
	thread, err := loadWritableSession(ctx, env, task.ThreadID)
	if err != nil {
		return err
	}
	if thread == nil {
		return fmt.Errorf("reviewer session %q not found", task.ThreadID)
	}
	reportThreadID := metadataString(root, metaFinalArtifactThread, "")
	reportName := metadataString(root, metaFinalArtifactName, "")
	if reportThreadID == "" || reportName == "" {
		return fmt.Errorf("final report artifact is not available for review")
	}
	item, err := env.Artifacts.Load(ctx, reportThreadID, reportName, 0)
	if err != nil {
		return err
	}
	if item == nil {
		return fmt.Errorf("final report artifact %q not found", reportName)
	}
	reviewText, err := buildReview(ctx, env, thread, topic, string(item.Data), detail, asOf, lang)
	if err != nil {
		return err
	}
	if _, err := publishArtifact(ctx, env, task.SwarmRunID, thread.ID, kswarm.ArtifactConfidenceNote, task.ID, "review-note.md", "text/markdown", []byte(reviewText), "Review note"); err != nil {
		return err
	}
	message := taskrt.TaskMessage{
		TaskID:       task.ID,
		SwarmRunID:   task.SwarmRunID,
		ThreadID:     thread.ID,
		FromThreadID: thread.ID,
		ToThreadID:   reportThreadID,
		Kind:         string(kswarm.MessageStatus),
		Subject:      "Review outcome",
		Content:      reviewText,
		Metadata:     kswarm.GovernanceMetadata(kswarm.GovernanceApproved, "review accepted", nil),
		CreatedAt:    time.Now().UTC(),
	}
	if _, err := env.MessageWriter.EnqueueTaskMessage(ctx, message); err != nil {
		return err
	}
	task.Status = taskrt.TaskCompleted
	task.Result = "approved"
	if err := env.TaskWriter.UpsertTask(ctx, *task); err != nil {
		return err
	}
	if err := completeThreadSession(ctx, env, thread); err != nil {
		return err
	}
	root.Status = session.StatusCompleted
	root.SetMetadata(metaRunStatus, string(kswarm.RunCompleted))
	return env.SessionStore.Save(ctx, root)
}

func createFixedRoleSessions(ctx context.Context, env *runtimeEnv, root *session.Session, runID, topic string) (map[string]*session.Session, error) {
	out := make(map[string]*session.Session, 3)
	for _, role := range []kswarm.Role{kswarm.RolePlanner, kswarm.RoleSynthesizer, kswarm.RoleReviewer} {
		sess, err := createThreadSession(ctx, env, root, runID, role, topic)
		if err != nil {
			return nil, err
		}
		out[string(role)] = sess
	}
	return out, nil
}

func createThreadSession(ctx context.Context, env *runtimeEnv, root *session.Session, runID string, role kswarm.Role, goal string) (*session.Session, error) {
	spec := roleSpecForRole(env, role)
	maxSteps := 12
	systemPrompt := ""
	if spec != nil {
		maxSteps = spec.MaxSteps
		systemPrompt = spec.SystemPrompt
	}
	sess, err := env.Kernel.NewSession(ctx, session.SessionConfig{
		Goal:         goal,
		Mode:         "swarm",
		MaxSteps:     maxSteps,
		SystemPrompt: systemPrompt,
	})
	if err != nil {
		return nil, err
	}
	sess.Status = session.StatusRunning
	sess.SetTitle(string(role) + ": " + goal)
	session.SetThreadSource(sess, threadSourceExample)
	session.SetThreadParent(sess, root.ID)
	session.SetThreadSwarmRunID(sess, runID)
	session.SetThreadRole(sess, string(role))
	session.SetThreadPreview(sess, goal)
	if err := env.SessionStore.Save(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func remapSeedTasks(seed *hswarm.ResearchSeed, env *runtimeEnv, rootID string, roleSessions map[string]*session.Session, workspace string) ([]taskrt.TaskRecord, error) {
	roleByAgent := make(map[string]kswarm.Role)
	for _, spec := range env.Swarm.RolePack() {
		roleByAgent[spec.AgentName] = spec.Protocol.Role
	}
	out := make([]taskrt.TaskRecord, 0, len(seed.Tasks))
	var plannerTaskID string
	for _, item := range seed.Tasks {
		role, ok := roleByAgent[item.AgentName]
		if !ok {
			return nil, fmt.Errorf("unknown seed agent %q", item.AgentName)
		}
		task := item
		switch role {
		case kswarm.RolePlanner:
			task.ThreadID = roleSessions[string(role)].ID
			task.SessionID = roleSessions[string(role)].ID
			task.ParentSessionID = rootID
			plannerTaskID = task.ID
		case kswarm.RoleSupervisor:
			task.ThreadID = rootID
			task.SessionID = rootID
			task.ParentSessionID = ""
			task.DependsOn = []string{plannerTaskID}
		default:
			continue
		}
		task.WorkspaceID = workspace
		task.Status = taskrt.TaskPending
		out = append(out, task)
	}
	return out, nil
}

func nextReadyTask(ctx context.Context, env *runtimeEnv, runID string) (*taskrt.TaskRecord, error) {
	records, err := env.Tasks.ListTasks(ctx, taskrt.TaskQuery{SwarmRunID: runID})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]taskrt.TaskRecord, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	for i := range records {
		record := records[i]
		if record.Status != taskrt.TaskPending {
			continue
		}
		ready := true
		for _, depID := range record.DependsOn {
			dep, ok := byID[depID]
			if !ok || dep.Status != taskrt.TaskCompleted {
				ready = false
				break
			}
		}
		if ready {
			cp := record
			return &cp, nil
		}
	}
	return nil, nil
}

func planQuestions(ctx context.Context, env *runtimeEnv, topic string, detail reportDetail, asOf time.Time, workers int, lang string) ([]plannedQuestion, error) {
	prompt := fmt.Sprintf(`Return a JSON array with exactly %d objects. Each object must contain "slug" and "question".

Topic: %s
Report detail: %s
As of: %s

Requirements:
- Cover distinct research angles: market drivers, opposing risks, practical execution, and (for larger counts) competitive landscape, economics, adoption patterns, regulatory factors, or integration challenges.
- Questions should be researchable with current evidence, not generic.
- Return valid JSON only.%s`, workers, topic, detail, asOf.UTC().Format(time.RFC3339), langInstruction(lang))
	text, err := completeText(ctx, env.Kernel.LLM(),
		"You are a research planner. Return valid JSON only.",
		prompt,
	)
	if err != nil {
		return nil, fmt.Errorf("planner LLM call failed: %w", err)
	}
	var questions []plannedQuestion
	if parseErr := json.Unmarshal([]byte(strings.TrimSpace(text)), &questions); parseErr != nil {
		return nil, fmt.Errorf("planner returned invalid JSON: %w", parseErr)
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("planner returned empty questions list")
	}
	return questions, nil
}

func buildWorkerOutput(ctx context.Context, env *runtimeEnv, thread *session.Session, topic, question string, detail reportDetail, asOf time.Time, lang string) (string, string, string, error) {
	prompt := fmt.Sprintf(`Research topic: %s
Assigned question: %s
Report detail: %s
As of: %s

You must gather current evidence before answering.

Requirements:
1. Call the built-in datetime tool to confirm the current reference time.
2. Use at least one external retrieval capability before answering:
   - http_request for public web pages or APIs
   - run_command for local CLI retrieval when appropriate
3. If a relevant skill is available in the workspace/runtime, use it instead of freeform guessing.
4. Cite dates explicitly and surface stale/uncertain evidence.
5. Return exactly this structure:

## Finding
<markdown analysis with concrete claims, dated evidence, and explicit trade-offs>

## Sources
[
  {
    "source": "...",
    "summary": "...",
    "published_at": "YYYY-MM-DD or unknown",
    "retrieved_at": "RFC3339",
    "evidence": "specific datapoint or quote"
  }
]

## Confidence
<short confidence note explaining freshness, coverage, and gaps>

Do not omit any section.%s`, topic, question, detail, asOf.UTC().Format(time.RFC3339), langInstruction(lang))
	roleRun, err := runRoleAgent(ctx, env, thread, kswarm.RoleWorker, prompt, detail, asOf)
	if err != nil {
		return "", "", "", err
	}
	if roleRun.ToolCalls+roleRun.ToolResults == 0 {
		return "", "", "", fmt.Errorf("worker thread %q produced no tool activity; current-data research requires tool usage", thread.ID)
	}
	finding, sourceSet, confidence, err := parseWorkerOutput(roleRun.Output)
	if err != nil {
		return "", "", "", err
	}
	return finding, sourceSet, confidence, nil
}

func buildFinalReport(ctx context.Context, env *runtimeEnv, thread *session.Session, topic string, findings, sourceSets []string, detail reportDetail, asOf time.Time, lang string) (string, error) {
	prompt := fmt.Sprintf(`Topic: %s
As of: %s
Detail level: %s

Worker findings:
%s

Worker source sets:
%s

Write a markdown final report that is evidence-backed and current.

Requirements:
- Include an explicit "As of" section near the top.
- Every major claim must be tied to dated evidence from the worker source sets.
- Explain both the supporting case and the strongest counterarguments.
- End with concrete recommendations and risk triggers.
- For detail=brief: keep it concise but still evidence-backed.
- For detail=standard: provide a balanced report with evidence tables and trade-offs.
- For detail=comprehensive: produce a long-form report with detailed arguments, counterarguments, evidence ledger, and operational recommendations.
- Markdown only.%s`, topic, asOf.UTC().Format(time.RFC3339), detail, strings.Join(findings, "\n\n---\n\n"), strings.Join(sourceSets, "\n\n"), langInstruction(lang))
	roleRun, err := runRoleAgent(ctx, env, thread, kswarm.RoleSynthesizer, prompt, detail, asOf)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(roleRun.Output), nil
}

func buildReview(ctx context.Context, env *runtimeEnv, thread *session.Session, topic, report string, detail reportDetail, asOf time.Time, lang string) (string, error) {
	prompt := fmt.Sprintf(`Topic: %s
As of: %s
Detail level: %s

Final report:
%s

Review this report for unsupported claims, missing dated evidence, overconfidence, and stale assumptions.
Return a short markdown review note that explicitly states approval status and any gaps.%s`, topic, asOf.UTC().Format(time.RFC3339), detail, report, langInstruction(lang))
	roleRun, err := runRoleAgent(ctx, env, thread, kswarm.RoleReviewer, prompt, detail, asOf)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(roleRun.Output), nil
}

func publishArtifact(ctx context.Context, env *runtimeEnv, runID, sessionID string, kind kswarm.ArtifactKind, taskID, name, mime string, data []byte, summary string) (*artifact.Artifact, error) {
	item := &artifact.Artifact{Name: name, MIMEType: mime, Data: data}
	kswarm.StampArtifact(item, kswarm.ArtifactRef{
		RunID:    runID,
		ThreadID: sessionID,
		TaskID:   taskID,
		Kind:     kind,
		Name:     name,
		MIMEType: mime,
		Summary:  summary,
	})
	if err := env.Artifacts.Save(ctx, sessionID, item); err != nil {
		return nil, err
	}
	return item, nil
}

func collectArtifactTexts(ctx context.Context, env *runtimeEnv, snapshot *kswarm.Snapshot, kind kswarm.ArtifactKind) []string {
	if env == nil || snapshot == nil {
		return nil
	}
	var out []string
	for _, ref := range snapshot.Artifacts {
		if ref.Kind != kind {
			continue
		}
		item, err := env.Artifacts.Load(ctx, ref.SessionID, ref.Name, ref.Version)
		if err != nil || item == nil {
			continue
		}
		out = append(out, string(item.Data))
	}
	return out
}

func roleForTask(env *runtimeEnv, agentName string) kswarm.Role {
	if env == nil || env.Swarm == nil {
		return ""
	}
	for _, spec := range env.Swarm.RolePack() {
		if spec.AgentName == agentName {
			return spec.Protocol.Role
		}
	}
	return ""
}

func completeText(ctx context.Context, llm model.LLM, systemPrompt, userPrompt string) (string, error) {
	if llm == nil {
		return "", fmt.Errorf("llm is required")
	}
	var sb strings.Builder
	for chunk, err := range llm.GenerateContent(ctx, model.CompletionRequest{
		Messages: []model.Message{
			{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart(systemPrompt)}},
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(userPrompt)}},
		},
	}) {
		if err != nil {
			return sb.String(), err
		}
		if chunk.Type == model.StreamChunkTextDelta {
			sb.WriteString(chunk.Content)
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

func loadRunTopic(ctx context.Context, env *runtimeEnv, sessionID string) string {
	if env == nil {
		return ""
	}
	sess, err := env.SessionStore.Load(ctx, sessionID)
	if err != nil || sess == nil {
		return ""
	}
	return strings.TrimSpace(sess.Config.Goal)
}

func loadWritableSession(ctx context.Context, env *runtimeEnv, sessionID string) (*session.Session, error) {
	if env != nil && env.Kernel != nil {
		if sess, ok := env.Kernel.SessionManager().Get(sessionID); ok && sess != nil {
			return sess, nil
		}
	}
	return env.SessionStore.Load(ctx, sessionID)
}

func completeThreadSession(ctx context.Context, env *runtimeEnv, sess *session.Session) error {
	if sess == nil {
		return nil
	}
	sess.Status = session.StatusCompleted
	if sess.EndedAt.IsZero() {
		sess.EndedAt = time.Now().UTC()
	}
	return env.SessionStore.Save(ctx, sess)
}

func artifactFileName(prefix, taskID, ext string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		taskID = "artifact"
	}
	return fmt.Sprintf("%s-%s.%s", prefix, taskID, ext)
}

func boolSource(flag bool, yes, no string) string {
	if flag {
		return yes
	}
	return no
}

func reportDetailFromSession(root *session.Session) reportDetail {
	detail, err := parseReportDetail(metadataString(root, metaReportDetail, string(detailComprehensive)))
	if err != nil {
		return detailComprehensive
	}
	return detail
}

func reportAsOfFromSession(root *session.Session) time.Time {
	raw := metadataString(root, metaReportAsOf, "")
	if raw == "" {
		return time.Now().UTC()
	}
	asOf, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Now().UTC()
	}
	return asOf.UTC()
}

func workerCountFromSession(root *session.Session) int {
	if root == nil {
		return 3
	}
	raw, ok := root.GetMetadata(metaWorkerCount)
	if !ok {
		return 3
	}
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return v
		}
	case float64:
		if n := int(v); n > 0 {
			return n
		}
	}
	return 3
}

func langFromSession(root *session.Session) string {
	return metadataString(root, metaLang, "")
}

func langInstruction(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return ""
	}
	return fmt.Sprintf("\n\nWrite all output in %s.", lang)
}

func roleSpecForRole(env *runtimeEnv, role kswarm.Role) *hswarm.RoleSpec {
	if env == nil || env.Swarm == nil {
		return nil
	}
	for _, spec := range env.Swarm.RolePack() {
		if spec.Protocol.Role == role {
			cp := spec
			return &cp
		}
	}
	return nil
}

func runRoleAgent(ctx context.Context, env *runtimeEnv, thread *session.Session, role kswarm.Role, userPrompt string, detail reportDetail, asOf time.Time) (*roleRunResult, error) {
	if env == nil || env.Kernel == nil {
		return nil, fmt.Errorf("kernel runtime is not initialized")
	}
	if thread == nil {
		return nil, fmt.Errorf("thread session is required")
	}
	spec := roleSpecForRole(env, role)
	if spec == nil {
		return nil, fmt.Errorf("role spec %q not found", role)
	}
	systemPrompt := composeRoleSystemPrompt(*spec, detail, asOf)
	thread.Config.MaxSteps = spec.MaxSteps
	thread.Config.SystemPrompt = systemPrompt
	thread.UpdateSystemPrompt(systemPrompt)
	before := len(thread.CopyMessages())
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(userPrompt)}}
	thread.AppendMessage(userMsg)
	result, err := kernel.CollectRunAgentResult(ctx, env.Kernel, kernel.RunAgentRequest{
		Session:     thread,
		Agent:       env.Kernel.BuildLLMAgent(appName),
		UserContent: &userMsg,
		IO:          env.Kernel.UserIO(),
	})
	if err != nil {
		return nil, err
	}
	if err := env.SessionStore.Save(ctx, thread); err != nil {
		return nil, err
	}
	calls, toolResults := countToolUsageSince(thread.CopyMessages(), before)
	return &roleRunResult{Output: strings.TrimSpace(result.Output), ToolCalls: calls, ToolResults: toolResults}, nil
}

func composeRoleSystemPrompt(spec hswarm.RoleSpec, detail reportDetail, asOf time.Time) string {
	return strings.TrimSpace(fmt.Sprintf(`%s

You are running inside research-swarm, a research-first swarm example built on durable session/task/message/artifact facts.

Current reference time: %s
Requested report detail: %s

When the user asks for current or market-sensitive information, prefer tool use over memory. If relevant runtime skills are available, use them. Do not present stale claims as current.`, spec.SystemPrompt, asOf.UTC().Format(time.RFC3339), detail))
}

func countToolUsageSince(messages []model.Message, start int) (int, int) {
	if start < 0 {
		start = 0
	}
	var calls int
	var results int
	for i := start; i < len(messages); i++ {
		calls += len(messages[i].ToolCalls)
		results += len(messages[i].ToolResults)
	}
	return calls, results
}

func parseWorkerOutput(raw string) (string, string, string, error) {
	finding, err := extractSection(raw, "## Finding", "## Sources")
	if err != nil {
		return "", "", "", err
	}
	sourceSet, err := extractSection(raw, "## Sources", "## Confidence")
	if err != nil {
		return "", "", "", err
	}
	confidence, err := extractSection(raw, "## Confidence", "")
	if err != nil {
		return "", "", "", err
	}
	sourceSet = strings.TrimSpace(sourceSet)
	sourceSet = strings.TrimPrefix(sourceSet, "```json")
	sourceSet = strings.TrimPrefix(sourceSet, "```")
	sourceSet = strings.TrimSuffix(sourceSet, "```")
	sourceSet = strings.TrimSpace(sourceSet)
	return finding, sourceSet, confidence, nil
}

func extractSection(raw, startMarker, endMarker string) (string, error) {
	start := strings.Index(raw, startMarker)
	if start < 0 {
		return "", fmt.Errorf("missing section %q", startMarker)
	}
	start += len(startMarker)
	rest := raw[start:]
	end := len(rest)
	if endMarker != "" {
		idx := strings.Index(rest, endMarker)
		if idx < 0 {
			return "", fmt.Errorf("missing section %q", endMarker)
		}
		end = idx
	}
	section := strings.TrimSpace(rest[:end])
	if section == "" {
		return "", fmt.Errorf("empty section %q", startMarker)
	}
	return section, nil
}
