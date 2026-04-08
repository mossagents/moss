package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/product"
	"github.com/mossagents/moss/appkit/runtime"
	configpkg "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	intr "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	mdl "github.com/mossagents/moss/kernel/model"
	kobs "github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/skill"
	"github.com/mossagents/moss/userio/prompting"
	"sort"
	"strings"
	"sync"
	"time"
)

const appVersion = "0.3.0"

// appState 表示 TUI 应用的状态。
type appState int

const (
	stateWelcome appState = iota
	stateChat
)

// Config 是启动 TUI 的配置。
type Config struct {
	ProviderName             string
	Provider                 string
	WelcomeBanner            string
	Model                    string
	Workspace                string
	Trust                    string
	Profile                  string
	ApprovalMode             string
	SessionStoreDir          string
	InitialSessionID         string
	BaseURL                  string
	APIKey                   string
	BaseObserver             kobs.Observer
	BuildKernel              func(wsDir, trust, approvalMode, profile, provider, model, apiKey, baseURL string, io intr.UserIO) (*kernel.Kernel, error)
	BuildRunTraceObserver    func() (*product.RunTraceRecorder, kobs.Observer)
	AfterBoot                func(ctx context.Context, k *kernel.Kernel, io intr.UserIO) error
	BuildSystemPrompt        func(workspace, trust string) string
	BuildSessionConfig       func(workspace, trust, approvalMode, profile, systemPrompt string) session.SessionConfig
	PromptConfigInstructions string
	PromptModelInstructions  string
	ScheduleController       runtime.ScheduleController
}

// kernelReadyMsg 表示 kernel 已初始化并启动，session 已创建。
type kernelReadyMsg struct {
	agent   *agentState
	notices []string
}

// agentState 管理 kernel 和 session 的长生命周期状态（跨 Bubble Tea 值传递）。
// 使用指针共享，避免 Bubble Tea 值语义问题。
type agentState struct {
	k                        *kernel.Kernel
	sess                     *session.Session
	store                    session.SessionStore
	ctx                      context.Context
	cancel                   context.CancelFunc
	runCancel                context.CancelFunc
	bridge                   *BridgeIO
	workspace                string
	trust                    string
	profile                  string
	approvalMode             string
	baseObserver             kobs.Observer
	buildRunTraceObserver    func() (*product.RunTraceRecorder, kobs.Observer)
	buildKernel              func(wsDir, trust, approvalMode, profile, provider, model, apiKey, baseURL string, io intr.UserIO) (*kernel.Kernel, error)
	afterBoot                func(ctx context.Context, k *kernel.Kernel, io intr.UserIO) error
	buildSystemPrompt        func(workspace, trust string) string
	buildSessionConfig       func(workspace, trust, approvalMode, profile, systemPrompt string) session.SessionConfig
	promptConfigInstructions string
	promptModelInstructions  string
	provider                 string
	model                    string
	apiKey                   string
	baseURL                  string
	permissions              map[string]string
	mu                       sync.Mutex
	running                  bool // 是否正在执行 loop
}

func renderSkillsSummary(agent *agentState, workspace string) string {
	manifests := runtime.SkillManifests(agent.k)
	if len(manifests) == 0 {
		manifests = skill.DiscoverSkillManifestsForTrust(workspace, agent.trust)
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Name < manifests[j].Name })

	var sb strings.Builder
	if len(manifests) == 0 {
		sb.WriteString("No user-installed SKILL.md skills were found.")
	} else {
		sb.WriteString("Discovered user skills:\n")
		for _, mf := range manifests {
			statusIcon := "[ ]"
			if _, ok := runtime.SkillsManager(agent.k).Get(mf.Name); ok {
				statusIcon = "[x]"
			}
			if strings.TrimSpace(mf.Description) == "" {
				sb.WriteString(fmt.Sprintf("  • %s %s\n", statusIcon, mf.Name))
			} else {
				sb.WriteString(fmt.Sprintf("  • %s %s — %s\n", statusIcon, mf.Name, mf.Description))
			}
		}
		sb.WriteString("\nDirect slash usage:\n")
		sb.WriteString("  /skill <name> <task...>\n")
		sb.WriteString("  /<skill_or_tool_name> <task...>")
	}

	return strings.TrimRight(sb.String(), "\n")
}

func discoveredSkillNames(agent *agentState, workspace string) []string {
	manifests := runtime.SkillManifests(agent.k)
	if len(manifests) == 0 {
		manifests = skill.DiscoverSkillManifestsForTrust(workspace, agent.trust)
	}
	names := make([]string, 0, len(manifests))
	seen := make(map[string]struct{}, len(manifests))
	for _, mf := range manifests {
		name := strings.TrimSpace(mf.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (a *agentState) sessionSummary() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess == nil {
		return "No active session."
	}
	dialogCount := 0
	for _, msg := range a.sess.Messages {
		if msg.Role != mdl.RoleSystem {
			dialogCount++
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Current thread: %s\n", a.sess.ID))
	b.WriteString(fmt.Sprintf("Status: %s\n", a.sess.Status))
	b.WriteString(fmt.Sprintf("Messages: %d (dialog: %d)\n", len(a.sess.Messages), dialogCount))
	b.WriteString(fmt.Sprintf("Budget: steps %d/%d, tokens %d/%d",
		a.sess.Budget.UsedSteps, a.sess.Budget.MaxSteps,
		a.sess.Budget.UsedTokens, a.sess.Budget.MaxTokens,
	))
	if v, ok := a.sess.GetState("last_offload_snapshot"); ok {
		b.WriteString(fmt.Sprintf("\nLast offload snapshot: %v", v))
	}
	if v, ok := a.sess.GetState("last_offload_at"); ok {
		b.WriteString(fmt.Sprintf("\nLast offload time: %v", v))
	}
	b.WriteString(fmt.Sprintf("\nTrust: %s", a.trust))
	if strings.TrimSpace(a.profile) != "" {
		b.WriteString(fmt.Sprintf("\nProfile: %s", a.profile))
	}
	if strings.TrimSpace(a.approvalMode) != "" {
		b.WriteString(fmt.Sprintf("\nApproval mode: %s", a.approvalMode))
	}
	if a.k != nil {
		policy := runtime.ExecutionPolicyOf(a.k)
		if len(policy.Command.Rules) > 0 || len(policy.HTTP.Rules) > 0 {
			b.WriteString(fmt.Sprintf("\nRules: command=%d http=%d", len(policy.Command.Rules), len(policy.HTTP.Rules)))
		}
	}
	return b.String()
}

func (a *agentState) promptDebugInfo() (baseSource, dynamicSections, sourceChain string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess == nil || len(a.sess.Config.Metadata) == 0 {
		return "", "", ""
	}
	if v, ok := a.sess.Config.Metadata[prompting.MetadataBaseSourceKey].(string); ok {
		baseSource = strings.TrimSpace(v)
	}
	if v, ok := a.sess.Config.Metadata[prompting.MetadataDynamicSectionsKey].(string); ok {
		dynamicSections = strings.TrimSpace(v)
	}
	if v, ok := a.sess.Config.Metadata[prompting.MetadataSourceChainKey].(string); ok {
		sourceChain = strings.TrimSpace(v)
	}
	return baseSource, dynamicSections, sourceChain
}

func (a *agentState) refreshSystemPrompt() error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("cannot refresh the thread prompt while a run is active")
	}
	sess := a.sess
	store := a.store
	ctx := a.ctx
	workspace := a.workspace
	trust := a.trust
	buildPrompt := a.buildSystemPrompt
	a.mu.Unlock()
	if sess == nil {
		return errors.New("active thread is unavailable")
	}
	nextPrompt, err := prompting.ComposeSystemPrompt(workspace, trust, a.k, a.promptConfigInstructions, a.promptModelInstructions, sess.Config.Metadata)
	if err != nil {
		return err
	}
	if buildPrompt != nil {
		nextPrompt = buildPrompt(workspace, trust)
	}
	a.mu.Lock()
	sess.Config.SystemPrompt = nextPrompt
	a.mu.Unlock()
	sess.UpdateSystemPrompt(nextPrompt)
	if store != nil {
		saveCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := store.Save(saveCtx, sess); err != nil {
			return fmt.Errorf("save updated thread prompt: %w", err)
		}
	}
	return nil
}

func (a *agentState) listPersistedCheckpoints(limit int) (string, error) {
	a.mu.Lock()
	k := a.k
	ctx := a.ctx
	a.mu.Unlock()
	if k == nil || k.Checkpoints() == nil {
		return "", fmt.Errorf("checkpoint store is unavailable")
	}
	if limit <= 0 {
		limit = 20
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	items, err := k.Checkpoints().List(reqCtx)
	if err != nil {
		return "", err
	}
	summaries := product.SummarizeCheckpoints(items)
	if len(summaries) > limit {
		summaries = summaries[:limit]
	}
	return product.RenderCheckpointSummaries(summaries), nil
}

func (a *agentState) showPersistedCheckpoint(checkpointID string) (string, error) {
	a.mu.Lock()
	k := a.k
	ctx := a.ctx
	a.mu.Unlock()
	if k == nil || k.Checkpoints() == nil {
		return "", fmt.Errorf("checkpoint store is unavailable")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	detail, err := product.LoadCheckpointWithStore(reqCtx, k.Checkpoints(), checkpointID)
	if err != nil {
		return "", err
	}
	return product.RenderCheckpointDetail(detail), nil
}

func (a *agentState) listPersistedChanges(limit int) (string, error) {
	a.mu.Lock()
	ctx := a.ctx
	workspace := a.workspace
	a.mu.Unlock()
	if limit <= 0 {
		limit = 20
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	items, err := product.ListChangeOperations(reqCtx, workspace, limit)
	if err != nil {
		return "", err
	}
	return product.RenderChangeSummaries(items), nil
}

func (a *agentState) showPersistedChange(changeID string) (string, error) {
	a.mu.Lock()
	ctx := a.ctx
	workspace := a.workspace
	a.mu.Unlock()
	changeID = strings.TrimSpace(changeID)
	if changeID == "" {
		return "", fmt.Errorf("change id is required")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	item, err := product.LoadChangeOperation(reqCtx, workspace, changeID)
	if err != nil {
		return "", err
	}
	return product.RenderChangeDetail(item), nil
}

func (a *agentState) setPermission(toolName, mode string) (string, error) {
	toolName = strings.TrimSpace(toolName)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if toolName == "" {
		return "", fmt.Errorf("tool name is required")
	}
	switch mode {
	case "allow", "ask", "deny", "reset":
	default:
		return "", fmt.Errorf("mode must be allow|ask|deny|reset")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.permissions == nil {
		a.permissions = map[string]string{}
	}
	if mode == "reset" {
		delete(a.permissions, toolName)
		return fmt.Sprintf("Permission reset for %s.", toolName), nil
	}
	a.permissions[toolName] = mode
	return fmt.Sprintf("Permission updated: %s -> %s", toolName, mode), nil
}

func (a *agentState) permissionSummary() string {
	a.mu.Lock()
	k := a.k
	defer a.mu.Unlock()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Trust: %s\n", a.trust))
	if strings.TrimSpace(a.approvalMode) != "" {
		b.WriteString(fmt.Sprintf("Approval mode: %s\n", a.approvalMode))
	}
	if k != nil {
		policy := runtime.ExecutionPolicyOf(k)
		if len(policy.Command.Rules) > 0 {
			b.WriteString("Command rules:\n")
			for _, rule := range policy.Command.Rules {
				label := strings.TrimSpace(rule.Name)
				if label == "" {
					label = strings.TrimSpace(rule.Match)
				}
				b.WriteString(fmt.Sprintf("- %s => %s (%s)\n", label, rule.Access, rule.Match))
			}
		}
		if len(policy.HTTP.Rules) > 0 {
			b.WriteString("HTTP rules:\n")
			for _, rule := range policy.HTTP.Rules {
				label := strings.TrimSpace(rule.Name)
				if label == "" {
					label = strings.TrimSpace(rule.Match)
				}
				methods := strings.Join(rule.Methods, ",")
				if methods == "" {
					methods = "*"
				}
				b.WriteString(fmt.Sprintf("- %s => %s (%s methods=%s)\n", label, rule.Access, rule.Match, methods))
			}
		}
	}
	if len(a.permissions) == 0 {
		b.WriteString("Overrides: none")
		return b.String()
	}
	keys := make([]string, 0, len(a.permissions))
	for k := range a.permissions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("Overrides:\n")
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("- %s: %s\n", k, a.permissions[k]))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (a *agentState) permissionOverrideMiddleware() middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase == middleware.BeforeToolCall && mc.Tool != nil {
			a.mu.Lock()
			mode := a.permissions[mc.Tool.Name]
			a.mu.Unlock()
			switch mode {
			case "deny":
				if mc.IO != nil {
					_ = mc.IO.Send(ctx, intr.OutputMessage{
						Type: intr.OutputText,
						Content: intr.FormatDeniedMessage(
							mc.Tool.Name,
							"permission override denied tool execution",
							"permission.override.deny",
							intr.EnforcementHardBlock,
						),
					})
				}
				return builtins.ErrDenied
			case "ask":
				if mc.IO != nil {
					approval := &intr.ApprovalRequest{
						ID:          fmt.Sprintf("approval-%d", time.Now().UnixNano()),
						Kind:        intr.ApprovalKindTool,
						SessionID:   mc.Session.ID,
						ToolName:    mc.Tool.Name,
						Risk:        string(mc.Tool.Risk),
						Prompt:      "Allow tool " + mc.Tool.Name + "?",
						Reason:      "permission override requires approval",
						ReasonCode:  "permission.override.ask",
						Enforcement: intr.EnforcementRequireApproval,
						Input:       append(json.RawMessage(nil), mc.Input...),
						RequestedAt: time.Now().UTC(),
					}
					approval.Prompt = intr.FormatApprovalPrompt(approval)
					resp, err := mc.IO.Ask(ctx, intr.InputRequest{
						Type:     intr.InputConfirm,
						Prompt:   approval.Prompt,
						Approval: approval,
						Meta: map[string]any{
							"tool":        mc.Tool.Name,
							"input":       mc.Input,
							"approval_id": approval.ID,
							"reason":      approval.Reason,
							"reason_code": approval.ReasonCode,
							"risk":        approval.Risk,
						},
					})
					if err != nil {
						return err
					}
					if resp.Decision != nil {
						resp.Approved = resp.Decision.Approved
					}
					if !resp.Approved {
						_ = mc.IO.Send(ctx, intr.OutputMessage{
							Type: intr.OutputText,
							Content: intr.FormatDeniedMessage(
								mc.Tool.Name,
								approval.Reason,
								approval.ReasonCode,
								approval.Enforcement,
							),
						})
						return builtins.ErrDenied
					}
				}
				return next(ctx)
			case "allow":
				return next(ctx)
			}
		}
		return next(ctx)
	}
}

func (a *agentState) invokeTool(ctx context.Context, name string, input any) (json.RawMessage, error) {
	_, handler, ok := a.k.ToolRegistry().Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %q not available in current runtime", name)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal tool input: %w", err)
	}
	return handler(ctx, raw)
}

func formatJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

func (a *agentState) offloadContext(keepRecent int, note string) (string, error) {
	a.mu.Lock()
	sess := a.sess
	a.mu.Unlock()
	if sess == nil {
		return "", errors.New("no active session")
	}
	if keepRecent <= 0 {
		keepRecent = 20
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	raw, err := a.invokeTool(ctx, "offload_context", map[string]any{
		"session_id":  sess.ID,
		"keep_recent": keepRecent,
		"note":        note,
	})
	if err != nil {
		return "", err
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		return formatJSON(raw), nil
	}
	status, _ := resp["status"].(string)
	switch status {
	case "noop":
		return fmt.Sprintf("No offload needed: conversation length does not exceed keep_recent=%d.", keepRecent), nil
	case "offloaded":
		return "Context offload completed.\n" + formatJSON(raw), nil
	default:
		return formatJSON(raw), nil
	}
}

type taskView struct {
	ID        string `json:"id"`
	AgentName string `json:"agent_name"`
	Goal      string `json:"goal"`
	Status    string `json:"status"`
	Error     string `json:"error"`
}

func formatTaskList(raw json.RawMessage) string {
	var payload struct {
		Tasks []taskView `json:"tasks"`
		Count int        `json:"count"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return formatJSON(raw)
	}
	if len(payload.Tasks) == 0 {
		return "No matching background tasks."
	}
	sort.Slice(payload.Tasks, func(i, j int) bool { return payload.Tasks[i].ID < payload.Tasks[j].ID })
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Agent threads (%d):\n", payload.Count))
	for _, t := range payload.Tasks {
		line := fmt.Sprintf("- %s | %s | %s", t.ID, t.AgentName, t.Status)
		if t.Goal != "" {
			line += " | " + t.Goal
		}
		if t.Error != "" {
			line += " | err: " + t.Error
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (a *agentState) listTasks(status string, limit int) (string, error) {
	if limit <= 0 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	input := map[string]any{
		"status": strings.TrimSpace(status),
		"limit":  limit,
	}
	raw, err := a.invokeTool(ctx, "list_tasks", input)
	if err != nil {
		raw, err = a.invokeTool(ctx, "task", map[string]any{
			"mode":   "list",
			"status": strings.TrimSpace(status),
			"limit":  limit,
		})
		if err != nil {
			return "", err
		}
	}
	return formatTaskList(raw), nil
}

func (a *agentState) queryTask(taskID string) (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	raw, err := a.invokeTool(ctx, "query_agent", map[string]any{"task_id": taskID})
	if err != nil {
		raw, err = a.invokeTool(ctx, "task", map[string]any{
			"mode":    "query",
			"task_id": taskID,
		})
		if err != nil {
			return "", err
		}
	}
	return formatJSON(raw), nil
}

func (a *agentState) cancelTask(taskID, reason string) (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	raw, err := a.invokeTool(ctx, "cancel_task", map[string]any{
		"task_id": taskID,
		"reason":  reason,
	})
	if err != nil {
		raw, err = a.invokeTool(ctx, "task", map[string]any{
			"mode":    "cancel",
			"task_id": taskID,
			"reason":  reason,
		})
		if err != nil {
			return "", err
		}
	}
	return formatJSON(raw), nil
}

// appendAndRun 追加用户消息到 session 并重新执行 agent loop。
func (a *agentState) appendAndRun(text string, parts []mdl.ContentPart) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return // 防止重复执行
	}
	a.running = true
	runCtx, runCancel := context.WithCancel(a.ctx)
	a.runCancel = runCancel
	baseObserver := a.baseObserver
	traceFactory := a.buildRunTraceObserver
	a.mu.Unlock()

	if len(parts) == 0 {
		parts = []mdl.ContentPart{mdl.TextPart(text)}
	}
	a.sess.AppendMessage(mdl.Message{Role: mdl.RoleUser, ContentParts: parts})
	var traceRecorder *product.RunTraceRecorder
	progressObserver := newExecutionProgressObserver(a.bridge, a.sess)
	if traceFactory != nil {
		recorder, runObserver := traceFactory()
		traceRecorder = recorder
		a.k.SetObserver(kobs.JoinObservers(baseObserver, runObserver, runtime.ObserverForStateCatalog(a.k), progressObserver))
	} else {
		a.k.SetObserver(kobs.JoinObservers(baseObserver, runtime.ObserverForStateCatalog(a.k), progressObserver))
	}

	result, err := a.k.Run(runCtx, a.sess)
	a.k.SetObserver(kobs.JoinObservers(baseObserver, runtime.ObserverForStateCatalog(a.k)))

	a.mu.Lock()
	a.running = false
	a.runCancel = nil
	a.mu.Unlock()

	if a.bridge.program != nil {
		msg := sessionResultMsg{err: err}
		if result != nil {
			msg.output = result.Output
			msg.outputMedia = collectOutputMediaParts(a.sess)
		}
		if traceRecorder != nil {
			trace := traceRecorder.Snapshot()
			steps := 0
			if result != nil {
				steps = result.Steps
			}
			traceSummary := &product.RunTraceSummary{
				Status:        runTraceStatus(result, err),
				Steps:         steps,
				Trace:         trace,
				Error:         runTraceError(result, err),
				CostAvailable: trace.EstimatedCostUSD > 0,
			}
			msg.trace = traceSummary
			msg.traceSummary = product.RenderRunTraceSummary(*traceSummary)
		}
		a.bridge.program.Send(msg)
	}
}

func collectOutputMediaParts(sess *session.Session) []mdl.ContentPart {
	if sess == nil || len(sess.Messages) == 0 {
		return nil
	}
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		msg := sess.Messages[i]
		if msg.Role != mdl.RoleAssistant {
			continue
		}
		var out []mdl.ContentPart
		for _, p := range msg.ContentParts {
			if p.Type == mdl.ContentPartOutputImage || p.Type == mdl.ContentPartOutputAudio || p.Type == mdl.ContentPartOutputVideo {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}

func (a *agentState) publishProgressReplay() {
	a.mu.Lock()
	k := a.k
	sess := a.sess
	bridge := a.bridge
	a.mu.Unlock()
	publishProgressReplay(bridge, k, sess)
}

func runTraceStatus(result *loop.SessionResult, err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if err != nil {
		return "failed"
	}
	if result == nil || result.Success {
		return "completed"
	}
	return "failed"
}

func runTraceError(result *loop.SessionResult, err error) string {
	if err != nil {
		return err.Error()
	}
	if result != nil {
		return result.Error
	}
	return ""
}

func (a *agentState) cancelCurrentRun() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running || a.runCancel == nil {
		return false
	}
	a.runCancel()
	return true
}

func sessionDialogCount(sess *session.Session) int {
	if sess == nil {
		return 0
	}
	count := 0
	for _, msg := range sess.Messages {
		if msg.Role != mdl.RoleSystem {
			count++
		}
	}
	return count
}

// appModel 是顶层 Bubble Tea Model。
type appModel struct {
	state               appState
	welcome             welcomeModel
	chat                chatModel
	config              Config
	bridgeIO            *BridgeIO
	agent               *agentState // 共享指针，跨值传递保持一致
	width               int
	height              int
	initCmd             tea.Cmd // 直接进入 chat 时的初始化命令
	postInitDisplayText string
	postInitRunText     string
	theme               string
}

func (m *appModel) configureChatShell() {
	m.chat.startupBanner = m.config.WelcomeBanner
	m.chat.setProviderIdentity(m.config.Provider, m.config.ProviderName)
}

// Run 启动 TUI 应用。
func Run(cfg Config) error {
	bridge := NewBridgeIO()
	if provider, providerName, model, ok := persistedModelOverride(); ok {
		cfg.Provider = provider
		cfg.ProviderName = providerName
		cfg.Model = model
	}

	m := appModel{
		config:   cfg,
		bridgeIO: bridge,
	}

	// 如果 CLI 已提供足够配置，跳过 Welcome 直接进入 Chat
	defaultProvider := configpkg.NormalizeProviderIdentity("", cfg.Provider, cfg.ProviderName)
	defaultProviderID := defaultProvider.EffectiveAPIType()
	defaultProviderName := defaultProvider.DisplayName()
	if defaultProviderID != "" && cfg.Workspace != "" {
		wCfg := WelcomeConfig{
			Provider:     defaultProviderID,
			ProviderName: defaultProviderName,
			Model:        cfg.Model,
			Workspace:    cfg.Workspace,
		}
		m.state = stateChat
		theme := m.theme
		m.chat = newChatModel(configpkg.NormalizeProviderIdentity("", wCfg.Provider, wCfg.ProviderName).Label(), wCfg.Model, wCfg.Workspace)
		m.chat.modelAuto = !hasPersistedModelOverride()
		m.configureChatShell()
		if strings.TrimSpace(theme) != "" {
			m.chat.theme = theme
			applyTheme(theme)
		}
		m.theme = m.chat.theme
		m.initCmd = initKernelCmd(cfg, wCfg, bridge)
	} else {
		m.state = stateWelcome
		m.welcome = newWelcomeModel(defaultProviderID, defaultProviderName, cfg.Model, cfg.Workspace, cfg.WelcomeBanner)
	}

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	bridge.SetProgram(p)

	_, err := p.Run()
	return err
}

func (m appModel) Init() tea.Cmd {
	if m.state == stateChat {
		// 跳过 Welcome 直接进入 Chat，同时启动 textarea 光标闪烁和 kernel 初始化
		if strings.TrimSpace(m.config.Trust) != "" {
			m.chat.trust = m.config.Trust
		}
		if strings.TrimSpace(m.config.ApprovalMode) != "" {
			m.chat.approvalMode = m.config.ApprovalMode
		}
		return tea.Batch(m.chat.Init(), m.initCmd)
	}
	return m.welcome.Init()
}

func (m appModel) View() string {
	switch m.state {
	case stateWelcome:
		return m.welcome.View()
	case stateChat:
		return m.chat.View()
	}
	return ""
}

func (m appModel) updateWelcome(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.welcome, cmd = m.welcome.Update(msg)

	if m.welcome.cancelled {
		return m, tea.Quit
	}

	if m.welcome.confirmed {
		cfg := m.welcome.config()
		// 持久化用户选择的 provider/model 到 ~/.moss/config.yaml
		saveWelcomeConfig(cfg)
		m.config.Provider = cfg.Provider
		m.config.ProviderName = cfg.ProviderName
		m.config.Model = cfg.Model
		m.config.Workspace = cfg.Workspace
		theme := m.theme
		m.chat = newChatModel(configpkg.NormalizeProviderIdentity("", cfg.Provider, cfg.ProviderName).Label(), cfg.Model, cfg.Workspace)
		m.chat.modelAuto = !hasPersistedModelOverride()
		m.configureChatShell()
		if strings.TrimSpace(theme) != "" {
			m.chat.theme = theme
			applyTheme(theme)
		}
		m.theme = m.chat.theme
		m.chat.approvalMode = m.config.ApprovalMode
		m.state = stateChat

		// 将当前窗口尺寸传递给 chatModel，避免它因未收到 WindowSizeMsg 而卡在 "加载中"
		if m.width > 0 && m.height > 0 {
			m.chat.width = m.width
			m.chat.height = m.height
			m.chat.recalcLayout()
		}

		return m, initKernelCmd(m.config, cfg, m.bridgeIO)
	}

	return m, cmd
}

func (m appModel) stopAgentForKernelRebuild() appModel {
	if m.agent != nil && m.agent.cancel != nil {
		m.agent.cancel()
	}
	m.agent = nil
	m.chat.sendFn = nil
	return m
}

func (m appModel) welcomeConfigForModelSelection(provider, providerName, model string) (WelcomeConfig, string) {
	identity := configpkg.NormalizeProviderIdentity("", provider, providerName)
	return WelcomeConfig{
		ProviderName: identity.Name,
		Provider:     identity.Provider,
		Model:        model,
		Workspace:    m.config.Workspace,
	}, identity.Label()
}

func (m appModel) rebuildKernelWithSelection(provider, providerName, model string) (tea.Model, tea.Cmd) {
	wCfg, providerLabel := m.welcomeConfigForModelSelection(provider, providerName, model)
	m.chat.provider = providerLabel
	m.chat.providerID = strings.TrimSpace(wCfg.Provider)
	m.chat.providerName = strings.TrimSpace(wCfg.ProviderName)
	m.chat.model = model
	m.chat.trust = m.config.Trust
	m.chat.profile = m.config.Profile
	m.chat.approvalMode = m.config.ApprovalMode
	return m, initKernelCmd(m.config, wCfg, m.bridgeIO)
}

func (m appModel) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateChatCore(msg)
}
