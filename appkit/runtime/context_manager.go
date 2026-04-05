package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
)

const (
	contextStateVersion           = 1
	contextSummaryFragmentID      = "context:summary"
	contextRealtimeSnapshotKey    = "context_realtime_snapshot"
	contextStartupSessionID       = "startup:session"
	contextStartupStateCatalogID  = "startup:state_catalog"
	contextStartupMemoryID        = "startup:memory"
	contextStartupWorkspaceID     = "startup:workspace"
	contextStartupRepoID          = "startup:repo"
	contextRealtimeRepoID         = "realtime:repo"
	contextRealtimeWorkspaceID    = "realtime:workspace"
	contextSummaryFragmentKind    = "summary"
	contextStartupFragmentKind    = "startup"
	contextRealtimeFragmentKind   = "realtime"
	contextBaselineFragmentPrefix = "baseline:"
)

func preparePromptContext(ctx context.Context, k *kernel.Kernel, st *contextState, sess *session.Session) (session.PromptContextState, []port.Message, int, error) {
	state := session.ReadPromptContextState(sess)
	state.Version = contextStateVersion
	lightweightChat := session.LatestUserTurnIsLightweightChat(sess.Messages)
	if st.maxPromptTokens > 0 {
		state.PromptBudget = st.maxPromptTokens
	}
	if st.startupTokens > 0 {
		state.StartupBudget = st.startupTokens
	}
	if st.keepRecent > 0 {
		state.KeepRecent = st.keepRecent
	}
	state.BaselineFragments = buildBaselineFragments(sess.Messages)
	if lightweightChat {
		state.StartupFragments = nil
		state.DynamicFragments = nil
	} else {
		state.StartupFragments = buildStartupFragments(ctx, k, sess, state.StartupBudget)
		state.DynamicFragments = append(filterFragmentsByKind(state.DynamicFragments, contextSummaryFragmentKind), buildRealtimeFragments(ctx, k, sess)...)
	}
	currentPrompt := session.BuildPromptMessages(sess.Messages, state)
	currentTokens := session.EstimateMessagesTokens(currentPrompt)
	if !lightweightChat && shouldCompactPrompt(sess, state, st, currentTokens) {
		session.WritePromptContextState(sess, state)
		if _, err := compactSessionContext(ctx, st.store, sess, state.KeepRecent, "auto compact", k.LLM(), true); err != nil {
			return state, nil, 0, err
		}
		state = session.ReadPromptContextState(sess)
		state.BaselineFragments = buildBaselineFragments(sess.Messages)
		state.StartupFragments = buildStartupFragments(ctx, k, sess, state.StartupBudget)
		state.DynamicFragments = append(filterFragmentsByKind(state.DynamicFragments, contextSummaryFragmentKind), buildRealtimeFragments(ctx, k, sess)...)
		currentPrompt = session.BuildPromptMessages(sess.Messages, state)
		currentTokens = session.EstimateMessagesTokens(currentPrompt)
	}
	changed, hashes := session.ComputePromptFragmentDiff(state.FragmentHashes, session.FlattenPromptContextFragments(state))
	state.FragmentHashes = hashes
	state.LastFragmentDiff = changed
	state.LastPromptTokens = currentTokens
	state.LastPromptBuiltAt = time.Now().UTC()
	session.WritePromptContextState(sess, state)
	logging.GetLogger().DebugContext(ctx, "prompt context prepared",
		"session_id", sess.ID,
		"prompt_tokens", currentTokens,
		"baseline_fragments", len(state.BaselineFragments),
		"startup_fragments", len(state.StartupFragments),
		"dynamic_fragments", len(state.DynamicFragments),
		"fragment_diff", strings.Join(state.LastFragmentDiff, ","),
	)
	return state, currentPrompt, currentTokens, nil
}

func compactSessionContext(
	ctx context.Context,
	store session.SessionStore,
	sess *session.Session,
	keepRecent int,
	note string,
	llm port.LLM,
	withSummary bool,
) (map[string]any, error) {
	if sess == nil {
		return nil, fmt.Errorf("session is required")
	}
	dialogCount := countDialogMessages(sess.Messages)
	if keepRecent <= 0 {
		keepRecent = 20
	}
	if dialogCount <= keepRecent {
		return map[string]any{
			"status":       "noop",
			"session_id":   sess.ID,
			"dialog_count": dialogCount,
			"keep_recent":  keepRecent,
		}, nil
	}
	original := append([]port.Message(nil), sess.Messages...)
	snapshotID := fmt.Sprintf("%s_context_%d", sess.ID, time.Now().UnixNano())
	summaryText := strings.TrimSpace(note)
	if withSummary {
		summaryText = buildSummary(ctx, llm, messagesBeforeDialogTail(original, keepRecent))
		if summaryText == "" {
			summaryText = "Earlier context compacted into a structured summary."
		}
		if strings.TrimSpace(note) != "" {
			summaryText += " Note: " + strings.TrimSpace(note)
		}
	}
	if !withSummary && summaryText == "" {
		summaryText = fmt.Sprintf("Context offloaded to snapshot %s.", snapshotID)
	}
	snapshot := &session.Session{
		ID:       snapshotID,
		Status:   session.StatusCompleted,
		Config:   sess.Config,
		Messages: original,
		State: map[string]any{
			"context_snapshot_of": sess.ID,
			"context_summary":     summaryText,
			"context_keep_recent": keepRecent,
			"context_mode":        map[bool]string{true: "summary", false: "offload"}[withSummary],
			"note":                note,
		},
		Budget:    sess.Budget.Clone(),
		CreatedAt: time.Now(),
		EndedAt:   time.Now(),
	}
	session.MarkHistoryHidden(snapshot)
	if store != nil {
		if err := store.Save(ctx, snapshot); err != nil {
			return nil, fmt.Errorf("save context snapshot: %w", err)
		}
	}
	state := session.ReadPromptContextState(sess)
	state.Version = contextStateVersion
	state.BaselineFragments = buildBaselineFragments(sess.Messages)
	state.KeepRecent = keepRecent
	state.CompactedDialogCount = maxInt(0, dialogCount-keepRecent)
	state.LastSnapshotID = snapshotID
	state.LastSummary = summaryText
	state.DynamicFragments = []session.PromptContextFragment{
		newSummaryFragment(snapshotID, summaryText, withSummary),
	}
	changed, hashes := session.ComputePromptFragmentDiff(state.FragmentHashes, session.FlattenPromptContextFragments(state))
	state.FragmentHashes = hashes
	state.LastFragmentDiff = changed
	state.LastPromptBuiltAt = time.Now().UTC()
	session.WritePromptContextState(sess, state)
	if withSummary {
		sess.SetState("last_context_snapshot", snapshotID)
		sess.SetState("last_context_summary", summaryText)
		sess.SetState("last_context_offload_at", time.Now().Format(time.RFC3339))
	} else {
		sess.SetState("last_offload_snapshot", snapshotID)
		sess.SetState("last_offload_at", time.Now().Format(time.RFC3339))
	}
	if store != nil {
		if err := store.Save(ctx, sess); err != nil {
			return nil, fmt.Errorf("save compacted session: %w", err)
		}
	}
	logging.GetLogger().DebugContext(ctx, "prompt context compacted",
		"session_id", sess.ID,
		"snapshot_session", snapshotID,
		"dialog_before", dialogCount,
		"keep_recent", keepRecent,
		"with_summary", withSummary,
	)
	return map[string]any{
		"status":                   "offloaded",
		"session_id":               sess.ID,
		"snapshot_session":         snapshotID,
		"dialog_before":            dialogCount,
		"kept_recent":              keepRecent,
		"compacted_dialog_count":   state.CompactedDialogCount,
		"message_count_now":        len(session.BuildPromptMessages(sess.Messages, state)),
		"full_history_messages":    len(sess.Messages),
		"summary":                  summaryText,
		"last_prompt_fragment_ids": append([]string(nil), state.LastFragmentDiff...),
	}, nil
}

func shouldCompactPrompt(sess *session.Session, state session.PromptContextState, st *contextState, promptTokens int) bool {
	if sess == nil {
		return false
	}
	if st.triggerTokens > 0 && promptTokens >= st.triggerTokens {
		return true
	}
	if st.triggerDialog > 0 && countDialogMessages(sess.Messages) >= st.triggerDialog {
		return true
	}
	if st.maxPromptTokens > 0 && promptTokens > st.maxPromptTokens && countDialogMessages(sess.Messages) > state.KeepRecent {
		return true
	}
	return false
}

func buildBaselineFragments(messages []port.Message) []session.PromptContextFragment {
	fragments := make([]session.PromptContextFragment, 0, len(messages))
	for i, msg := range messages {
		if msg.Role != port.RoleSystem {
			continue
		}
		text := strings.TrimSpace(port.ContentPartsToPlainText(msg.ContentParts))
		if text == "" {
			continue
		}
		kind := string(classifyContextFragment(msg))
		fragments = append(fragments, session.NewPromptContextFragment(
			fmt.Sprintf("%s%d", contextBaselineFragmentPrefix, i),
			kind,
			port.RoleSystem,
			kind,
			text,
		))
	}
	return fragments
}

func newSummaryFragment(snapshotID, summary string, withSummary bool) session.PromptContextFragment {
	label := "context_offload"
	title := "Context offload snapshot"
	if withSummary {
		label = "context_summary"
		title = "Compacted conversation summary"
	}
	body := strings.TrimSpace(summary)
	if snapshotID != "" {
		body = fmt.Sprintf("Snapshot: %s\n\n%s", snapshotID, body)
	}
	return session.NewPromptContextFragment(
		contextSummaryFragmentID,
		contextSummaryFragmentKind,
		port.RoleSystem,
		title,
		session.FormatPromptContextFragment(label, body),
	)
}

func messagesBeforeDialogTail(messages []port.Message, keepRecent int) []port.Message {
	dialogCount := countDialogMessages(messages)
	cut := dialogCount - keepRecent
	if cut <= 0 {
		return append([]port.Message(nil), messages...)
	}
	seenDialog := 0
	out := make([]port.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == port.RoleSystem {
			out = append(out, msg)
			continue
		}
		seenDialog++
		if seenDialog > cut {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func buildStartupFragments(ctx context.Context, k *kernel.Kernel, sess *session.Session, budget int) []session.PromptContextFragment {
	if k == nil || sess == nil || budget == 0 {
		return nil
	}
	candidates := []session.PromptContextFragment{
		buildSessionStartupFragment(sess),
		buildRepoStartupFragment(ctx, k),
		buildMemoryStartupFragment(ctx, k),
		buildStateCatalogStartupFragment(k, sess),
		buildWorkspaceStartupFragment(ctx, k),
	}
	filtered := make([]session.PromptContextFragment, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Text) != "" {
			filtered = append(filtered, candidate)
		}
	}
	if budget < 0 {
		return filtered
	}
	return takeFragmentsWithinBudget(filtered, budget)
}

func buildSessionStartupFragment(sess *session.Session) session.PromptContextFragment {
	lines := []string{
		fmt.Sprintf("Session: %s", strings.TrimSpace(sess.ID)),
		fmt.Sprintf("Status: %s", strings.TrimSpace(string(sess.Status))),
		fmt.Sprintf("Goal: %s", strings.TrimSpace(sess.Config.Goal)),
		fmt.Sprintf("Budget used: steps=%d tokens=%d", sess.Budget.UsedStepsValue(), sess.Budget.UsedTokensValue()),
	}
	if raw, ok := sess.GetState("last_context_snapshot"); ok {
		lines = append(lines, fmt.Sprintf("Last context snapshot: %v", raw))
	}
	if raw, ok := sess.GetState("last_context_summary"); ok {
		lines = append(lines, fmt.Sprintf("Last summary: %s", trimText(fmt.Sprint(raw), 240)))
	}
	return session.NewPromptContextFragment(
		contextStartupSessionID,
		contextStartupFragmentKind,
		port.RoleSystem,
		"Session runtime context",
		session.FormatPromptContextFragment("startup_session_context", strings.Join(lines, "\n")),
	)
}

func buildRepoStartupFragment(ctx context.Context, k *kernel.Kernel) session.PromptContextFragment {
	capture := k.RepoStateCapture()
	if capture == nil {
		return session.PromptContextFragment{}
	}
	state, err := capture.Capture(ctx)
	if err != nil || state == nil {
		return session.PromptContextFragment{}
	}
	lines := []string{
		fmt.Sprintf("Repo root: %s", strings.TrimSpace(state.RepoRoot)),
		fmt.Sprintf("Branch: %s", firstNonEmpty(strings.TrimSpace(state.Branch), "(detached)")),
		fmt.Sprintf("Dirty: %t", state.IsDirty),
	}
	if len(state.Untracked) > 0 {
		lines = append(lines, fmt.Sprintf("Untracked: %s", strings.Join(limitStrings(state.Untracked, 6), ", ")))
	}
	return session.NewPromptContextFragment(
		contextStartupRepoID,
		contextStartupFragmentKind,
		port.RoleSystem,
		"Repository state",
		session.FormatPromptContextFragment("startup_repo_state", strings.Join(lines, "\n")),
	)
}

func buildMemoryStartupFragment(ctx context.Context, k *kernel.Kernel) session.PromptContextFragment {
	ws := k.Workspace()
	if ws == nil {
		return session.PromptContextFragment{}
	}
	paths := []string{"MEMORY.md", "memory_summary.md", "README.md", "AGENTS.md"}
	chunks := make([]string, 0, len(paths))
	for _, path := range paths {
		data, err := ws.ReadFile(ctx, path)
		if err != nil {
			continue
		}
		text := trimText(string(data), 500)
		if strings.TrimSpace(text) == "" {
			continue
		}
		chunks = append(chunks, fmt.Sprintf("[%s]\n%s", path, text))
	}
	if len(chunks) == 0 {
		return session.PromptContextFragment{}
	}
	return session.NewPromptContextFragment(
		contextStartupMemoryID,
		contextStartupFragmentKind,
		port.RoleSystem,
		"Workspace memory artifacts",
		session.FormatPromptContextFragment("startup_memory_context", strings.Join(chunks, "\n\n")),
	)
}

func buildStateCatalogStartupFragment(k *kernel.Kernel, sess *session.Session) session.PromptContextFragment {
	catalog := StateCatalogOf(k)
	if catalog == nil || !catalog.Enabled() || sess == nil {
		return session.PromptContextFragment{}
	}
	page, err := catalog.Query(StateQuery{
		SessionID: sess.ID,
		Kinds:     []StateKind{StateKindMemory, StateKindTask, StateKindJob, StateKindJobItem, StateKindCheckpoint, StateKindExecutionEvent},
		Limit:     6,
	})
	if err != nil || len(page.Items) == 0 {
		return session.PromptContextFragment{}
	}
	lines := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		line := fmt.Sprintf("- [%s/%s] %s", item.Kind, firstNonEmpty(item.Status, "active"), strings.TrimSpace(item.Title))
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			line += " - " + summary
		}
		lines = append(lines, line)
	}
	return session.NewPromptContextFragment(
		contextStartupStateCatalogID,
		contextStartupFragmentKind,
		port.RoleSystem,
		"Recent runtime state",
		session.FormatPromptContextFragment("startup_state_catalog", strings.Join(lines, "\n")),
	)
}

func buildWorkspaceStartupFragment(ctx context.Context, k *kernel.Kernel) session.PromptContextFragment {
	ws := k.Workspace()
	if ws == nil {
		return session.PromptContextFragment{}
	}
	files, err := ws.ListFiles(ctx, "*")
	if err != nil || len(files) == 0 {
		return session.PromptContextFragment{}
	}
	sort.Strings(files)
	return session.NewPromptContextFragment(
		contextStartupWorkspaceID,
		contextStartupFragmentKind,
		port.RoleSystem,
		"Top-level workspace files",
		session.FormatPromptContextFragment("startup_workspace_map", strings.Join(limitStrings(files, 12), "\n")),
	)
}

type realtimeContextSnapshot struct {
	Repo      repoRealtimeState                `json:"repo,omitempty"`
	Workspace map[string]workspaceRealtimeFile `json:"workspace,omitempty"`
}

type repoRealtimeState struct {
	Root      string   `json:"root,omitempty"`
	Branch    string   `json:"branch,omitempty"`
	Dirty     bool     `json:"dirty,omitempty"`
	Untracked []string `json:"untracked,omitempty"`
}

type workspaceRealtimeFile struct {
	Size    int64 `json:"size,omitempty"`
	ModUnix int64 `json:"mod_unix,omitempty"`
}

func buildRealtimeFragments(ctx context.Context, k *kernel.Kernel, sess *session.Session) []session.PromptContextFragment {
	if k == nil || sess == nil {
		return nil
	}
	previous := readRealtimeSnapshot(sess)
	current := realtimeContextSnapshot{
		Repo:      captureRepoRealtimeState(ctx, k),
		Workspace: captureWorkspaceRealtimeState(ctx, k),
	}
	writeRealtimeSnapshot(sess, current)
	fragments := make([]session.PromptContextFragment, 0, 2)
	if fragment := buildRepoRealtimeFragment(previous.Repo, current.Repo); strings.TrimSpace(fragment.Text) != "" {
		fragments = append(fragments, fragment)
	}
	if fragment := buildWorkspaceRealtimeFragment(previous.Workspace, current.Workspace); strings.TrimSpace(fragment.Text) != "" {
		fragments = append(fragments, fragment)
	}
	return fragments
}

func readRealtimeSnapshot(sess *session.Session) realtimeContextSnapshot {
	raw, ok := sess.GetState(contextRealtimeSnapshotKey)
	if !ok || raw == nil {
		return realtimeContextSnapshot{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return realtimeContextSnapshot{}
	}
	var snapshot realtimeContextSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return realtimeContextSnapshot{}
	}
	if snapshot.Workspace == nil {
		snapshot.Workspace = make(map[string]workspaceRealtimeFile)
	}
	return snapshot
}

func writeRealtimeSnapshot(sess *session.Session, snapshot realtimeContextSnapshot) {
	if sess == nil {
		return
	}
	if snapshot.Workspace == nil {
		snapshot.Workspace = make(map[string]workspaceRealtimeFile)
	}
	sess.SetState(contextRealtimeSnapshotKey, snapshot)
}

func captureRepoRealtimeState(ctx context.Context, k *kernel.Kernel) repoRealtimeState {
	capture := k.RepoStateCapture()
	if capture == nil {
		return repoRealtimeState{}
	}
	state, err := capture.Capture(ctx)
	if err != nil || state == nil {
		return repoRealtimeState{}
	}
	return repoRealtimeState{
		Root:      strings.TrimSpace(state.RepoRoot),
		Branch:    strings.TrimSpace(state.Branch),
		Dirty:     state.IsDirty,
		Untracked: append([]string(nil), limitStrings(state.Untracked, 8)...),
	}
}

func buildRepoRealtimeFragment(previous, current repoRealtimeState) session.PromptContextFragment {
	if isZeroRepoRealtimeState(current) || isZeroRepoRealtimeState(previous) {
		return session.PromptContextFragment{}
	}
	lines := make([]string, 0, 4)
	if previous.Branch != current.Branch {
		lines = append(lines, fmt.Sprintf("Branch changed: %s -> %s", firstNonEmpty(previous.Branch, "(detached)"), firstNonEmpty(current.Branch, "(detached)")))
	}
	if previous.Dirty != current.Dirty {
		lines = append(lines, fmt.Sprintf("Dirty changed: %t -> %t", previous.Dirty, current.Dirty))
	}
	if strings.Join(previous.Untracked, ",") != strings.Join(current.Untracked, ",") {
		lines = append(lines, fmt.Sprintf("Untracked: %s", strings.Join(limitStrings(current.Untracked, 6), ", ")))
	}
	if len(lines) == 0 {
		return session.PromptContextFragment{}
	}
	return session.NewPromptContextFragment(
		contextRealtimeRepoID,
		contextRealtimeFragmentKind,
		port.RoleSystem,
		"Repository changes since last turn",
		session.FormatPromptContextFragment("realtime_repo_changes", strings.Join(lines, "\n")),
	)
}

func isZeroRepoRealtimeState(state repoRealtimeState) bool {
	return strings.TrimSpace(state.Root) == "" &&
		strings.TrimSpace(state.Branch) == "" &&
		!state.Dirty &&
		len(state.Untracked) == 0
}

func captureWorkspaceRealtimeState(ctx context.Context, k *kernel.Kernel) map[string]workspaceRealtimeFile {
	ws := k.Workspace()
	if ws == nil {
		return nil
	}
	files, err := ws.ListFiles(ctx, "*")
	if err != nil || len(files) == 0 {
		return nil
	}
	sort.Strings(files)
	if len(files) > 64 {
		files = files[:64]
	}
	state := make(map[string]workspaceRealtimeFile, len(files))
	for _, file := range files {
		info, err := ws.Stat(ctx, file)
		if err != nil {
			continue
		}
		stamp := workspaceRealtimeFile{Size: info.Size}
		if !info.ModTime.IsZero() {
			stamp.ModUnix = info.ModTime.UTC().UnixNano()
		}
		state[file] = stamp
	}
	return state
}

func buildWorkspaceRealtimeFragment(previous, current map[string]workspaceRealtimeFile) session.PromptContextFragment {
	if len(previous) == 0 || len(current) == 0 {
		return session.PromptContextFragment{}
	}
	added := make([]string, 0)
	changed := make([]string, 0)
	removed := make([]string, 0)
	for path, stamp := range current {
		prev, ok := previous[path]
		switch {
		case !ok:
			added = append(added, path)
		case prev != stamp:
			changed = append(changed, path)
		}
	}
	for path := range previous {
		if _, ok := current[path]; !ok {
			removed = append(removed, path)
		}
	}
	if len(added) == 0 && len(changed) == 0 && len(removed) == 0 {
		return session.PromptContextFragment{}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)
	lines := make([]string, 0, 3)
	if len(added) > 0 {
		lines = append(lines, "Added: "+strings.Join(limitStrings(added, 6), ", "))
	}
	if len(changed) > 0 {
		lines = append(lines, "Changed: "+strings.Join(limitStrings(changed, 6), ", "))
	}
	if len(removed) > 0 {
		lines = append(lines, "Removed: "+strings.Join(limitStrings(removed, 6), ", "))
	}
	return session.NewPromptContextFragment(
		contextRealtimeWorkspaceID,
		contextRealtimeFragmentKind,
		port.RoleSystem,
		"Workspace changes since last turn",
		session.FormatPromptContextFragment("realtime_workspace_changes", strings.Join(lines, "\n")),
	)
}

func filterFragmentsByKind(fragments []session.PromptContextFragment, kind string) []session.PromptContextFragment {
	if len(fragments) == 0 {
		return nil
	}
	out := make([]session.PromptContextFragment, 0, len(fragments))
	for _, fragment := range fragments {
		if strings.TrimSpace(fragment.Kind) == kind {
			out = append(out, fragment)
		}
	}
	return out
}

func takeFragmentsWithinBudget(fragments []session.PromptContextFragment, budget int) []session.PromptContextFragment {
	if budget <= 0 || len(fragments) == 0 {
		return nil
	}
	out := make([]session.PromptContextFragment, 0, len(fragments))
	used := 0
	for _, fragment := range fragments {
		cost := fragment.Tokens
		if cost <= 0 {
			cost = session.EstimateTextTokens(fragment.Text)
		}
		if len(out) > 0 && used+cost > budget {
			break
		}
		used += cost
		out = append(out, fragment)
	}
	return out
}

func trimText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return strings.TrimSpace(text[:limit-3]) + "..."
}

func limitStrings(items []string, limit int) []string {
	if limit <= 0 || len(items) <= limit {
		return append([]string(nil), items...)
	}
	out := append([]string(nil), items[:limit]...)
	out = append(out, fmt.Sprintf("... +%d more", len(items)-limit))
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
