package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/appkit"
	appruntime "github.com/mossagents/moss/appkit/runtime"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/presets/deepagent"
	"github.com/mossagents/moss/scheduler"
	"github.com/mossagents/moss/skill"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ChatService 桥接 moss kernel 与桌面前端。
type ChatService struct {
	cfg   config
	k     *kernel.Kernel
	sched *scheduler.Scheduler
	store session.SessionStore
	sess  *session.Session

	wailsIO    *WailsUserIO
	serviceCtx context.Context

	mu            sync.Mutex
	activeRuns    map[string]context.CancelFunc
	monitorCancel context.CancelFunc
}

func NewChatService(cfg config) *ChatService {
	return &ChatService{
		cfg:        cfg,
		activeRuns: make(map[string]context.CancelFunc),
	}
}

func (s *ChatService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	slog.Info("ChatService starting up...")
	s.serviceCtx = ctx
	s.wailsIO = NewWailsUserIO()

	// Apply user settings saved via the UI (fills any unset flags/env fields)
	saved := s.loadUserSettings()
	if s.cfg.provider == "" && saved.Provider != "" {
		s.cfg.provider = saved.Provider
	}
	if s.cfg.model == "" && saved.Model != "" {
		s.cfg.model = saved.Model
	}
	if s.cfg.baseURL == "" && saved.BaseURL != "" {
		s.cfg.baseURL = saved.BaseURL
	}
	if s.cfg.apiKey == "" && saved.APIKey != "" {
		s.cfg.apiKey = saved.APIKey
	}
	if s.cfg.workers == 0 && saved.Workers > 0 {
		s.cfg.workers = saved.Workers
	}

	k, err := s.buildKernel()
	if err != nil {
		return fmt.Errorf("build kernel: %w", err)
	}
	if err := k.Boot(ctx); err != nil {
		return fmt.Errorf("boot kernel: %w", err)
	}
	s.k = k

	// Repair any sessions left in "running" state from a previous crash before
	// deciding which thread to restore on startup.
	s.repairStaleRunningSessions(ctx)

	sess, err := s.restoreOrCreateStartupSession(ctx)
	if err != nil {
		return err
	}
	s.sess = sess

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
	cancels := make([]context.CancelFunc, 0, len(s.activeRuns))
	for sessionID, cancel := range s.activeRuns {
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
		delete(s.activeRuns, sessionID)
	}
	monitorCancel := s.monitorCancel
	s.monitorCancel = nil
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	if monitorCancel != nil {
		monitorCancel()
	}

	// Wait up to 2s for the running goroutine to finish and persist its session.
	// The kernel sets sess.Status = StatusCancelled on context cancellation, so if
	// the goroutine finishes in time the session will be correctly persisted.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
		s.mu.Lock()
		running := len(s.activeRuns) > 0
		s.mu.Unlock()
		if !running {
			break
		}
	}

	// If the goroutine didn't finish in time, force-persist the current session
	// as cancelled so it isn't left with status="running" on disk.
	s.mu.Lock()
	running := len(s.activeRuns) > 0
	sess := s.sess
	s.mu.Unlock()
	if running && sess != nil && s.store != nil {
		if sess.Status == session.StatusRunning || sess.Status == session.StatusCreated {
			sess.Status = session.StatusCancelled
			sess.EndedAt = time.Now()
		}
		if err := s.persistSession(sess); err != nil {
			slog.Warn("force-persist cancelled session failed", slog.Any("error", err))
		}
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
	if s.sess == nil || s.k == nil {
		s.mu.Unlock()
		return fmt.Errorf("service not initialized")
	}
	// Capture sess locally so mid-run session switches don't affect this goroutine.
	sess := s.sess
	sessID := sess.ID
	s.mu.Unlock()

	if strings.HasPrefix(trimmed, "/") {
		if handled, err := s.tryHandleRetryCommand(trimmed); handled {
			return err
		}
		if rewritten, ok, err := s.rewriteSkillLikeSlashCommand(trimmed); ok {
			trimmed = rewritten
			content = rewritten
		} else if err != nil {
			emitEvent("chat:error", map[string]any{"message": err.Error(), "session_id": sessID})
			emitEvent("chat:done", map[string]any{
				"session_id":  sessID,
				"steps":       0,
				"tokens_used": 0,
				"output":      "",
			})
			s.emitDashboard()
			return nil
		} else {
			output, err := s.handleSlashCommand(trimmed)
			if err != nil {
				emitEvent("chat:error", map[string]any{"message": err.Error(), "session_id": sessID})
			} else if strings.TrimSpace(output) != "" {
				emitEvent("chat:text", map[string]any{"content": output, "session_id": sessID})
			}
			emitEvent("chat:done", map[string]any{
				"session_id":  sessID,
				"steps":       0,
				"tokens_used": 0,
				"output":      "",
			})
			s.emitDashboard()
			return nil
		}
	}

	return s.sendMessageToSession(sess, content)
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
	if s.sess == nil {
		return
	}
	if cancel := s.activeRuns[s.sess.ID]; cancel != nil {
		cancel()
	}
}

func (s *ChatService) RespondToAsk(value string, approved bool) {
	s.wailsIO.RespondToAsk(io.InputResponse{Value: value, Approved: approved})
}

func (s *ChatService) NewSession() error {
	// Intentionally does NOT stop any running agent.
	// The running session continues in the background; UI switches to the new session.
	ctx := application.Get().Context()
	sess, err := s.k.NewSession(ctx, s.newSessionConfig())
	if err != nil {
		return err
	}
	sess.SetTitle("New Chat")
	s.mu.Lock()
	s.sess = sess
	s.mu.Unlock()
	if err := s.persistSession(sess); err != nil {
		slog.Warn("persist new session failed", slog.Any("error", err))
	}
	s.emitDashboard()
	return nil
}

func (s *ChatService) ResumeSession(id string) error {
	// Intentionally does NOT stop any running agent.
	// The running session continues in the background; UI switches to the resumed session.
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
	s.mu.Lock()
	s.sess = sess
	s.mu.Unlock()
	if err := s.persistSession(sess); err != nil {
		slog.Warn("persist resumed session failed", slog.Any("error", err))
	}
	s.emitDashboard()
	return nil
}

// HistoryTool represents a single tool call and its result from session history.
type HistoryTool struct {
	Name    string `json:"name"`
	Input   string `json:"input,omitempty"`
	Result  string `json:"result,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

// HistoryMessage is a frontend-friendly representation of a session message.
type HistoryMessage struct {
	HistoryIndex int           `json:"history_index"`
	Role         string        `json:"role"`
	Content      string        `json:"content"`
	Thinking     string        `json:"thinking,omitempty"`
	Tools        []HistoryTool `json:"tools,omitempty"`
	Retryable    bool          `json:"retryable,omitempty"`
}

// GetSessionHistory returns the message history of a session in a format suitable for the chat UI.
// It skips system/tool-role messages and inlines tool call results into the assistant turn.
func (s *ChatService) GetSessionHistory(id string) ([]HistoryMessage, error) {
	s.mu.Lock()
	var sess *session.Session
	if s.sess != nil && s.sess.ID == id {
		sess = s.sess
	}
	s.mu.Unlock()

	if sess == nil {
		if s.store == nil {
			return nil, fmt.Errorf("session store not configured")
		}
		loaded, err := s.store.Load(context.Background(), id)
		if err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}
		if loaded == nil {
			return nil, fmt.Errorf("session %q not found", id)
		}
		sess = loaded
	}
	return convertToHistoryMessages(sess.CopyMessages()), nil
}

func convertToHistoryMessages(msgs []model.Message) []HistoryMessage {
	// Build call ID → tool result mapping from tool-role messages.
	toolResults := make(map[string]model.ToolResult)
	for _, msg := range msgs {
		for _, tr := range msg.ToolResults {
			toolResults[tr.CallID] = tr
		}
	}

	var out []HistoryMessage
	historyIndex := 0
	for _, msg := range msgs {
		switch msg.Role {
		case model.RoleSystem, model.RoleTool:
			continue
		case model.RoleUser:
			text := extractTextFromParts(msg.ContentParts)
			if text == "" {
				continue
			}
			out = append(out, HistoryMessage{
				HistoryIndex: historyIndex,
				Role:         "user",
				Content:      text,
				Retryable:    true,
			})
			historyIndex++
		case model.RoleAssistant:
			hm := HistoryMessage{
				HistoryIndex: historyIndex,
				Role:         "assistant",
			}
			for _, cp := range msg.ContentParts {
				switch cp.Type {
				case model.ContentPartText:
					hm.Content += cp.Text
				case model.ContentPartReasoning:
					hm.Thinking += cp.Text
				}
			}
			for _, tc := range msg.ToolCalls {
				ht := HistoryTool{Name: tc.Name, Input: string(tc.Arguments)}
				if tr, ok := toolResults[tc.ID]; ok {
					ht.Result = extractTextFromParts(tr.ContentParts)
					ht.IsError = tr.IsError
				}
				hm.Tools = append(hm.Tools, ht)
			}
			if hm.Content == "" && hm.Thinking == "" && len(hm.Tools) == 0 {
				continue
			}
			out = append(out, hm)
			historyIndex++
		}
	}
	return out
}

func (s *ChatService) sendMessageToSession(sess *session.Session, content string) error {
	if sess == nil || s.k == nil {
		return fmt.Errorf("service not initialized")
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	s.mu.Lock()
	if _, running := s.activeRuns[sess.ID]; running {
		s.mu.Unlock()
		return fmt.Errorf("agent is already running")
	}
	sessID := sess.ID
	s.mu.Unlock()

	sess.AppendMessage(model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(content)}})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				emitEvent("chat:error", map[string]any{"message": fmt.Sprintf("internal error: %v", r), "session_id": sessID})
			}
			if err := s.persistSession(sess); err != nil {
				slog.Debug("persist session after run failed", slog.Any("error", err))
			}
			s.mu.Lock()
			delete(s.activeRuns, sessID)
			s.mu.Unlock()
			s.emitDashboard()
		}()

		ctx, cancel := context.WithCancel(context.Background())
		ctx = context.WithValue(ctx, sessionIDKey{}, sessID)
		s.mu.Lock()
		s.activeRuns[sessID] = cancel
		s.mu.Unlock()
		defer cancel()

		result, err := s.k.RunWithUserIO(ctx, sess, s.wailsIO)
		if err != nil {
			if ctx.Err() != nil {
				emitEvent("chat:cancelled", map[string]any{"message": "已取消", "session_id": sessID})
				return
			}
			emitEvent("chat:error", map[string]any{"message": err.Error(), "session_id": sessID})
			return
		}

		go s.maybeGenerateTitle(sess)

		emitEvent("chat:done", map[string]any{
			"session_id":  result.SessionID,
			"steps":       result.Steps,
			"tokens_used": result.TokensUsed.TotalTokens,
			"output":      result.Output,
		})
	}()

	return nil
}

func locateRetryPoint(msgs []model.Message, targetHistoryIndex int) (int, string, error) {
	historyIndex := 0
	for rawIndex, msg := range msgs {
		switch msg.Role {
		case model.RoleSystem, model.RoleTool:
			continue
		case model.RoleUser:
			text := strings.TrimSpace(extractTextFromParts(msg.ContentParts))
			if text == "" {
				continue
			}
			if historyIndex == targetHistoryIndex {
				return rawIndex, text, nil
			}
			historyIndex++
		case model.RoleAssistant:
			hasVisibleContent := false
			for _, cp := range msg.ContentParts {
				if cp.Type == model.ContentPartText || cp.Type == model.ContentPartReasoning {
					hasVisibleContent = true
					break
				}
			}
			if !hasVisibleContent && len(msg.ToolCalls) == 0 {
				continue
			}
			if historyIndex == targetHistoryIndex {
				return -1, "", fmt.Errorf("retry only supports user messages")
			}
			historyIndex++
		}
	}
	return -1, "", fmt.Errorf("message point %d not found", targetHistoryIndex)
}

func (s *ChatService) retryUserMessage(historyIndex int) error {
	s.mu.Lock()
	if s.sess == nil || s.k == nil {
		s.mu.Unlock()
		return fmt.Errorf("service not initialized")
	}
	sess := s.sess
	s.mu.Unlock()

	msgs := sess.CopyMessages()
	rawIndex, content, err := locateRetryPoint(msgs, historyIndex)
	if err != nil {
		return err
	}
	if rawIndex < 0 {
		return fmt.Errorf("retry point %d is invalid", historyIndex)
	}

	truncated := append([]model.Message(nil), msgs[:rawIndex]...)
	sess.ReplaceMessages(truncated)
	sess.Status = session.StatusCreated
	sess.EndedAt = time.Time{}
	if err := s.persistSession(sess); err != nil {
		slog.Warn("persist retried session failed", slog.Any("error", err))
	}
	s.emitDashboard()
	return s.sendMessageToSession(sess, content)
}

func (s *ChatService) tryHandleRetryCommand(content string) (bool, error) {
	parts := strings.Fields(content)
	if len(parts) == 0 || parts[0] != "/retry" {
		return false, nil
	}
	if len(parts) != 2 {
		return true, fmt.Errorf("usage: /retry <history_index>")
	}
	idx, err := strconv.Atoi(parts[1])
	if err != nil || idx < 0 {
		return true, fmt.Errorf("history_index must be a non-negative integer")
	}
	return true, s.retryUserMessage(idx)
}

func extractTextFromParts(parts []model.ContentPart) string {
	var sb strings.Builder
	for _, cp := range parts {
		if cp.Type == model.ContentPartText {
			sb.WriteString(cp.Text)
		}
	}
	return sb.String()
}

// DeleteSession removes a single session by ID.
// If the deleted session is the current active session, the active session is cleared.
func (s *ChatService) DeleteSession(id string) error {
	s.mu.Lock()
	if _, running := s.activeRuns[id]; running {
		s.mu.Unlock()
		return fmt.Errorf("cannot delete session while that session is running")
	}
	s.mu.Unlock()

	if s.store == nil {
		return fmt.Errorf("session store is not configured")
	}
	if err := s.store.Delete(context.Background(), id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	s.mu.Lock()
	if s.sess != nil && s.sess.ID == id {
		s.sess = nil
	}
	s.mu.Unlock()
	s.emitDashboard()
	return nil
}

// DeleteSessions removes multiple sessions by ID in one call.
// Errors from individual deletions are collected and returned as a combined error.
func (s *ChatService) DeleteSessions(ids []string) error {
	s.mu.Lock()
	for _, id := range ids {
		if _, running := s.activeRuns[id]; running {
			s.mu.Unlock()
			return fmt.Errorf("cannot delete session %q while it is running", id)
		}
	}
	s.mu.Unlock()

	if s.store == nil {
		return fmt.Errorf("session store is not configured")
	}
	var errs []string
	for _, id := range ids {
		if err := s.store.Delete(context.Background(), id); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		s.mu.Lock()
		if s.sess != nil && s.sess.ID == id {
			s.sess = nil
		}
		s.mu.Unlock()
	}
	s.emitDashboard()
	if len(errs) > 0 {
		return fmt.Errorf("delete sessions: %s", strings.Join(errs, "; "))
	}
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

// ToolInfo is a frontend-friendly summary of a registered tool.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Risk        string `json:"risk"`
	Source      string `json:"source,omitempty"`
}

// SkillInfo is a frontend-friendly summary of a discovered skill.
type SkillInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on,omitempty"`
	RequiredEnv []string `json:"required_env,omitempty"`
	Source      string   `json:"source,omitempty"`
	Active      bool     `json:"active"`
}

// GetTools returns the list of tools registered with the kernel.
func (s *ChatService) GetTools() []ToolInfo {
	if s.k == nil {
		return nil
	}
	specs := s.k.ToolRegistry().List()
	infos := make([]ToolInfo, 0, len(specs))
	for _, sp := range specs {
		infos = append(infos, ToolInfo{
			Name:        sp.Name,
			Description: sp.Description,
			Risk:        string(sp.Risk),
			Source:      sp.Source,
		})
	}
	return infos
}

// GetSkills returns the list of discovered skills and whether they are active.
func (s *ChatService) GetSkills() []SkillInfo {
	if s.k == nil {
		return nil
	}
	manager := appruntime.SkillsManager(s.k)
	workspaceDir := s.cfg.workspace
	if strings.TrimSpace(workspaceDir) == "" {
		workspaceDir = "."
	}
	manifests := skill.DiscoverSkillManifestsForTrust(workspaceDir, s.cfg.trust)
	infos := make([]SkillInfo, 0, len(manifests))
	for _, manifest := range manifests {
		_, active := manager.Get(manifest.Name)
		infos = append(infos, SkillInfo{
			Name:        manifest.Name,
			Description: manifest.Description,
			DependsOn:   append([]string(nil), manifest.DependsOn...),
			RequiredEnv: append([]string(nil), manifest.RequiredEnv...),
			Source:      manifest.Source,
			Active:      active,
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Active != infos[j].Active {
			return infos[i].Active
		}
		return infos[i].Name < infos[j].Name
	})
	return infos
}

func (s *ChatService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sess == nil {
		return false
	}
	_, running := s.activeRuns[s.sess.ID]
	return running
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
	deepCfg := deepagent.DefaultConfig()
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

	k, err := deepagent.BuildKernel(ctx, flags, s.wailsIO, &deepCfg)
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

type desktopSlashCommandHandler func(*ChatService, context.Context, []string) (string, error)

var desktopSlashCommandRegistry = map[string]desktopSlashCommandHandler{
	"/session":         (*ChatService).handleSessionSlashCommand,
	"/sessions":        (*ChatService).handleSessionsSlashCommand,
	"/resume":          (*ChatService).handleResumeSlashCommand,
	"/compact":         (*ChatService).handleCompactSlashCommand,
	"/tasks":           (*ChatService).handleTasksSlashCommand,
	"/task":            (*ChatService).handleTaskSlashCommand,
	"/schedules":       (*ChatService).handleSchedulesSlashCommand,
	"/schedule":        (*ChatService).handleScheduleSlashCommand,
	"/schedule-cancel": (*ChatService).handleScheduleCancelSlashCommand,
	"/dashboard":       (*ChatService).handleDashboardSlashCommand,
}

func (s *ChatService) handleSlashCommand(content string) (string, error) {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	handler, ok := desktopSlashCommandRegistry[parts[0]]
	if !ok {
		return "", fmt.Errorf("unknown command: %s", parts[0])
	}
	return handler(s, ctx, parts)
}

func (s *ChatService) rewriteSkillLikeSlashCommand(content string) (string, bool, error) {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return "", false, nil
	}

	cmd := strings.TrimSpace(parts[0])
	if _, ok := desktopSlashCommandRegistry[cmd]; ok {
		return "", false, nil
	}

	if cmd == "/skill" {
		if len(parts) < 3 {
			return "", true, fmt.Errorf("usage: /skill <name> <task...>")
		}
		name := strings.TrimSpace(parts[1])
		task := strings.TrimSpace(strings.Join(parts[2:], " "))
		if task == "" {
			return "", true, fmt.Errorf("usage: /skill <name> <task...>")
		}
		if !s.hasSkillLikeTarget(name) {
			return "", true, fmt.Errorf("unknown skill or tool: %s", name)
		}
		return buildSkillLikePrompt(name, task), true, nil
	}

	name := strings.TrimSpace(strings.TrimPrefix(cmd, "/"))
	if name == "" || !s.hasSkillLikeTarget(name) {
		return "", false, nil
	}
	if len(parts) < 2 {
		return "", true, fmt.Errorf("usage: /%s <task...>", name)
	}
	task := strings.TrimSpace(strings.Join(parts[1:], " "))
	if task == "" {
		return "", true, fmt.Errorf("usage: /%s <task...>", name)
	}
	return buildSkillLikePrompt(name, task), true, nil
}

func (s *ChatService) hasSkillLikeTarget(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || s.k == nil {
		return false
	}
	if _, _, ok := s.k.ToolRegistry().Get(name); ok {
		return true
	}
	if manager := appruntime.SkillsManager(s.k); manager != nil {
		if _, ok := manager.Get(name); ok {
			return true
		}
	}
	workspaceDir := s.cfg.workspace
	if strings.TrimSpace(workspaceDir) == "" {
		workspaceDir = "."
	}
	for _, manifest := range skill.DiscoverSkillManifestsForTrust(workspaceDir, s.cfg.trust) {
		if manifest.Name == name {
			return true
		}
	}
	return false
}

func buildSkillLikePrompt(name, task string) string {
	return fmt.Sprintf(
		"Use skill or tool '%s' to complete this request.\n"+
			"If you call run_command, you must provide structured inputs with separate command and args fields. "+
			"Do not send shell-form command strings when execution paths are restricted.\n"+
			"Request:\n%s",
		name,
		task,
	)
}

func (s *ChatService) handleSessionSlashCommand(_ context.Context, _ []string) (string, error) {
	return s.sessionSummary(), nil
}

func (s *ChatService) handleSessionsSlashCommand(ctx context.Context, _ []string) (string, error) {
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
}

func (s *ChatService) handleResumeSlashCommand(_ context.Context, parts []string) (string, error) {
	if len(parts) < 2 {
		return "", fmt.Errorf("usage: /resume <session_id>")
	}
	if err := s.ResumeSession(parts[1]); err != nil {
		return "", err
	}
	return "", nil
}

func (s *ChatService) handleCompactSlashCommand(ctx context.Context, parts []string) (string, error) {
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
}

func (s *ChatService) handleTasksSlashCommand(ctx context.Context, parts []string) (string, error) {
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
}

func (s *ChatService) handleTaskSlashCommand(ctx context.Context, parts []string) (string, error) {
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
}

func (s *ChatService) handleSchedulesSlashCommand(_ context.Context, _ []string) (string, error) {
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
}

func (s *ChatService) handleScheduleSlashCommand(_ context.Context, parts []string) (string, error) {
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
}

func (s *ChatService) handleScheduleCancelSlashCommand(_ context.Context, parts []string) (string, error) {
	if len(parts) < 2 {
		return "", fmt.Errorf("usage: /schedule-cancel <id>")
	}
	if err := s.sched.RemoveJob(parts[1]); err != nil {
		return "", err
	}
	s.emitDashboard()
	return fmt.Sprintf("已取消定时任务 %s", parts[1]), nil
}

func (s *ChatService) handleDashboardSlashCommand(ctx context.Context, _ []string) (string, error) {
	snapshot, err := s.dashboardSnapshot(ctx)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(snapshot)
	return formatJSON(raw), nil
}

func (s *ChatService) sessionSummary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sess == nil {
		return "当前没有活动 session。"
	}
	dialogCount := 0
	for _, msg := range s.sess.Messages {
		if msg.Role != model.RoleSystem {
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

func (s *ChatService) restoreOrCreateStartupSession(ctx context.Context) (*session.Session, error) {
	if s.k == nil {
		return nil, fmt.Errorf("kernel not initialized")
	}
	if s.store != nil {
		summaries, err := s.store.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		// Restore the most recently created session.
		sort.Slice(summaries, func(i, j int) bool {
			return summaries[i].CreatedAt > summaries[j].CreatedAt
		})
		for _, summary := range summaries {
			sess, err := s.store.Load(ctx, summary.ID)
			if err != nil {
				slog.Warn("load startup session failed",
					slog.String("session_id", summary.ID),
					slog.Any("error", err),
				)
				continue
			}
			if sess == nil {
				continue
			}
			return sess, nil
		}
	}

	sess, err := s.k.NewSession(ctx, s.newSessionConfig())
	if err != nil {
		return nil, fmt.Errorf("create startup session: %w", err)
	}
	sess.SetTitle("New Chat")
	if err := s.persistSession(sess); err != nil {
		slog.Warn("persist startup session failed", slog.Any("error", err))
	}
	return sess, nil
}

// repairStaleRunningSessions scans the session store for sessions that are
// still in "running" state — left over from a previous crash — and marks them
// as cancelled. Called once on startup before the service becomes active.
func (s *ChatService) repairStaleRunningSessions(ctx context.Context) {
	if s.store == nil {
		return
	}
	summaries, err := s.store.List(ctx)
	if err != nil {
		slog.Warn("repairStaleRunningSessions: list failed", slog.Any("error", err))
		return
	}
	for _, sum := range summaries {
		if sum.Status != session.StatusRunning {
			continue
		}
		sess, err := s.store.Load(ctx, sum.ID)
		if err != nil || sess == nil {
			continue
		}
		sess.Status = session.StatusCancelled
		sess.EndedAt = time.Now()
		if err := s.store.Save(ctx, sess); err != nil {
			slog.Warn("repairStaleRunningSessions: save failed",
				slog.String("session_id", sum.ID),
				slog.Any("error", err),
			)
		} else {
			slog.Info("repairStaleRunningSessions: marked cancelled",
				slog.String("session_id", sum.ID),
			)
		}
	}
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
		if m.Role != model.RoleSystem {
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
		Messages: append([]model.Message(nil), sess.Messages...),
		State: map[string]any{
			"offload_of": sess.ID,
			"note":       note,
		},
		Budget:    sess.Budget.Clone(),
		CreatedAt: time.Now(),
		EndedAt:   time.Now(),
	}
	session.MarkHistoryHidden(snapshot)
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

// maybeGenerateTitle generates an LLM-based display title for the session if
// the title is still the default "New Chat". Called as a background goroutine
// after the first successful agent run.
func (s *ChatService) maybeGenerateTitle(sess *session.Session) {
	if sess == nil || sess.GetTitle() != "New Chat" {
		return
	}
	title := s.generateTitleFromLLM(sess)
	if title == "" {
		return
	}
	sess.SetTitle(title)
	if err := s.persistSession(sess); err != nil {
		slog.Debug("persist session title failed", slog.Any("error", err))
	}
	emitEvent("session:title", map[string]any{
		"session_id": sess.ID,
		"title":      title,
	})
	s.emitDashboard()
}

// generateTitleFromLLM makes a lightweight LLM call to generate a short
// display title based on the first user+assistant exchange in the session.
func (s *ChatService) generateTitleFromLLM(sess *session.Session) string {
	if s.k == nil {
		return ""
	}
	llm := s.k.LLM()
	if llm == nil {
		return ""
	}

	msgs := sess.CopyMessages()
	var firstUser, firstAssistant string
	for _, m := range msgs {
		if m.Role == model.RoleUser && firstUser == "" {
			firstUser = model.ContentPartsToPlainText(m.ContentParts)
		}
		if m.Role == model.RoleAssistant && firstAssistant == "" {
			firstAssistant = model.ContentPartsToPlainText(m.ContentParts)
		}
		if firstUser != "" && firstAssistant != "" {
			break
		}
	}
	if firstUser == "" {
		return ""
	}

	const maxInputRunes = 200
	truncate := func(s string) string {
		r := []rune(s)
		if len(r) > maxInputRunes {
			return string(r[:maxInputRunes]) + "..."
		}
		return s
	}
	prompt := "用户：" + truncate(firstUser)
	if firstAssistant != "" {
		prompt += "\n助手：" + truncate(firstAssistant)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := llm.Complete(ctx, model.CompletionRequest{
		Messages: []model.Message{
			{
				Role: model.RoleSystem,
				ContentParts: []model.ContentPart{model.TextPart(
					"你是一个会话标题生成器。根据以下对话内容，生成一个简洁的中文标题（不超过15字，只输出标题本身，不含引号、标点符号、序号、解释说明）。",
				)},
			},
			{
				Role:         model.RoleUser,
				ContentParts: []model.ContentPart{model.TextPart(prompt)},
			},
		},
		Config: model.ModelConfig{
			MaxTokens:   64,
			Temperature: 0.3,
		},
	})
	if err != nil {
		slog.Debug("title generation failed", slog.Any("error", err))
		return ""
	}

	title := strings.TrimSpace(model.ContentPartsToPlainText(resp.Message.ContentParts))
	title = strings.Trim(title, `"'"""''`)
	title = strings.TrimSpace(title)

	const maxTitleRunes = 20
	if r := []rune(title); len(r) > maxTitleRunes {
		title = string(r[:maxTitleRunes])
	}
	return title
}

// AddAutomation creates or replaces a scheduled automation task.
func (s *ChatService) AddAutomation(id, schedule, goal string) error {
	if s.sched == nil {
		return fmt.Errorf("scheduler not initialized")
	}
	_ = s.sched.RemoveJob(id)
	if err := s.sched.AddJob(scheduler.Job{
		ID:       id,
		Schedule: schedule,
		Goal:     goal,
		Config: session.SessionConfig{
			Goal:       goal,
			Mode:       "scheduled",
			MaxSteps:   30,
			TrustLevel: s.cfg.trust,
		},
	}); err != nil {
		return err
	}
	s.emitDashboard()
	return nil
}

// RemoveAutomation removes a scheduled task by ID.
func (s *ChatService) RemoveAutomation(id string) error {
	if s.sched == nil {
		return fmt.Errorf("scheduler not initialized")
	}
	if err := s.sched.RemoveJob(id); err != nil {
		return err
	}
	s.emitDashboard()
	return nil
}

// GetAutomations returns all scheduled tasks.
func (s *ChatService) GetAutomations() ([]scheduleView, error) {
	if s.sched == nil {
		return []scheduleView{}, nil
	}
	jobs := s.sched.ListJobs()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	views := make([]scheduleView, 0, len(jobs))
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
		views = append(views, v)
	}
	return views, nil
}

// RunAutomationNow triggers an automation task immediately (fire-and-forget).
func (s *ChatService) RunAutomationNow(id string) error {
	if s.sched == nil {
		return fmt.Errorf("scheduler not initialized")
	}
	jobs := s.sched.ListJobs()
	for _, j := range jobs {
		if j.ID == id {
			go func(job scheduler.Job) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				cfg := job.Config
				if cfg.Goal == "" {
					cfg.Goal = job.Goal
				}
				sess, err := s.k.NewSession(ctx, cfg)
				if err != nil {
					slog.Warn("RunAutomationNow: create session failed", slog.Any("error", err))
					return
				}
				sess.AppendMessage(model.Message{
					Role:         model.RoleUser,
					ContentParts: []model.ContentPart{model.TextPart(job.Goal)},
				})
				if _, err := s.k.RunWithUserIO(ctx, sess, s.wailsIO); err != nil {
					slog.Warn("RunAutomationNow: run failed", slog.Any("error", err))
				}
				if err := s.persistSession(sess); err != nil {
					slog.Debug("RunAutomationNow: persist failed", slog.Any("error", err))
				}
			}(j)
			return nil
		}
	}
	return fmt.Errorf("automation %q not found", id)
}

// ─── Settings & Model Configuration ─────────────────────────────────────────

// ModelPreset describes a known LLM model combination.
// Provider is the canonical API type (openai-completions, openai-responses, claude, gemini).
// Group is a display sub-group for grouping models within the same API type (e.g. "openai", "deepseek", "ollama").
type ModelPreset struct {
	Provider string `json:"provider"`
	Group    string `json:"group,omitempty"`
	Label    string `json:"label"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
}

// appSettings mirrors the fields that can be persisted to settings.json.
type appSettings struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Workers  int    `json:"workers,omitempty"`
}

var presetModels = []ModelPreset{
	// OpenAI Completions — OpenAI
	{Provider: "openai-completions", Group: "openai", Label: "GPT-4o", Model: "gpt-4o"},
	{Provider: "openai-completions", Group: "openai", Label: "GPT-4o mini", Model: "gpt-4o-mini"},
	{Provider: "openai-completions", Group: "openai", Label: "GPT-4.1", Model: "gpt-4.1"},
	{Provider: "openai-completions", Group: "openai", Label: "o1", Model: "o1"},
	{Provider: "openai-completions", Group: "openai", Label: "o3-mini", Model: "o3-mini"},
	// OpenAI Completions — DeepSeek (OpenAI-compatible endpoint)
	{Provider: "openai-completions", Group: "deepseek", Label: "DeepSeek V3 (Chat)", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1"},
	{Provider: "openai-completions", Group: "deepseek", Label: "DeepSeek R1 (Reasoner)", Model: "deepseek-reasoner", BaseURL: "https://api.deepseek.com/v1"},
	// OpenAI Completions — Ollama (local OpenAI-compatible endpoint)
	{Provider: "openai-completions", Group: "ollama", Label: "Llama 3.2", Model: "llama3.2", BaseURL: "http://localhost:11434/v1"},
	{Provider: "openai-completions", Group: "ollama", Label: "Qwen 2.5", Model: "qwen2.5", BaseURL: "http://localhost:11434/v1"},
	{Provider: "openai-completions", Group: "ollama", Label: "Mistral", Model: "mistral", BaseURL: "http://localhost:11434/v1"},
	{Provider: "openai-completions", Group: "ollama", Label: "Gemma 3", Model: "gemma3", BaseURL: "http://localhost:11434/v1"},
	{Provider: "openai-completions", Group: "ollama", Label: "DeepSeek R1 (local)", Model: "deepseek-r1", BaseURL: "http://localhost:11434/v1"},
	// Claude — Anthropic
	{Provider: "claude", Group: "anthropic", Label: "Claude 3.7 Sonnet", Model: "claude-3-7-sonnet-20250219"},
	{Provider: "claude", Group: "anthropic", Label: "Claude 3.5 Sonnet", Model: "claude-3-5-sonnet-20241022"},
	{Provider: "claude", Group: "anthropic", Label: "Claude 3.5 Haiku", Model: "claude-3-5-haiku-20241022"},
	{Provider: "claude", Group: "anthropic", Label: "Claude 3 Opus", Model: "claude-3-opus-20240229"},
	// Gemini — Google
	{Provider: "gemini", Group: "google", Label: "Gemini 2.5 Pro", Model: "gemini-2.5-pro-preview-05-06"},
	{Provider: "gemini", Group: "google", Label: "Gemini 2.0 Flash", Model: "gemini-2.0-flash"},
	{Provider: "gemini", Group: "google", Label: "Gemini 2.0 Flash Thinking", Model: "gemini-2.0-flash-thinking-exp"},
	{Provider: "gemini", Group: "google", Label: "Gemini 1.5 Pro", Model: "gemini-1.5-pro"},
	{Provider: "gemini", Group: "google", Label: "Gemini 1.5 Flash", Model: "gemini-1.5-flash"},
}

func (s *ChatService) settingsPath() string {
	appDir := appconfig.AppDir()
	if appDir == "" {
		return ""
	}
	return filepath.Join(appDir, "settings.json")
}

func (s *ChatService) loadUserSettings() appSettings {
	p := s.settingsPath()
	if p == "" {
		return appSettings{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return appSettings{}
	}
	var st appSettings
	if err := json.Unmarshal(data, &st); err != nil {
		return appSettings{}
	}
	// Migrate legacy provider values to canonical API types.
	st.Provider, st.BaseURL = migrateProviderSettings(st.Provider, st.BaseURL)
	return st
}

// migrateProviderSettings converts legacy provider names to canonical API types.
// Returns the normalized provider and (possibly updated) base URL.
func migrateProviderSettings(provider, baseURL string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return "openai-completions", baseURL
	case "anthropic":
		return "claude", baseURL
	case "deepseek":
		if baseURL == "" {
			baseURL = "https://api.deepseek.com/v1"
		}
		return "openai-completions", baseURL
	case "ollama":
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		return "openai-completions", baseURL
	case "google":
		return "gemini", baseURL
	case "custom":
		return "openai-completions", baseURL
	default:
		return provider, baseURL
	}
}

func (s *ChatService) saveUserSettings() error {
	p := s.settingsPath()
	if p == "" {
		return fmt.Errorf("cannot determine settings path")
	}
	st := appSettings{
		Provider: s.cfg.provider,
		Model:    s.cfg.model,
		BaseURL:  s.cfg.baseURL,
		APIKey:   s.cfg.apiKey,
		Workers:  s.cfg.workers,
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

func maskAPIKey(key string) string {
	if len(key) == 0 {
		return ""
	}
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

// GetSettings returns the current active model/provider settings (API key masked).
func (s *ChatService) GetSettings() map[string]any {
	return map[string]any{
		"provider": s.cfg.provider,
		"model":    s.cfg.model,
		"baseURL":  s.cfg.baseURL,
		"apiKey":   maskAPIKey(s.cfg.apiKey),
		"workers":  s.cfg.workers,
	}
}

// GetPresetModels returns the built-in list of known LLM providers and models.
func (s *ChatService) GetPresetModels() []ModelPreset {
	return presetModels
}

// UpdateModel hot-reloads the kernel with a new provider/model/key configuration.
// Returns an error if an agent is currently running.
func (s *ChatService) UpdateModel(provider, model, baseURL, apiKey string) error {
	s.mu.Lock()
	if len(s.activeRuns) > 0 {
		s.mu.Unlock()
		return fmt.Errorf("agent is running; please wait for it to finish")
	}
	monitorCancel := s.monitorCancel
	s.monitorCancel = nil
	s.mu.Unlock()

	if monitorCancel != nil {
		monitorCancel()
	}

	// Update in-memory config (skip API key if it's the masked placeholder)
	s.cfg.provider = provider
	s.cfg.model = model
	s.cfg.baseURL = baseURL
	if apiKey != "" && !strings.Contains(apiKey, "***") {
		s.cfg.apiKey = apiKey
	}

	if err := s.saveUserSettings(); err != nil {
		slog.Warn("save settings failed", slog.Any("error", err))
	}

	// Shutdown old kernel
	if s.k != nil {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		if err := s.k.Shutdown(shutdownCtx); err != nil &&
			!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("kernel shutdown during model update", slog.Any("error", err))
		}
		stop()
		s.k = nil
	}

	// Rebuild kernel with new settings
	ctx := s.serviceCtx
	if ctx == nil {
		ctx = context.Background()
	}
	k, err := s.buildKernel()
	if err != nil {
		return fmt.Errorf("rebuild kernel: %w", err)
	}
	if err := k.Boot(ctx); err != nil {
		return fmt.Errorf("boot kernel: %w", err)
	}
	s.k = k

	// Create a fresh session
	sess, err := k.NewSession(ctx, s.newSessionConfig())
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	sess.SetTitle("New Chat")
	s.mu.Lock()
	s.sess = sess
	s.mu.Unlock()
	if err := s.persistSession(sess); err != nil {
		slog.Warn("persist session after model update failed", slog.Any("error", err))
	}

	s.startDashboardMonitor(ctx)
	s.emitDashboard()

	emitEvent("config:updated", map[string]any{
		"provider": provider,
		"model":    model,
	})
	slog.Info("Model updated", slog.String("provider", provider), slog.String("model", model))
	return nil
}
