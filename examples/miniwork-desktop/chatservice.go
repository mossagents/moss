package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/appkit"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/tool"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ─── ChatService ────────────────────────────────────

// ChatService 是 Wails 服务，桥接 moss kernel 与桌面前端。
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

// NewChatService 创建 ChatService 实例。
func NewChatService(cfg config) *ChatService {
	return &ChatService{
		cfg: cfg,
	}
}

// ServiceStartup 在 Wails 应用启动时被调用。
func (s *ChatService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	s.wailsIO = NewWailsUserIO()
	s.tracker = newOrchestrationTracker(func() {
		emitEvent("worker:update", s.tracker.Summary())
	})

	k, err := s.buildKernel()
	if err != nil {
		return fmt.Errorf("build kernel: %w", err)
	}

	if err := k.Boot(ctx); err != nil {
		return fmt.Errorf("boot kernel: %w", err)
	}

	s.k = k

	// 创建初始 Session
	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "interactive desktop assistant",
		Mode:         "orchestrator",
		TrustLevel:   s.cfg.trust,
		SystemPrompt: buildManagerPrompt(resolveWorkspace(s.cfg.workspace), s.cfg.workers),
		MaxSteps:     100,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	s.sess = sess

	return nil
}

// ServiceShutdown 在 Wails 应用关闭时被调用。
func (s *ChatService) ServiceShutdown() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.k != nil {
		s.k.Shutdown(context.Background())
	}
	return nil
}

// SendMessage 接收用户消息并异步运行 agent loop。
// 前端通过事件接收流式输出。
func (s *ChatService) SendMessage(content string) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("agent is already running")
	}
	s.running = true
	s.mu.Unlock()

	// 追加用户消息
	s.sess.AppendMessage(port.Message{Role: port.RoleUser, Content: content})

	// 异步运行 agent loop
	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()

		ctx, cancel := context.WithCancel(application.Get().Context())
		s.mu.Lock()
		s.cancel = cancel
		s.mu.Unlock()
		defer cancel()

		result, err := s.k.RunWithUserIO(ctx, s.sess, s.wailsIO)
		if err != nil {
			if ctx.Err() != nil {
				emitEvent("chat:cancelled", nil)
				return
			}
			emitEvent("chat:error", map[string]any{
				"message": err.Error(),
			})
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

// SendMessageWithAttachments 发送带有附件的消息。
// attachments 是已上传到 workspace 的文件路径列表。
func (s *ChatService) SendMessageWithAttachments(content string, attachments []string) error {
	if len(attachments) > 0 {
		content += "\n\n📎 Attached files:\n"
		for _, path := range attachments {
			content += fmt.Sprintf("- %s\n", path)
		}
	}
	return s.SendMessage(content)
}

// StopAgent 中止当前正在运行的 agent。
func (s *ChatService) StopAgent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// RespondToAsk 响应 kernel 的 Ask 请求。
func (s *ChatService) RespondToAsk(value string, approved bool) {
	s.wailsIO.RespondToAsk(port.InputResponse{
		Value:    value,
		Approved: approved,
	})
}

// NewSession 创建新的对话会话。
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

// GetConfig 返回当前配置。
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

// IsRunning 返回 agent 是否正在运行。
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

// ─── Orchestration Tools ────────────────────────────

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
Workers can read/write files, run commands, and search code.
Returns the result of each worker as an array.`,
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"tasks": {
					"type": "array",
					"description": "Array of task descriptions for workers to execute",
					"items": {
						"type": "object",
						"properties": {
							"id":          {"type": "string", "description": "Unique task identifier"},
							"description": {"type": "string", "description": "Detailed description of what the worker should do"}
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

		// 并行执行，限制并发数
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

	sess.AppendMessage(port.Message{
		Role:    port.RoleUser,
		Content: task.Description,
	})

	result, err := k.Run(ctx, sess)
	if err != nil {
		workerResult := taskOutput{ID: task.ID, Success: false, Error: err.Error()}
		tracker.FinishWorker(workerResult)
		return workerResult
	}

	workerResult := taskOutput{
		ID:      task.ID,
		Success: result.Success,
		Output:  result.Output,
		Steps:   result.Steps,
		Error:   result.Error,
	}
	tracker.FinishWorker(workerResult)
	return workerResult
}

// ─── Helpers ────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// logIO 是一个只输出不交互的 UserIO 实现。
type logIO struct {
	writer *os.File
}

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
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			fmt.Fprintf(l.writer, "  ❌ %s\n", msg.Content)
		}
	}
	return nil
}

func (l *logIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	if req.Type == port.InputConfirm {
		return port.InputResponse{Approved: true}, nil
	}
	return port.InputResponse{Value: ""}, nil
}
