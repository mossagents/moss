package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/appkit"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/scheduler"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ChatService 桥接 moss kernel 与桌面前端。
type ChatService struct {
	cfg   config
	k     *kernel.Kernel
	sched *scheduler.Scheduler
	store session.SessionStore
	sess  *session.Session

	wailsIO *WailsUserIO

	mu            sync.Mutex
	running       bool
	cancel        context.CancelFunc
	monitorCancel context.CancelFunc
}

func NewChatService(cfg config) *ChatService {
	return &ChatService{cfg: cfg}
}

func (s *ChatService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	slog.Info("ChatService starting up...")
	s.wailsIO = NewWailsUserIO()

	k, err := s.buildKernel()
	if err != nil {
		return fmt.Errorf("build kernel: %w", err)
	}
	if err := k.Boot(ctx); err != nil {
		return fmt.Errorf("boot kernel: %w", err)
	}
	s.k = k

	sess, err := k.NewSession(ctx, s.newSessionConfig())
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	s.sess = sess
	if err := s.persistSession(sess); err != nil {
		slog.Warn("persist startup session failed", slog.Any("error", err))
	}

	s.startScheduler(ctx)
	s.startDashboardMonitor(ctx)
	s.emitDashboard()

	slog.Info("ChatService started",
		slog.String("provider", s.cfg.provider),
		slog.String("model", s.cfg.model),
		slog.String("workspace", resolveWorkspace(s.cfg.workspace)),
	)
	return nil
}

func (s *ChatService) ServiceShutdown() error {
	slog.Info("ChatService shutting down...")

	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	monitorCancel := s.monitorCancel
	s.monitorCancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if monitorCancel != nil {
		monitorCancel()
	}

	if s.sched != nil {
		s.sched.Stop()
	}

	if s.k != nil {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := s.k.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("kernel shutdown returned error", slog.Any("error", err))
		}
	}

	slog.Info("ChatService shutdown complete")
	return nil
}

func (s *ChatService) SendMessage(content string) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("agent is already running")
	}
	if s.sess == nil || s.k == nil {
		s.mu.Unlock()
		return fmt.Errorf("service not initialized")
	}
	s.mu.Unlock()

	if strings.HasPrefix(trimmed, "/") {
		output, err := s.handleSlashCommand(trimmed)
		if err != nil {
			emitEvent("chat:error", map[string]any{"message": err.Error()})
		} else if strings.TrimSpace(output) != "" {
			emitEvent("chat:text", map[string]any{"content": output})
		}
		s.mu.Lock()
		sessID := ""
		if s.sess != nil {
			sessID = s.sess.ID
		}
		s.mu.Unlock()
		emitEvent("chat:done", map[string]any{
			"session_id":  sessID,
			"steps":       0,
			"tokens_used": 0,
			"output":      "",
		})
		s.emitDashboard()
		return nil
	}

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	s.sess.AppendMessage(port.Message{Role: port.RoleUser, Content: content})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				emitEvent("chat:error", map[string]any{"message": fmt.Sprintf("internal error: %v", r)})
			}
			if err := s.persistSession(s.sess); err != nil {
				slog.Debug("persist session after run failed", slog.Any("error", err))
			}
			s.mu.Lock()
			s.running = false
			s.cancel = nil
			s.mu.Unlock()
			s.emitDashboard()
		}()

		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		s.cancel = cancel
		s.mu.Unlock()
		defer cancel()

		result, err := s.k.RunWithUserIO(ctx, s.sess, s.wailsIO)
		if err != nil {
			if ctx.Err() != nil {
				emitEvent("chat:cancelled", map[string]any{"message": "已取消"})
				return
			}
			emitEvent("chat:error", map[string]any{"message": err.Error()})
			return
		}

		emitEvent("chat:done", map[string]any{
			"session_id":  result.SessionID,
			"steps":       result.Steps,
			"tokens_used": result.TokensUsed.TotalTokens,
			"output":      result.Output,
		})
	}()

	return nil
}

func (s *ChatService) SendMessageWithAttachments(content string, attachments []string) error {
	if len(attachments) > 0 {
		content += "\n\n📎 Attached files:\n"
		for _, path := range attachments {
			content += fmt.Sprintf("- %s\n", path)
		}
	}
	return s.SendMessage(content)
}

func (s *ChatService) StopAgent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *ChatService) RespondToAsk(value string, approved bool) {
	s.wailsIO.RespondToAsk(port.InputResponse{Value: value, Approved: approved})
}

func (s *ChatService) NewSession() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("cannot create session while agent is running")
	}
	s.mu.Unlock()

	ctx := application.Get().Context()
	sess, err := s.k.NewSession(ctx, s.newSessionConfig())
	if err != nil {
		return err
	}
	s.sess = sess
	if err := s.persistSession(sess); err != nil {
		slog.Warn("persist new session failed", slog.Any("error", err))
	}
	s.emitDashboard()
	return nil
}

func (s *ChatService) ResumeSession(id string) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("cannot resume session while agent is running")
	}
	s.mu.Unlock()

	if s.store == nil {
		return fmt.Errorf("session store is not configured")
	}
	sess, err := s.store.Load(context.Background(), id)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	if sess == nil {
		return fmt.Errorf("session %q not found", id)
	}
	s.sess = sess
	if err := s.persistSession(sess); err != nil {
		slog.Warn("persist resumed session failed", slog.Any("error", err))
	}
	s.emitDashboard()
	return nil
}

func (s *ChatService) GetDashboard() (map[string]any, error) {
	return s.dashboardSnapshot(context.Background())
}

func (s *ChatService) GetConfig() map[string]any {
	return map[string]any{
		"provider":  s.cfg.provider,
		"model":     s.cfg.model,
		"workspace": resolveWorkspace(s.cfg.workspace),
		"trust":     s.cfg.trust,
		"baseURL":   s.cfg.baseURL,
		"workers":   s.cfg.workers,
	}
}

func (s *ChatService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *ChatService) newSessionConfig() session.SessionConfig {
	return session.SessionConfig{
		Goal:         "interactive desktop assistant",
		Mode:         "desktop",
		TrustLevel:   s.cfg.trust,
		SystemPrompt: buildManagerPrompt(resolveWorkspace(s.cfg.workspace), s.cfg.workers),
		MaxSteps:     120,
	}
}

func (s *ChatService) buildKernel() (*kernel.Kernel, error) {
	ctx := context.Background()
	appDir := appconfig.AppDir()
	sessionDir := filepath.Join(appDir, "sessions")
	store, err := session.NewFileStore(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("create session store: %w", err)
	}
	s.store = store

	sched := s.newScheduler(appDir)
	s.sched = sched

	flags := &appkit.AppFlags{
		Provider:  s.cfg.provider,
		Model:     s.cfg.model,
		Workspace: s.cfg.workspace,
		Trust:     s.cfg.trust,
		APIKey:    s.cfg.apiKey,
		BaseURL:   s.cfg.baseURL,
	}
	deepCfg := appkit.DefaultDeepAgentConfig()
	deepCfg.AppName = appconfig.AppName()
	deepCfg.EnableSessionStore = boolRef(true)
	deepCfg.EnablePersistentMemories = boolRef(true)
	deepCfg.EnableContextOffload = boolRef(true)
	deepCfg.EnableBootstrapContext = boolRef(true)
	deepCfg.SessionStoreDir = sessionDir
	deepCfg.MemoryDir = filepath.Join(appDir, "memories")
	deepCfg.AdditionalAppExtensions = []appkit.Extension{
		appkit.WithScheduling(sched),
	}

	k, err := appkit.BuildDeepAgentKernel(ctx, flags, s.wailsIO, &deepCfg)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (s *ChatService) newScheduler(appDir string) *scheduler.Scheduler {
	if appDir == "" {
		return scheduler.New()
	}
	jobsPath := filepath.Join(appDir, "schedules", "jobs.json")
	store, err := scheduler.NewFileJobStore(jobsPath)
	if err != nil {
		slog.Warn("scheduler persistence disabled", slog.Any("error", err))
		return scheduler.New()
	}
	return scheduler.New(scheduler.WithPersistence(store))
}

func (s *ChatService) startScheduler(ctx context.Context) {
	if s.sched == nil {
		return
	}
	s.sched.Start(ctx, func(_ context.Context, job scheduler.Job) {
		message := strings.TrimSpace(job.Goal)
		if message == "" {
			message = fmt.Sprintf("定时任务 %s 已触发", job.ID)
		}
		emitEvent("chat:text", map[string]any{
			"content": fmt.Sprintf("⏰ 定时提醒：%s", message),
		})
		emitEvent("chat:reminder", map[string]any{
			"id":      job.ID,
			"goal":    job.Goal,
			"content": message,
		})
		s.emitDashboard()
	})
}

func (s *ChatService) startDashboardMonitor(ctx context.Context) {
	s.mu.Lock()
	if s.monitorCancel != nil {
		s.monitorCancel()
	}
	mctx, cancel := context.WithCancel(ctx)
	s.monitorCancel = cancel
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		s.emitDashboard()
		for {
			select {
			case <-mctx.Done():
				return
			case <-ticker.C:
				s.emitDashboard()
			}
		}
	}()
}

func (s *ChatService) emitDashboard() {
	snapshot, err := s.dashboardSnapshot(context.Background())
	if err != nil {
		slog.Debug("dashboard snapshot failed", slog.Any("error", err))
		return
	}
	emitEvent("desktop:dashboard", snapshot)
	if worker, ok := snapshot["worker"]; ok {
		emitEvent("worker:update", worker)
	}
	if sessions, ok := snapshot["sessions"]; ok {
		emitEvent("desktop:sessions", sessions)
	}
	if schedules, ok := snapshot["schedules"]; ok {
		emitEvent("desktop:schedules", schedules)
	}
}

type sessionSummaryView struct {
	session.SessionSummary
	Current bool `json:"current"`
}

type scheduleView struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	Goal     string `json:"goal"`
	RunCount int    `json:"run_count"`
	LastRun  string `json:"last_run,omitempty"`
	NextRun  string `json:"next_run,omitempty"`
}

type workerTaskView struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Steps       int    `json:"steps"`
	Error       string `json:"error,omitempty"`
}

type workerStateView struct {
	State     string           `json:"state"`
	Running   int              `json:"running"`
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
	Tasks     []workerTaskView `json:"tasks"`
}

func (s *ChatService) dashboardSnapshot(ctx context.Context) (map[string]any, error) {
	currentSessionID := ""
	s.mu.Lock()
	if s.sess != nil {
		currentSessionID = s.sess.ID
	}
	s.mu.Unlock()

	sessionViews := make([]sessionSummaryView, 0)
	if s.store != nil {
		summaries, err := s.store.List(ctx)
		if err == nil {
			sort.Slice(summaries, func(i, j int) bool {
				return summaries[i].CreatedAt > summaries[j].CreatedAt
			})
			if len(summaries) > 20 {
				summaries = summaries[:20]
			}
			sessionViews = make([]sessionSummaryView, 0, len(summaries))
			for _, sum := range summaries {
				sessionViews = append(sessionViews, sessionSummaryView{
					SessionSummary: sum,
					Current:        sum.ID == currentSessionID,
				})
			}
		}
	}

	scheduleViews := make([]scheduleView, 0)
	if s.sched != nil {
		jobs := s.sched.ListJobs()
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
		scheduleViews = make([]scheduleView, 0, len(jobs))
		for _, j := range jobs {
			v := scheduleView{
				ID:       j.ID,
				Schedule: j.Schedule,
				Goal:     j.Goal,
				RunCount: j.RunCount,
			}
			if !j.LastRun.IsZero() {
				v.LastRun = j.LastRun.Format("2006-01-02 15:04:05")
			}
			if !j.NextRun.IsZero() {
				v.NextRun = j.NextRun.Format("2006-01-02 15:04:05")
			}
			scheduleViews = append(scheduleViews, v)
		}
	}

	worker, _ := s.buildWorkerState(ctx)
	if worker == nil {
		worker = &workerStateView{State: "completed", Tasks: []workerTaskView{}}
	}

	return map[string]any{
		"current_session_id": currentSessionID,
		"sessions":           sessionViews,
		"schedules":          scheduleViews,
		"worker":             worker,
	}, nil
}

func (s *ChatService) buildWorkerState(ctx context.Context) (*workerStateView, error) {
	tasks, err := s.listTasks(ctx, "", 24)
	if err != nil {
		return nil, err
	}
	state := &workerStateView{
		State: "completed",
		Tasks: make([]workerTaskView, 0, len(tasks)),
	}
	for _, t := range tasks {
		status := string(t.Status)
		if status == "" {
			status = "queued"
		}
		if status == string(agent.TaskRunning) {
			state.Running++
			state.State = "running"
		}
		if status == string(agent.TaskCompleted) {
			state.Succeeded++
		}
		if status == string(agent.TaskFailed) || status == string(agent.TaskCancelled) {
			state.Failed++
		}
		state.Tasks = append(state.Tasks, workerTaskView{
			ID:          t.ID,
			Description: t.Goal,
			Status:      status,
			Steps:       0,
			Error:       t.Error,
		})
	}
	return state, nil
}

func (s *ChatService) listTasks(ctx context.Context, status string, limit int) ([]agent.Task, error) {
	raw, err := s.invokeTool(ctx, "list_tasks", map[string]any{
		"status": strings.TrimSpace(status),
		"limit":  limit,
	})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tasks []agent.Task `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse list_tasks output: %w", err)
	}
	return payload.Tasks, nil
}

func (s *ChatService) invokeTool(ctx context.Context, name string, input any) (json.RawMessage, error) {
	if s.k == nil {
		return nil, fmt.Errorf("kernel not initialized")
	}
	_, handler, ok := s.k.ToolRegistry().Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %q not available", name)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal tool input: %w", err)
	}
	return handler(ctx, raw)
}

func (s *ChatService) handleSlashCommand(content string) (string, error) {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	switch parts[0] {
	case "/session":
		return s.sessionSummary(), nil
	case "/sessions":
		if s.store == nil {
			return "session store 不可用。", nil
		}
		summaries, err := s.store.List(ctx)
		if err != nil {
			return "", err
		}
		if len(summaries) == 0 {
			return "暂无历史会话。", nil
		}
		sort.Slice(summaries, func(i, j int) bool { return summaries[i].CreatedAt > summaries[j].CreatedAt })
		var b strings.Builder
		b.WriteString("Sessions:\n")
		for i, sum := range summaries {
			if i >= 15 {
				break
			}
			b.WriteString(fmt.Sprintf("- %s | %s | %s | steps=%d\n", sum.ID, sum.Status, sum.Goal, sum.Steps))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "/resume":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: /resume <session_id>")
		}
		if err := s.ResumeSession(parts[1]); err != nil {
			return "", err
		}
		return fmt.Sprintf("已恢复会话 %s", parts[1]), nil
	case "/offload":
		keepRecent := 20
		noteStart := 1
		if len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil {
				keepRecent = n
				noteStart = 2
			}
		}
		note := ""
		if len(parts) > noteStart {
			note = strings.Join(parts[noteStart:], " ")
		}
		s.mu.Lock()
		sess := s.sess
		s.mu.Unlock()
		if sess == nil {
			return "", fmt.Errorf("no active session")
		}
		raw, err := s.invokeTool(ctx, "offload_context", map[string]any{
			"session_id":  sess.ID,
			"keep_recent": keepRecent,
			"note":        note,
		})
		if err == nil {
			return formatJSON(raw), nil
		}
		// Fallback: resumed sessions loaded from store may not be tracked by SessionManager.
		return s.offloadContextLocally(ctx, sess, keepRecent, note)
	case "/tasks":
		status := ""
		limit := 20
		if len(parts) >= 2 {
			status = parts[1]
		}
		if len(parts) >= 3 {
			if n, err := strconv.Atoi(parts[2]); err == nil {
				limit = n
			}
		}
		raw, err := s.invokeTool(ctx, "list_tasks", map[string]any{"status": status, "limit": limit})
		if err != nil {
			return "", err
		}
		return formatJSON(raw), nil
	case "/task":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: /task <id> 或 /task cancel <id> [reason]")
		}
		if parts[1] == "cancel" {
			if len(parts) < 3 {
				return "", fmt.Errorf("usage: /task cancel <id> [reason]")
			}
			reason := ""
			if len(parts) > 3 {
				reason = strings.Join(parts[3:], " ")
			}
			raw, err := s.invokeTool(ctx, "cancel_task", map[string]any{"task_id": parts[2], "reason": reason})
			if err != nil {
				return "", err
			}
			return formatJSON(raw), nil
		}
		raw, err := s.invokeTool(ctx, "query_agent", map[string]any{"task_id": parts[1]})
		if err != nil {
			return "", err
		}
		return formatJSON(raw), nil
	case "/schedules":
		if s.sched == nil {
			return "scheduler 不可用。", nil
		}
		jobs := s.sched.ListJobs()
		if len(jobs) == 0 {
			return "当前没有定时任务。", nil
		}
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
		var b strings.Builder
		b.WriteString("Schedules:\n")
		for _, j := range jobs {
			b.WriteString(fmt.Sprintf("- %s | %s | %s\n", j.ID, j.Schedule, j.Goal))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "/schedule":
		if len(parts) < 4 {
			return "", fmt.Errorf("usage: /schedule <id> <@every|@after|@once> <goal>")
		}
		goal := strings.Join(parts[3:], " ")
		if err := s.sched.AddJob(scheduler.Job{
			ID:       parts[1],
			Schedule: parts[2],
			Goal:     goal,
			Config: session.SessionConfig{
				Goal:     goal,
				Mode:     "scheduled",
				MaxSteps: 30,
			},
		}); err != nil {
			return "", err
		}
		s.emitDashboard()
		return fmt.Sprintf("已添加定时任务 %s (%s)", parts[1], parts[2]), nil
	case "/schedule-cancel":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: /schedule-cancel <id>")
		}
		if err := s.sched.RemoveJob(parts[1]); err != nil {
			return "", err
		}
		s.emitDashboard()
		return fmt.Sprintf("已取消定时任务 %s", parts[1]), nil
	case "/dashboard":
		snapshot, err := s.dashboardSnapshot(ctx)
		if err != nil {
			return "", err
		}
		raw, _ := json.Marshal(snapshot)
		return formatJSON(raw), nil
	default:
		return "", fmt.Errorf("unknown command: %s", parts[0])
	}
}

func (s *ChatService) sessionSummary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sess == nil {
		return "当前没有活动 session。"
	}
	dialogCount := 0
	for _, msg := range s.sess.Messages {
		if msg.Role != port.RoleSystem {
			dialogCount++
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Session: %s\n", s.sess.ID))
	b.WriteString(fmt.Sprintf("Status: %s\n", s.sess.Status))
	b.WriteString(fmt.Sprintf("Messages: %d (dialog: %d)\n", len(s.sess.Messages), dialogCount))
	b.WriteString(fmt.Sprintf("Budget: steps %d/%d, tokens %d/%d",
		s.sess.Budget.UsedSteps, s.sess.Budget.MaxSteps,
		s.sess.Budget.UsedTokens, s.sess.Budget.MaxTokens,
	))
	if v, ok := s.sess.GetState("last_offload_snapshot"); ok {
		b.WriteString(fmt.Sprintf("\nLast offload snapshot: %v", v))
	}
	if v, ok := s.sess.GetState("last_offload_at"); ok {
		b.WriteString(fmt.Sprintf("\nLast offload time: %v", v))
	}
	return b.String()
}

func formatJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

func (s *ChatService) persistSession(sess *session.Session) error {
	if s.store == nil || sess == nil {
		return nil
	}
	return s.store.Save(context.Background(), sess)
}

func (s *ChatService) offloadContextLocally(ctx context.Context, sess *session.Session, keepRecent int, note string) (string, error) {
	if s.store == nil {
		return "", fmt.Errorf("session store is not configured")
	}
	if keepRecent <= 0 {
		keepRecent = 20
	}
	dialogCount := 0
	for _, m := range sess.Messages {
		if m.Role != port.RoleSystem {
			dialogCount++
		}
	}
	if dialogCount <= keepRecent {
		raw, _ := json.Marshal(map[string]any{
			"status":       "noop",
			"session_id":   sess.ID,
			"dialog_count": dialogCount,
			"keep_recent":  keepRecent,
		})
		return formatJSON(raw), nil
	}

	offloadID := fmt.Sprintf("%s_offload_%d", sess.ID, time.Now().UnixNano())
	snapshot := &session.Session{
		ID:       offloadID,
		Status:   session.StatusCompleted,
		Config:   sess.Config,
		Messages: append([]port.Message(nil), sess.Messages...),
		State: map[string]any{
			"offload_of": sess.ID,
			"note":       note,
		},
		Budget:    sess.Budget,
		CreatedAt: time.Now(),
		EndedAt:   time.Now(),
	}
	if err := s.store.Save(ctx, snapshot); err != nil {
		return "", fmt.Errorf("save offload snapshot: %w", err)
	}

	notice := fmt.Sprintf("[Context offloaded to snapshot %s; kept recent %d dialog messages]", offloadID, keepRecent)
	sess.Messages = session.BuildCompactedMessages(sess.Messages, keepRecent, notice)
	sess.SetState("last_offload_snapshot", offloadID)
	sess.SetState("last_offload_at", time.Now().Format(time.RFC3339))
	if err := s.persistSession(sess); err != nil {
		return "", fmt.Errorf("save compacted session: %w", err)
	}

	raw, _ := json.Marshal(map[string]any{
		"status":            "offloaded",
		"session_id":        sess.ID,
		"snapshot_session":  offloadID,
		"dialog_before":     dialogCount,
		"kept_recent":       keepRecent,
		"message_count_now": len(sess.Messages),
	})
	return formatJSON(raw), nil
}

func boolRef(v bool) *bool { return &v }
