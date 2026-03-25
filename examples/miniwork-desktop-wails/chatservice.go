package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/appkit"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/tool"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ChatService 桥接 moss kernel 与桌面前端。
type ChatService struct {
	cfg     config
	k       *kernel.Kernel
	sess    *session.Session
	wailsIO *WailsUserIO
	tracker *orchestrationTracker

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

func NewChatService(cfg config) *ChatService {
	return &ChatService{cfg: cfg}
}

func (s *ChatService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	slog.Info("ChatService starting up...")
	s.wailsIO = NewWailsUserIO()
	s.tracker = newOrchestrationTracker(func() {
		emitEvent("worker:update", s.tracker.Summary())
	})

	k, err := s.buildKernel()
	if err != nil {
		slog.Error("ChatService: failed to build kernel", slog.Any("error", err))
		return fmt.Errorf("build kernel: %w", err)
	}
	if err := k.Boot(ctx); err != nil {
		slog.Error("ChatService: failed to boot kernel", slog.Any("error", err))
		return fmt.Errorf("boot kernel: %w", err)
	}
	s.k = k

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "interactive desktop assistant",
		Mode:         "orchestrator",
		TrustLevel:   s.cfg.trust,
		SystemPrompt: buildManagerPrompt(resolveWorkspace(s.cfg.workspace), s.cfg.workers),
		MaxSteps:     100,
	})
	if err != nil {
		slog.Error("ChatService: failed to create session", slog.Any("error", err))
		return fmt.Errorf("create session: %w", err)
	}
	s.sess = sess
	slog.Info("ChatService started successfully",
		slog.String("provider", s.cfg.provider),
		slog.String("model", s.cfg.model),
		slog.String("workspace", resolveWorkspace(s.cfg.workspace)),
	)
	return nil
}

func (s *ChatService) ServiceShutdown() error {
	slog.Info("ChatService shutting down...")
	s.mu.Lock()

	// 停止任何正在运行的 agent
	if s.cancel != nil {
		slog.Info("Cancelling running agent...")
		s.cancel()
	}

	// 等待 agent 完成运行（最多 5 秒）
	s.mu.Unlock()

	// 给后台任务一些时间来响应取消
	shutdown := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.mu.Lock()
				if !s.running {
					s.mu.Unlock()
					shutdown <- struct{}{}
					return
				}
				s.mu.Unlock()
			}
		}
	}()

	select {
	case <-shutdown:
		slog.Info("Agent stopped gracefully")
	case <-time.After(5 * time.Second):
		slog.Warn("Agent shutdown timeout, forcing shutdown")
	}

	// 关闭 kernel
	if s.k != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		slog.Info("Shutting down kernel...")
		if err := s.k.Shutdown(shutdownCtx); err != nil && err != context.DeadlineExceeded {
			slog.Error("Error shutting down kernel", slog.Any("error", err))
		}
	}

	slog.Info("ChatService shutdown complete")
	return nil
}

func (s *ChatService) SendMessage(content string) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("agent is already running")
	}
	if s.sess == nil || s.k == nil {
		s.mu.Unlock()
		return fmt.Errorf("service not initialized")
	}
	s.running = true
	s.mu.Unlock()

	slog.Info("SendMessage called", slog.String("content", truncate(content, 80)))
	s.sess.AppendMessage(port.Message{Role: port.RoleUser, Content: content})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("SendMessage goroutine panic", slog.Any("panic", r))
				emitEvent("chat:error", map[string]any{"message": fmt.Sprintf("internal error: %v", r)})
			}
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()

		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		s.cancel = cancel
		s.mu.Unlock()
		defer cancel()

		slog.Info("Starting RunWithUserIO...")
		result, err := s.k.RunWithUserIO(ctx, s.sess, s.wailsIO)
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("Agent run cancelled")
				emitEvent("chat:cancelled", map[string]any{"message": "已取消"})
				return
			}
			slog.Error("Agent run failed", slog.Any("error", err))
			emitEvent("chat:error", map[string]any{"message": err.Error()})
			return
		}

		slog.Info("Agent run completed",
			slog.String("session", result.SessionID),
			slog.Int("steps", result.Steps),
			slog.Int("tokens", result.TokensUsed.TotalTokens),
		)
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
	sess, err := s.k.NewSession(ctx, session.SessionConfig{
		Goal:         "interactive desktop assistant",
		Mode:         "orchestrator",
		TrustLevel:   s.cfg.trust,
		SystemPrompt: buildManagerPrompt(resolveWorkspace(s.cfg.workspace), s.cfg.workers),
		MaxSteps:     100,
	})
	if err != nil {
		return err
	}
	s.sess = sess
	return nil
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

// ─── Kernel Builder ─────────────────────────────────

func (s *ChatService) buildKernel() (*kernel.Kernel, error) {
	ctx := context.Background()
	k, err := appkit.BuildKernel(ctx, &appkit.AppFlags{
		Provider:  s.cfg.provider,
		Model:     s.cfg.model,
		Workspace: s.cfg.workspace,
		Trust:     s.cfg.trust,
		APIKey:    s.cfg.apiKey,
		BaseURL:   s.cfg.baseURL,
	}, s.wailsIO)
	if err != nil {
		return nil, err
	}

	if err := registerOrchestrationTools(k, ctx, s.cfg, s.tracker); err != nil {
		return nil, fmt.Errorf("register orchestration tools: %w", err)
	}

	if s.cfg.trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command"),
			builtins.DefaultAllow(),
		)
	}

	return k, nil
}

// ─── Orchestration ──────────────────────────────────

type taskInput struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type taskOutput struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Steps   int    `json:"steps"`
	Error   string `json:"error,omitempty"`
}

func registerOrchestrationTools(k *kernel.Kernel, ctx context.Context, cfg config, tracker *orchestrationTracker) error {
	delegateSpec := tool.ToolSpec{
		Name: "delegate_tasks",
		Description: `Delegate multiple sub-tasks to worker agents for parallel execution.
Each task gets its own independent agent session with full tool access.
Returns the result of each worker as an array.`,
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"tasks": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"id":          {"type": "string"},
							"description": {"type": "string"}
						},
						"required": ["id", "description"]
					}
				}
			},
			"required": ["tasks"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"orchestration"},
	}

	handler := makeDelegateHandler(k, ctx, cfg, tracker)
	return k.ToolRegistry().Register(delegateSpec, handler)
}

func makeDelegateHandler(k *kernel.Kernel, _ context.Context, cfg config, tracker *orchestrationTracker) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var req struct {
			Tasks []taskInput `json:"tasks"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if len(req.Tasks) == 0 {
			return json.Marshal([]taskOutput{})
		}

		emitEventOnCtx(ctx, "chat:progress", map[string]any{
			"message": fmt.Sprintf("Delegating %d task(s)...", len(req.Tasks)),
		})
		tracker.StartBatch(req.Tasks)

		results := make([]taskOutput, len(req.Tasks))
		sem := make(chan struct{}, cfg.workers)
		var wg sync.WaitGroup

		for i, task := range req.Tasks {
			wg.Add(1)
			go func(idx int, t taskInput) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				results[idx] = runWorker(ctx, k, cfg, t, tracker)
			}(i, task)
		}
		wg.Wait()

		tracker.FinishBatch(results)
		return json.Marshal(results)
	}
}

func runWorker(ctx context.Context, k *kernel.Kernel, cfg config, task taskInput, tracker *orchestrationTracker) taskOutput {
	tracker.StartWorker(task)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         task.Description,
		Mode:         "worker",
		TrustLevel:   cfg.trust,
		SystemPrompt: buildWorkerPrompt(resolveWorkspace(cfg.workspace)),
		MaxSteps:     30,
	})
	if err != nil {
		result := taskOutput{ID: task.ID, Success: false, Error: err.Error()}
		tracker.FinishWorker(result)
		return result
	}

	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: task.Description})

	result, err := k.Run(ctx, sess)
	if err != nil {
		r := taskOutput{ID: task.ID, Success: false, Error: err.Error()}
		tracker.FinishWorker(r)
		return r
	}

	r := taskOutput{
		ID:      task.ID,
		Success: result.Success,
		Output:  result.Output,
		Steps:   result.Steps,
		Error:   result.Error,
	}
	tracker.FinishWorker(r)
	return r
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

type logIO struct{ writer *os.File }

func (l *logIO) Send(_ context.Context, msg port.OutputMessage) error {
	switch msg.Type {
	case port.OutputText:
		fmt.Fprintln(l.writer, msg.Content)
	case port.OutputStream:
		fmt.Fprint(l.writer, msg.Content)
	case port.OutputStreamEnd:
		fmt.Fprintln(l.writer)
	case port.OutputToolStart:
		fmt.Fprintf(l.writer, "  🔧 %s\n", msg.Content)
	case port.OutputToolResult:
		if isErr, _ := msg.Meta["is_error"].(bool); isErr {
			fmt.Fprintf(l.writer, "  ❌ %s\n", msg.Content)
		}
	}
	return nil
}

func (l *logIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	if req.Type == port.InputConfirm {
		return port.InputResponse{Approved: true}, nil
	}
	return port.InputResponse{}, nil
}
