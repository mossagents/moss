package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/product"
	"github.com/mossagents/moss/appkit/runtime"
	configpkg "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/skill"
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
	APIType               string
	ProviderName          string
	Provider              string
	Model                 string
	Workspace             string
	Trust                 string
	ApprovalMode          string
	SessionStoreDir       string
	InitialSessionID      string
	BaseURL               string
	APIKey                string
	BaseObserver          port.Observer
	BuildKernel           func(wsDir, trust, approvalMode, apiType, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error)
	BuildRunTraceObserver func() (*product.RunTraceRecorder, port.Observer)
	AfterBoot             func(ctx context.Context, k *kernel.Kernel, io port.UserIO) error
	BuildSystemPrompt     func(workspace string) string
	BuildSessionConfig    func(workspace, trust, systemPrompt string) session.SessionConfig
	ScheduleController    runtime.ScheduleController
	SidebarTitle          string
	RenderSidebar         func() string
}

// kernelReadyMsg 表示 kernel 已初始化并启动，session 已创建。
type kernelReadyMsg struct {
	agent *agentState
}

// agentState 管理 kernel 和 session 的长生命周期状态（跨 Bubble Tea 值传递）。
// 使用指针共享，避免 Bubble Tea 值语义问题。
type agentState struct {
	k                     *kernel.Kernel
	sess                  *session.Session
	store                 session.SessionStore
	ctx                   context.Context
	cancel                context.CancelFunc
	runCancel             context.CancelFunc
	bridge                *BridgeIO
	workspace             string
	trust                 string
	approvalMode          string
	baseObserver          port.Observer
	buildRunTraceObserver func() (*product.RunTraceRecorder, port.Observer)
	buildSystemPrompt     func(workspace string) string
	buildSessionConfig    func(workspace, trust, systemPrompt string) session.SessionConfig
	permissions           map[string]string
	mu                    sync.Mutex
	running               bool // 是否正在执行 loop
}

func renderSkillsSummary(agent *agentState, workspace string) string {
	manifests := runtime.SkillManifests(agent.k)
	if len(manifests) == 0 {
		manifests = skill.DiscoverSkillManifests(workspace)
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Name < manifests[j].Name })

	var sb strings.Builder
	if len(manifests) == 0 {
		sb.WriteString("No user-installed SKILL.md skills were found.")
	} else {
		sb.WriteString("Discovered user skills:\n")
		for _, mf := range manifests {
			loaded := "inactive"
			if _, ok := runtime.SkillsManager(agent.k).Get(mf.Name); ok {
				loaded = "active"
			}
			if strings.TrimSpace(mf.Description) == "" {
				sb.WriteString(fmt.Sprintf("  • %s [%s]\n", mf.Name, loaded))
			} else {
				sb.WriteString(fmt.Sprintf("  • %s [%s] — %s\n", mf.Name, loaded, mf.Description))
			}
		}
	}

	builtinSet := make(map[string]struct{})
	for _, name := range runtime.RegisteredBuiltinToolNames(agent.k.Sandbox(), agent.k.Workspace(), agent.k.Executor()) {
		builtinSet[name] = struct{}{}
	}
	var builtinNames []string
	for _, spec := range agent.k.ToolRegistry().List() {
		if _, ok := builtinSet[spec.Name]; ok {
			builtinNames = append(builtinNames, spec.Name)
		}
	}
	sort.Strings(builtinNames)
	if len(builtinNames) > 0 {
		sb.WriteString("\n\nRuntime builtin tools:\n")
		for _, name := range builtinNames {
			sb.WriteString("  - " + name + "\n")
		}
	}

	return "```text\n" + sb.String() + "\n```"
}

func (a *agentState) sessionSummary() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess == nil {
		return "No active session."
	}
	dialogCount := 0
	for _, msg := range a.sess.Messages {
		if msg.Role != port.RoleSystem {
			dialogCount++
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Session: %s\n", a.sess.ID))
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
	if strings.TrimSpace(a.approvalMode) != "" {
		b.WriteString(fmt.Sprintf("\nApproval mode: %s", a.approvalMode))
	}
	return b.String()
}

func (a *agentState) listPersistedSessions(limit int) (string, error) {
	a.mu.Lock()
	store := a.store
	a.mu.Unlock()
	if store == nil {
		return "", fmt.Errorf("session store is unavailable")
	}
	if limit <= 0 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	summaries, err := store.List(ctx)
	if err != nil {
		return "", err
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].CreatedAt > summaries[j].CreatedAt })
	if len(summaries) > limit {
		summaries = summaries[:limit]
	}
	if len(summaries) == 0 {
		return "No persisted sessions found.", nil
	}
	var b strings.Builder
	b.WriteString("Persisted sessions:\n")
	for _, s := range summaries {
		b.WriteString(fmt.Sprintf("- %s | %s | %s | steps=%d | created=%s\n", s.ID, s.Status, s.Mode, s.Steps, s.CreatedAt))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (a *agentState) restoreSession(sessionID string) (string, error) {
	a.mu.Lock()
	store := a.store
	a.mu.Unlock()
	if store == nil {
		return "", fmt.Errorf("session store is unavailable")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	loaded, err := store.Load(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if loaded == nil {
		return "", fmt.Errorf("session %q not found", sessionID)
	}
	a.mu.Lock()
	a.sess = loaded
	a.mu.Unlock()
	return fmt.Sprintf("Restored session %s (%s, steps=%d, messages=%d).", loaded.ID, loaded.Status, loaded.Budget.UsedSteps, len(loaded.Messages)), nil
}

func (a *agentState) createInteractiveSession() (*session.Session, error) {
	a.mu.Lock()
	k := a.k
	ctx := a.ctx
	workspace := a.workspace
	trust := a.trust
	buildPrompt := a.buildSystemPrompt
	buildCfg := a.buildSessionConfig
	a.mu.Unlock()
	if k == nil {
		return nil, errors.New("runtime is unavailable")
	}
	sysPrompt := buildSystemPrompt(workspace)
	if buildPrompt != nil {
		sysPrompt = buildPrompt(workspace)
	}
	sessCfg := session.SessionConfig{
		Goal:         "interactive",
		Mode:         "interactive",
		TrustLevel:   trust,
		MaxSteps:     200,
		SystemPrompt: sysPrompt,
	}
	if buildCfg != nil {
		sessCfg = buildCfg(workspace, trust, sysPrompt)
		if sessCfg.SystemPrompt == "" {
			sessCfg.SystemPrompt = sysPrompt
		}
		if sessCfg.TrustLevel == "" {
			sessCfg.TrustLevel = trust
		}
		if sessCfg.Goal == "" {
			sessCfg.Goal = "interactive"
		}
		if sessCfg.Mode == "" {
			sessCfg.Mode = "interactive"
		}
		if sessCfg.MaxSteps == 0 {
			sessCfg.MaxSteps = 200
		}
	}
	return k.NewSession(ctx, sessCfg)
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

func (a *agentState) createCheckpoint(note string) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot create a checkpoint while a run is active")
	}
	k := a.k
	sess := a.sess
	ctx := a.ctx
	a.mu.Unlock()
	if k == nil || k.Checkpoints() == nil {
		return "", fmt.Errorf("checkpoint store is unavailable")
	}
	if sess == nil {
		return "", fmt.Errorf("active session is unavailable")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	record, err := k.CreateCheckpoint(reqCtx, sess, port.CheckpointCreateRequest{Note: strings.TrimSpace(note)})
	if err != nil {
		return "", err
	}
	summary := product.SummarizeCheckpoint(*record)
	msg := fmt.Sprintf("Created checkpoint %s for session %s.", summary.ID, sess.ID)
	if summary.SnapshotID != "" {
		msg += fmt.Sprintf(" snapshot=%s.", summary.SnapshotID)
	}
	msg += fmt.Sprintf(" patches=%d lineage=%d.", summary.PatchCount, summary.LineageDepth)
	if strings.TrimSpace(summary.Note) != "" {
		msg += fmt.Sprintf(" note=%s.", summary.Note)
	}
	return msg, nil
}

func (a *agentState) applyChange(patchFile, summary string) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot apply a change while a run is active")
	}
	k := a.k
	ctx := a.ctx
	workspace := a.workspace
	a.mu.Unlock()
	if k == nil {
		return "", fmt.Errorf("runtime is unavailable")
	}
	patchFile = strings.TrimSpace(patchFile)
	if patchFile == "" {
		return "", fmt.Errorf("patch file is required")
	}
	if !filepath.IsAbs(patchFile) {
		patchFile = filepath.Join(workspace, patchFile)
	}
	data, err := os.ReadFile(patchFile)
	if err != nil {
		return "", fmt.Errorf("read patch file: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	item, err := product.ApplyChange(reqCtx, product.ChangeRuntimeFromKernel(workspace, k), product.ApplyChangeRequest{
		Patch:   string(data),
		Summary: strings.TrimSpace(summary),
		Source:  port.PatchSourceUser,
	})
	if err != nil {
		var opErr *product.ChangeOperationError
		if errors.As(err, &opErr) && opErr.Operation != nil {
			return "", fmt.Errorf("%s\nDetails: %s", product.RenderChangeDetail(opErr.Operation), opErr.Error())
		}
		return "", err
	}
	return product.RenderChangeDetail(item), nil
}

func (a *agentState) rollbackChange(changeID string) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot roll back a change while a run is active")
	}
	k := a.k
	ctx := a.ctx
	workspace := a.workspace
	a.mu.Unlock()
	if k == nil {
		return "", fmt.Errorf("runtime is unavailable")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	item, err := product.RollbackChange(reqCtx, product.ChangeRuntimeFromKernel(workspace, k), product.RollbackChangeRequest{
		ChangeID: strings.TrimSpace(changeID),
	})
	if err != nil {
		var opErr *product.ChangeOperationError
		if errors.As(err, &opErr) && opErr.Operation != nil {
			return "", fmt.Errorf("%s\nDetails: %s", product.RenderChangeDetail(opErr.Operation), opErr.Error())
		}
		return "", err
	}
	return product.RenderChangeDetail(item), nil
}

func (a *agentState) forkSession(sourceKind, sourceID string, restoreWorktree bool) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot fork a session while a run is active")
	}
	current := a.sess
	store := a.store
	ctx := a.ctx
	k := a.k
	a.mu.Unlock()
	if k == nil {
		return "", fmt.Errorf("runtime is unavailable")
	}
	if sourceKind == "" {
		sourceKind = string(port.ForkSourceSession)
	}
	if strings.TrimSpace(sourceID) == "" {
		if sourceKind != string(port.ForkSourceSession) || current == nil {
			return "", fmt.Errorf("source id is required")
		}
		sourceID = current.ID
	}
	notice, err := autosaveSessionBeforeSwitch(current, store, ctx)
	if err != nil {
		return "", err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	next, result, err := k.ForkSession(reqCtx, port.ForkRequest{
		SourceKind:      port.ForkSourceKind(sourceKind),
		SourceID:        strings.TrimSpace(sourceID),
		RestoreWorktree: restoreWorktree,
	})
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sess = next
	a.mu.Unlock()
	var b strings.Builder
	if notice != "" {
		b.WriteString(notice)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Switched to forked session %s from %s %s.", next.ID, result.SourceKind, result.SourceID)
	if result.CheckpointID != "" {
		fmt.Fprintf(&b, " checkpoint=%s.", result.CheckpointID)
	}
	if result.RestoredWorktree {
		b.WriteString(" worktree restored.")
	}
	if result.Degraded && strings.TrimSpace(result.Details) != "" {
		fmt.Fprintf(&b, " degraded=%s.", result.Details)
	}
	return b.String(), nil
}

func (a *agentState) replayCheckpoint(checkpointID, mode string, restoreWorktree bool) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot replay a checkpoint while a run is active")
	}
	current := a.sess
	store := a.store
	ctx := a.ctx
	k := a.k
	a.mu.Unlock()
	if k == nil || k.Checkpoints() == nil {
		return "", fmt.Errorf("checkpoint store is unavailable")
	}
	checkpointID = strings.TrimSpace(checkpointID)
	if checkpointID == "" {
		return "", fmt.Errorf("checkpoint id is required")
	}
	replayMode := port.ReplayMode(strings.ToLower(strings.TrimSpace(mode)))
	if replayMode == "" {
		replayMode = port.ReplayModeResume
	}
	if replayMode != port.ReplayModeResume && replayMode != port.ReplayModeRerun {
		return "", fmt.Errorf("replay mode must be resume or rerun")
	}
	notice, err := autosaveSessionBeforeSwitch(current, store, ctx)
	if err != nil {
		return "", err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	next, result, err := k.ReplayFromCheckpoint(reqCtx, port.ReplayRequest{
		CheckpointID:    checkpointID,
		Mode:            replayMode,
		RestoreWorktree: restoreWorktree,
	})
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sess = next
	a.mu.Unlock()
	var b strings.Builder
	if notice != "" {
		b.WriteString(notice)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Switched to replay session %s from checkpoint %s (%s).", next.ID, result.CheckpointID, result.Mode)
	if result.RestoredWorktree {
		b.WriteString(" worktree restored.")
	}
	if result.Degraded && strings.TrimSpace(result.Details) != "" {
		fmt.Fprintf(&b, " degraded=%s.", result.Details)
	}
	return b.String(), nil
}

func (a *agentState) newSession() (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot create a new session while a run is active")
	}
	current := a.sess
	store := a.store
	ctx := a.ctx
	a.mu.Unlock()

	notice, err := autosaveSessionBeforeSwitch(current, store, ctx)
	if err != nil {
		return "", err
	}

	next, err := a.createInteractiveSession()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sess = next
	a.mu.Unlock()

	if notice != "" {
		return fmt.Sprintf("%s\nSwitched to new session %s.", notice, next.ID), nil
	}
	return fmt.Sprintf("Started new session %s.", next.ID), nil
}

func autosaveSessionBeforeSwitch(current *session.Session, store session.SessionStore, ctx context.Context) (string, error) {
	if current == nil || sessionDialogCount(current) == 0 {
		return "", nil
	}
	if store == nil {
		return "", errors.New("session store is unavailable, cannot auto-save current session before switching")
	}
	saveCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := store.Save(saveCtx, current); err != nil {
		return "", fmt.Errorf("save current session %q: %w", current.ID, err)
	}
	return fmt.Sprintf("Previous session %s auto-saved. Use /session restore %s or /sessions to continue it later.", current.ID, current.ID), nil
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
	defer a.mu.Unlock()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Trust: %s\n", a.trust))
	if strings.TrimSpace(a.approvalMode) != "" {
		b.WriteString(fmt.Sprintf("Approval mode: %s\n", a.approvalMode))
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
					_ = mc.IO.Send(ctx, port.OutputMessage{
						Type: port.OutputText,
						Content: port.FormatDeniedMessage(
							mc.Tool.Name,
							"permission override denied tool execution",
							"permission.override.deny",
							port.EnforcementHardBlock,
						),
					})
				}
				return builtins.ErrDenied
			case "ask":
				if mc.IO != nil {
					approval := &port.ApprovalRequest{
						ID:          fmt.Sprintf("approval-%d", time.Now().UnixNano()),
						Kind:        port.ApprovalKindTool,
						SessionID:   mc.Session.ID,
						ToolName:    mc.Tool.Name,
						Risk:        string(mc.Tool.Risk),
						Prompt:      "Allow tool " + mc.Tool.Name + "?",
						Reason:      "permission override requires approval",
						ReasonCode:  "permission.override.ask",
						Enforcement: port.EnforcementRequireApproval,
						Input:       append(json.RawMessage(nil), mc.Input...),
						RequestedAt: time.Now().UTC(),
					}
					approval.Prompt = port.FormatApprovalPrompt(approval)
					resp, err := mc.IO.Ask(ctx, port.InputRequest{
						Type:     port.InputConfirm,
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
						_ = mc.IO.Send(ctx, port.OutputMessage{
							Type: port.OutputText,
							Content: port.FormatDeniedMessage(
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
	b.WriteString(fmt.Sprintf("Tasks (%d):\n", payload.Count))
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
func (a *agentState) appendAndRun(text string) {
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

	a.sess.AppendMessage(port.Message{Role: port.RoleUser, Content: text})
	var traceRecorder *product.RunTraceRecorder
	if traceFactory != nil {
		recorder, runObserver := traceFactory()
		traceRecorder = recorder
		a.k.SetObserver(port.JoinObservers(baseObserver, runObserver))
	} else {
		a.k.SetObserver(baseObserver)
	}

	result, err := a.k.Run(runCtx, a.sess)
	a.k.SetObserver(baseObserver)

	a.mu.Lock()
	a.running = false
	a.runCancel = nil
	a.mu.Unlock()

	if a.bridge.program != nil {
		msg := sessionResultMsg{err: err}
		if result != nil {
			msg.output = result.Output
		}
		if traceRecorder != nil {
			trace := traceRecorder.Snapshot()
			steps := 0
			if result != nil {
				steps = result.Steps
			}
			msg.traceSummary = product.RenderRunTraceSummary(product.RunTraceSummary{
				Status:        runTraceStatus(result, err),
				Steps:         steps,
				Trace:         trace,
				Error:         runTraceError(result, err),
				CostAvailable: trace.EstimatedCostUSD > 0,
			})
		}
		a.bridge.program.Send(msg)
	}
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
		if msg.Role != port.RoleSystem {
			count++
		}
	}
	return count
}

// appModel 是顶层 Bubble Tea Model。
type appModel struct {
	state    appState
	welcome  welcomeModel
	chat     chatModel
	config   Config
	bridgeIO *BridgeIO
	agent    *agentState // 共享指针，跨值传递保持一致
	width    int
	height   int
	initCmd  tea.Cmd // 直接进入 chat 时的初始化命令
}

// Run 启动 TUI 应用。
func Run(cfg Config) error {
	bridge := NewBridgeIO()

	m := appModel{
		config:   cfg,
		bridgeIO: bridge,
	}

	// 如果 CLI 已提供足够配置，跳过 Welcome 直接进入 Chat
	defaultProvider := configpkg.NormalizeProviderIdentity(cfg.APIType, cfg.Provider, cfg.ProviderName)
	defaultAPIType := defaultProvider.EffectiveAPIType()
	defaultProviderName := defaultProvider.DisplayName()
	if defaultAPIType != "" && cfg.Workspace != "" {
		wCfg := WelcomeConfig{
			APIType:      defaultAPIType,
			ProviderName: defaultProviderName,
			Model:        cfg.Model,
			Workspace:    cfg.Workspace,
		}
		m.state = stateChat
		m.chat = newChatModel(configpkg.NormalizeProviderIdentity(wCfg.APIType, wCfg.Provider, wCfg.ProviderName).Label(), wCfg.Model, wCfg.Workspace)
		m.initCmd = initKernelCmd(cfg, wCfg, bridge)
	} else {
		m.state = stateWelcome
		m.welcome = newWelcomeModel(defaultAPIType, defaultProviderName, cfg.Model, cfg.Workspace)
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

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// 全局窗口大小
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = ws.Width
		m.height = ws.Height
	}

	switch m.state {
	case stateWelcome:
		return m.updateWelcome(msg)
	case stateChat:
		return m.updateChat(msg)
	}
	return m, nil
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
		m.config.APIType = cfg.APIType
		m.config.Provider = cfg.Provider
		m.config.ProviderName = cfg.ProviderName
		m.config.Model = cfg.Model
		m.config.Workspace = cfg.Workspace
		m.chat = newChatModel(configpkg.NormalizeProviderIdentity(cfg.APIType, cfg.Provider, cfg.ProviderName).Label(), cfg.Model, cfg.Workspace)
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

func (m appModel) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	// 取消并退出
	if _, ok := msg.(cancelMsg); ok {
		if m.agent != nil && m.agent.cancel != nil {
			m.agent.cancel()
		}
		return m, tea.Quit
	}

	// 切换模型：关闭旧 kernel，用新 model 重建
	if sm, ok := msg.(switchModelMsg); ok {
		if m.agent != nil && m.agent.cancel != nil {
			m.agent.cancel()
		}
		m.agent = nil
		m.chat.sendFn = nil

		// 更新 config 中的 model
		m.config.Model = sm.model
		identity := configpkg.NormalizeProviderIdentity(m.config.APIType, m.config.Provider, m.config.ProviderName)
		wCfg := WelcomeConfig{
			APIType:      identity.APIType,
			ProviderName: identity.Name,
			Provider:     identity.Provider,
			Model:        sm.model,
			Workspace:    m.config.Workspace,
		}
		m.chat.provider = identity.Label()
		m.chat.model = sm.model
		m.chat.trust = m.config.Trust
		m.chat.approvalMode = m.config.ApprovalMode
		return m, initKernelCmd(m.config, wCfg, m.bridgeIO)
	}

	// 切换 trust：关闭旧 kernel，用新 trust 重建
	if st, ok := msg.(switchTrustMsg); ok {
		if m.agent != nil && m.agent.cancel != nil {
			m.agent.cancel()
		}
		m.agent = nil
		m.chat.sendFn = nil
		m.config.Trust = st.trust
		identity := configpkg.NormalizeProviderIdentity(m.config.APIType, m.config.Provider, m.config.ProviderName)
		wCfg := WelcomeConfig{
			APIType:      identity.APIType,
			ProviderName: identity.Name,
			Provider:     identity.Provider,
			Model:        m.config.Model,
			Workspace:    m.config.Workspace,
		}
		m.chat.trust = st.trust
		m.chat.approvalMode = m.config.ApprovalMode
		return m, initKernelCmd(m.config, wCfg, m.bridgeIO)
	}

	if st, ok := msg.(switchApprovalMsg); ok {
		if m.agent != nil && m.agent.cancel != nil {
			m.agent.cancel()
		}
		m.agent = nil
		m.chat.sendFn = nil
		m.config.ApprovalMode = st.mode
		identity := configpkg.NormalizeProviderIdentity(m.config.APIType, m.config.Provider, m.config.ProviderName)
		wCfg := WelcomeConfig{
			APIType:      identity.APIType,
			ProviderName: identity.Name,
			Provider:     identity.Provider,
			Model:        m.config.Model,
			Workspace:    m.config.Workspace,
		}
		m.chat.approvalMode = st.mode
		return m, initKernelCmd(m.config, wCfg, m.bridgeIO)
	}

	// kernel 就绪：设置 sendFn 为多轮复用 session 的方式
	if ready, ok := msg.(kernelReadyMsg); ok {
		m.agent = ready.agent
		agent := ready.agent
		m.chat.sendFn = func(text string) {
			go agent.appendAndRun(text)
		}
		m.chat.cancelRunFn = agent.cancelCurrentRun
		m.chat.trust = m.config.Trust
		m.chat.approvalMode = m.config.ApprovalMode
		m.chat.sessionInfoFn = agent.sessionSummary
		m.chat.sessionListFn = func(limit int) (string, error) {
			return agent.listPersistedSessions(limit)
		}
		m.chat.changeListFn = func(limit int) (string, error) {
			return agent.listPersistedChanges(limit)
		}
		m.chat.changeShowFn = func(changeID string) (string, error) {
			return agent.showPersistedChange(changeID)
		}
		m.chat.applyChangeFn = func(patchFile, summary string) (string, error) {
			return agent.applyChange(patchFile, summary)
		}
		m.chat.rollbackChangeFn = func(changeID string) (string, error) {
			return agent.rollbackChange(changeID)
		}
		m.chat.checkpointListFn = func(limit int) (string, error) {
			return agent.listPersistedCheckpoints(limit)
		}
		m.chat.checkpointCreateFn = func(note string) (string, error) {
			return agent.createCheckpoint(note)
		}
		m.chat.checkpointForkFn = func(sourceKind, sourceID string, restoreWorktree bool) (string, error) {
			return agent.forkSession(sourceKind, sourceID, restoreWorktree)
		}
		m.chat.checkpointReplayFn = func(checkpointID, mode string, restoreWorktree bool) (string, error) {
			return agent.replayCheckpoint(checkpointID, mode, restoreWorktree)
		}
		m.chat.sessionRestoreFn = func(sessionID string) (string, error) {
			return agent.restoreSession(sessionID)
		}
		m.chat.newSessionFn = func() (string, error) {
			return agent.newSession()
		}
		m.chat.offloadFn = func(keepRecent int, note string) (string, error) {
			return agent.offloadContext(keepRecent, note)
		}
		m.chat.taskListFn = func(status string, limit int) (string, error) {
			return agent.listTasks(status, limit)
		}
		m.chat.taskQueryFn = func(taskID string) (string, error) {
			return agent.queryTask(taskID)
		}
		m.chat.taskCancelFn = func(taskID, reason string) (string, error) {
			return agent.cancelTask(taskID, reason)
		}
		m.chat.scheduleCtrl = m.config.ScheduleController
		m.chat.permissionSummaryFn = agent.permissionSummary
		m.chat.setPermissionFn = agent.setPermission
		m.chat.gitRunFn = func(cmd string, args []string) (string, error) {
			raw, err := agent.invokeTool(agent.ctx, "run_command", map[string]any{
				"command": cmd,
				"args":    args,
			})
			if err != nil {
				return "", err
			}
			return formatJSON(raw), nil
		}
		m.chat.skillListFn = func() string {
			return renderSkillsSummary(agent, m.config.Workspace)
		}
		connInfo := m.chat.provider
		if m.config.Model != "" {
			m.chat.model = m.config.Model
			connInfo += " (" + m.config.Model + ")"
		}
		if m.config.Trust != "" {
			connInfo += " [" + m.config.Trust + "]"
		}
		if strings.TrimSpace(m.config.ApprovalMode) != "" {
			connInfo += " {" + m.config.ApprovalMode + "}"
		}
		m.chat.streaming = false
		m.chat.messages = append(m.chat.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("Connected to %s", connInfo),
		})
		m.chat.refreshViewport()
		return m, nil
	}

	var cmd tea.Cmd
	m.chat, cmd = m.chat.Update(msg)
	return m, cmd
}

// initKernelCmd 异步创建 kernel + session。
func initKernelCmd(cfg Config, wCfg WelcomeConfig, bridge *BridgeIO) tea.Cmd {
	return func() tea.Msg {
		apiType := strings.ToLower(configpkg.NormalizeProviderIdentity(wCfg.APIType, wCfg.Provider, wCfg.ProviderName).EffectiveAPIType())

		k, err := cfg.BuildKernel(wCfg.Workspace, cfg.Trust, cfg.ApprovalMode, apiType, wCfg.Model, cfg.APIKey, cfg.BaseURL, bridge)
		if err != nil {
			return sessionResultMsg{err: fmt.Errorf("failed to initialize kernel: %w", err)}
		}

		ctx, cancel := context.WithCancel(context.Background())
		if err := k.Boot(ctx); err != nil {
			cancel()
			return sessionResultMsg{err: fmt.Errorf("failed to boot kernel: %w", err)}
		}
		var store session.SessionStore
		if strings.TrimSpace(cfg.SessionStoreDir) != "" {
			store, _ = session.NewFileStore(cfg.SessionStoreDir)
		}
		if cfg.AfterBoot != nil {
			if err := cfg.AfterBoot(ctx, k, bridge); err != nil {
				cancel()
				return sessionResultMsg{err: fmt.Errorf("failed to initialize runtime: %w", err)}
			}
		}

		var sess *session.Session
		if strings.TrimSpace(cfg.InitialSessionID) != "" {
			if store == nil {
				cancel()
				return sessionResultMsg{err: fmt.Errorf("failed to load session %q: session store is unavailable", cfg.InitialSessionID)}
			}
			sess, err = store.Load(ctx, cfg.InitialSessionID)
			if err != nil {
				cancel()
				return sessionResultMsg{err: fmt.Errorf("failed to load session %q: %w", cfg.InitialSessionID, err)}
			}
			if sess == nil {
				cancel()
				return sessionResultMsg{err: fmt.Errorf("session %q not found", cfg.InitialSessionID)}
			}
		} else {
			// 创建持久 session，注入 system prompt（Kernel 自动合并 skill additions）
			sysPrompt := buildSystemPrompt(wCfg.Workspace)
			if cfg.BuildSystemPrompt != nil {
				sysPrompt = cfg.BuildSystemPrompt(wCfg.Workspace)
			}
			sessCfg := session.SessionConfig{
				Goal:         "interactive",
				Mode:         "interactive",
				TrustLevel:   cfg.Trust,
				MaxSteps:     200,
				SystemPrompt: sysPrompt,
			}
			if cfg.BuildSessionConfig != nil {
				sessCfg = cfg.BuildSessionConfig(wCfg.Workspace, cfg.Trust, sysPrompt)
				if sessCfg.SystemPrompt == "" {
					sessCfg.SystemPrompt = sysPrompt
				}
				if sessCfg.TrustLevel == "" {
					sessCfg.TrustLevel = cfg.Trust
				}
				if sessCfg.Goal == "" {
					sessCfg.Goal = "interactive"
				}
				if sessCfg.Mode == "" {
					sessCfg.Mode = "interactive"
				}
				if sessCfg.MaxSteps == 0 {
					sessCfg.MaxSteps = 200
				}
			}

			sess, err = k.NewSession(ctx, sessCfg)
			if err != nil {
				cancel()
				return sessionResultMsg{err: fmt.Errorf("failed to create session: %w", err)}
			}
		}

		agent := &agentState{
			k:                     k,
			sess:                  sess,
			store:                 store,
			ctx:                   ctx,
			cancel:                cancel,
			bridge:                bridge,
			workspace:             wCfg.Workspace,
			trust:                 cfg.Trust,
			approvalMode:          cfg.ApprovalMode,
			baseObserver:          cfg.BaseObserver,
			buildRunTraceObserver: cfg.BuildRunTraceObserver,
			buildSystemPrompt:     cfg.BuildSystemPrompt,
			buildSessionConfig:    cfg.BuildSessionConfig,
			permissions:           map[string]string{},
		}

		k.Middleware().Use(agent.permissionOverrideMiddleware())

		return kernelReadyMsg{agent: agent}
	}
}
