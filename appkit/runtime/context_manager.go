package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

const (
	contextStateVersion           = 1
	contextSummaryFragmentID      = "context:summary"
	contextStartupSessionID       = "startup:session"
	contextStartupStateCatalogID  = "startup:state_catalog"
	contextStartupMemoryID        = "startup:memory"
	contextStartupWorkspaceID     = "startup:workspace"
	contextStartupRepoID          = "startup:repo"
	contextSummaryFragmentKind    = "summary"
	contextStartupFragmentKind    = "startup"
	contextBaselineFragmentPrefix = "baseline:"
)

func preparePromptContext(ctx context.Context, k *kernel.Kernel, st *contextState, sess *session.Session) (session.PromptContextState, []port.Message, int, error) {
	state := session.ReadPromptContextState(sess)
	state.Version = contextStateVersion
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
	state.StartupFragments = buildStartupFragments(ctx, k, sess, state.StartupBudget)
	currentPrompt := session.BuildPromptMessages(sess.Messages, state)
	currentTokens := session.EstimateMessagesTokens(currentPrompt)
	if shouldCompactPrompt(sess, state, st, currentTokens) {
		session.WritePromptContextState(sess, state)
		if _, err := compactSessionContext(ctx, st.store, sess, state.KeepRecent, "auto compact", k.LLM(), true); err != nil {
			return state, nil, 0, err
		}
		state = session.ReadPromptContextState(sess)
		state.BaselineFragments = buildBaselineFragments(sess.Messages)
		state.StartupFragments = buildStartupFragments(ctx, k, sess, state.StartupBudget)
		currentPrompt = session.BuildPromptMessages(sess.Messages, state)
		currentTokens = session.EstimateMessagesTokens(currentPrompt)
	}
	changed, hashes := session.ComputePromptFragmentDiff(state.FragmentHashes, session.FlattenPromptContextFragments(state))
	state.FragmentHashes = hashes
	state.LastFragmentDiff = changed
	state.LastPromptTokens = currentTokens
	state.LastPromptBuiltAt = time.Now().UTC()
	session.WritePromptContextState(sess, state)
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
