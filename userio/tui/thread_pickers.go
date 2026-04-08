package tui

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/product"
	ckpt "github.com/mossagents/moss/kernel/checkpoint"
	kernelsession "github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"strings"
)

type resumePickerState struct {
	threads []product.ThreadBrowseSummary
	list    *selectionListState
}

func newResumePickerState(workspace string) (*resumePickerState, error) {
	threads, err := product.ListThreadBrowseSummaries(context.Background(), workspace, kernelsession.ThreadQuery{
		RecoverableOnly: true,
		Limit:           20,
	})
	if err != nil {
		return nil, err
	}
	items := make([]selectionListItem, 0, len(threads))
	for _, thread := range threads {
		items = append(items, selectionListItem{
			Key:    thread.Thread.SessionID,
			Title:  threadTitle(thread.Thread),
			Detail: fmt.Sprintf("%s · %s", thread.Thread.Status, valueOrDefaultString(thread.Thread.Profile, "default")),
		})
	}
	return &resumePickerState{
		threads: threads,
		list: &selectionListState{
			Title:        "Resume thread",
			Footer:       "↑↓ choose • Enter resume • Esc close",
			EmptyMessage: "No recoverable threads found.",
			Message:      "Browse recent recoverable threads using the shared session lineage catalog.",
			Items:        items,
		},
	}, nil
}

func (m chatModel) openResumePicker() (chatModel, tea.Cmd) {
	if m.sessionRestoreFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Resume is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	state, err := newResumePickerState(m.workspace)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list resumable threads: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	m.resumePicker = state
	m.openResumeOverlay()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) handleResumePickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.resumePicker == nil || m.resumePicker.list == nil || len(m.resumePicker.threads) == 0 {
		return m.closeResumeOverlay(), nil
	}
	switch msg.String() {
	case "up":
		m.resumePicker.list.Move(-1)
	case "down":
		m.resumePicker.list.Move(1)
	case "enter":
		idx := m.resumePicker.list.SelectedIndex()
		if idx >= 0 {
			thread := m.resumePicker.threads[idx].Thread
			m = m.closeResumeOverlay()
			return m.startThreadSwitch(fmt.Sprintf("Resuming thread %s...", thread.SessionID), func() (string, error) {
				out, err := m.sessionRestoreFn(thread.SessionID)
				if err != nil {
					return "", fmt.Errorf("failed to resume thread: %v", err)
				}
				return out, nil
			})
		}
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) renderResumePicker(width int) string {
	if m.resumePicker == nil || m.resumePicker.list == nil {
		return ""
	}
	if idx := m.resumePicker.list.SelectedIndex(); idx >= 0 && idx < len(m.resumePicker.threads) {
		m.resumePicker.list.Message = renderThreadBrowseSummary(m.resumePicker.threads[idx])
	}
	return renderSelectionListDialog(width, m.resumePicker.list)
}

type forkPickerState struct {
	sources []kernelsession.ForkSource
	restore bool
	list    *selectionListState
}

func newForkPickerState(workspace string) (*forkPickerState, error) {
	sources, err := product.ListForkSources(context.Background(), workspace, 12, 12)
	if err != nil {
		return nil, err
	}
	items := make([]selectionListItem, 0, len(sources))
	for _, source := range sources {
		items = append(items, selectionListItem{
			Key:    string(source.Kind) + ":" + source.SourceID,
			Title:  forkSourceTitle(source),
			Detail: forkSourceSubtitle(source),
		})
	}
	return &forkPickerState{
		sources: sources,
		list: &selectionListState{
			Title:        "Fork thread",
			Footer:       "↑↓ choose • R toggle restore • Enter fork • Esc close",
			EmptyMessage: "No fork sources available.",
			Message:      "Pick a thread or checkpoint to branch from.",
			Items:        items,
		},
	}, nil
}

func (m chatModel) openForkPicker() (chatModel, tea.Cmd) {
	if m.checkpointForkFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Fork is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	state, err := newForkPickerState(m.workspace)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to load fork sources: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	m.forkPicker = state
	m.openForkOverlay()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) handleForkPickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.forkPicker == nil || m.forkPicker.list == nil || len(m.forkPicker.sources) == 0 {
		return m.closeForkOverlay(), nil
	}
	switch msg.String() {
	case "up":
		m.forkPicker.list.Move(-1)
	case "down":
		m.forkPicker.list.Move(1)
	case "r":
		m.forkPicker.restore = !m.forkPicker.restore
	case "enter":
		idx := m.forkPicker.list.SelectedIndex()
		if idx >= 0 {
			source := m.forkPicker.sources[idx]
			restore := m.forkPicker.restore
			label := strings.TrimSpace(forkSourceTitle(source))
			m = m.closeForkOverlay()
			return m.startThreadSwitch(fmt.Sprintf("Forking from %s...", label), func() (string, error) {
				out, err := m.checkpointForkFn(string(source.Kind), source.SourceID, restore)
				if err != nil {
					return "", fmt.Errorf("failed to fork thread: %v", err)
				}
				return out, nil
			})
		}
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) renderForkPicker(width int) string {
	if m.forkPicker == nil || m.forkPicker.list == nil {
		return ""
	}
	if idx := m.forkPicker.list.SelectedIndex(); idx >= 0 && idx < len(m.forkPicker.sources) {
		detail := renderForkSourceDetail(m.forkPicker.sources[idx])
		detail += "\n\nRestore worktree: " + onOff(m.forkPicker.restore) + " (press R to toggle)"
		m.forkPicker.list.Message = detail
	}
	return renderSelectionListDialog(width, m.forkPicker.list)
}

type agentPickerState struct {
	tasks []taskrt.TaskSummary
	list  *selectionListState
}

func newAgentPickerState() (*agentPickerState, error) {
	tasks, err := product.ListTaskBrowseSummaries(context.Background(), taskrt.TaskQuery{Limit: 20})
	if err != nil {
		return nil, err
	}
	items := make([]selectionListItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, selectionListItem{
			Key:    task.Handle.ID,
			Title:  taskTitle(task),
			Detail: string(task.Status),
		})
	}
	return &agentPickerState{
		tasks: tasks,
		list: &selectionListState{
			Title:        "Agent threads",
			Footer:       "↑↓ choose • Enter send detail • C cancel • Esc close",
			EmptyMessage: "No agent tasks recorded.",
			Message:      "Browse background agent threads using the persisted task graph.",
			Items:        items,
		},
	}, nil
}

func (m chatModel) openAgentPicker() (chatModel, tea.Cmd) {
	state, err := newAgentPickerState()
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list agent threads: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	m.agentPicker = state
	m.openAgentOverlay()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) handleAgentPickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.agentPicker == nil || m.agentPicker.list == nil || len(m.agentPicker.tasks) == 0 {
		return m.closeAgentOverlay(), nil
	}
	idx := m.agentPicker.list.SelectedIndex()
	if idx < 0 || idx >= len(m.agentPicker.tasks) {
		return m.closeAgentOverlay(), nil
	}
	task := m.agentPicker.tasks[idx]
	switch msg.String() {
	case "up":
		m.agentPicker.list.Move(-1)
	case "down":
		m.agentPicker.list.Move(1)
	case "c":
		if m.taskCancelFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Agent thread cancellation is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		out, err := m.taskCancelFn(task.Handle.ID, "cancelled by user from agent picker")
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to cancel agent thread: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		return m.closeAgentOverlay(), nil
	case "enter":
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: renderTaskSummary(task)})
		return m.closeAgentOverlay(), nil
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) renderAgentPicker(width int) string {
	if m.agentPicker == nil || m.agentPicker.list == nil {
		return ""
	}
	if idx := m.agentPicker.list.SelectedIndex(); idx >= 0 && idx < len(m.agentPicker.tasks) {
		m.agentPicker.list.Message = renderTaskSummary(m.agentPicker.tasks[idx])
	}
	return renderSelectionListDialog(width, m.agentPicker.list)
}

func threadTitle(thread kernelsession.ThreadRef) string {
	return firstPopulatedString(thread.Preview, thread.Goal, thread.SessionID)
}

func renderThreadBrowseSummary(item product.ThreadBrowseSummary) string {
	thread := item.Thread
	var b strings.Builder
	fmt.Fprintf(&b, "Thread: %s\n", thread.SessionID)
	fmt.Fprintf(&b, "Status: %s\n", thread.Status)
	fmt.Fprintf(&b, "Profile: %s\n", valueOrDefaultString(thread.Profile, "default"))
	fmt.Fprintf(&b, "Trust: %s\n", valueOrDefaultString(thread.EffectiveTrust, "trusted"))
	fmt.Fprintf(&b, "Approval: %s\n", valueOrDefaultString(thread.EffectiveApproval, "confirm"))
	fmt.Fprintf(&b, "Snapshots: %d\n", item.SnapshotCount)
	if strings.TrimSpace(thread.Preview) != "" {
		fmt.Fprintf(&b, "Preview: %s\n", thread.Preview)
	}
	if strings.TrimSpace(thread.UpdatedAt) != "" {
		fmt.Fprintf(&b, "Updated: %s\n", thread.UpdatedAt)
	}
	return strings.TrimRight(b.String(), "\n")
}

func forkSourceTitle(source kernelsession.ForkSource) string {
	switch source.Kind {
	case ckpt.ForkSourceCheckpoint:
		return "checkpoint " + firstPopulatedString(source.CheckpointID, source.SourceID)
	default:
		return "thread " + firstPopulatedString(source.SessionID, source.SourceID)
	}
}

func forkSourceSubtitle(source kernelsession.ForkSource) string {
	if strings.TrimSpace(source.Label) != "" {
		return source.Label
	}
	if source.Kind == ckpt.ForkSourceCheckpoint {
		return valueOrDefaultString(source.CheckpointID, source.SourceID)
	}
	return valueOrDefaultString(source.SessionID, source.SourceID)
}

func renderForkSourceDetail(source kernelsession.ForkSource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Source: %s\n", forkSourceTitle(source))
	if strings.TrimSpace(source.Label) != "" {
		fmt.Fprintf(&b, "Label: %s\n", source.Label)
	}
	if strings.TrimSpace(source.SessionID) != "" {
		fmt.Fprintf(&b, "Session: %s\n", source.SessionID)
	}
	if strings.TrimSpace(source.CheckpointID) != "" {
		fmt.Fprintf(&b, "Checkpoint: %s\n", source.CheckpointID)
	}
	fmt.Fprintf(&b, "Lineage depth: %d", len(source.Lineage))
	return strings.TrimRight(b.String(), "\n")
}

func taskTitle(task taskrt.TaskSummary) string {
	return firstPopulatedString(task.Goal, task.AgentName, task.Handle.ID)
}

func firstPopulatedString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func renderTaskSummary(task taskrt.TaskSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\n", task.Handle.ID)
	fmt.Fprintf(&b, "Status: %s\n", task.Status)
	if strings.TrimSpace(task.AgentName) != "" {
		fmt.Fprintf(&b, "Agent: %s\n", task.AgentName)
	}
	if strings.TrimSpace(task.Goal) != "" {
		fmt.Fprintf(&b, "Goal: %s\n", task.Goal)
	}
	if strings.TrimSpace(task.Handle.SessionID) != "" {
		fmt.Fprintf(&b, "Session: %s\n", task.Handle.SessionID)
	}
	if strings.TrimSpace(task.Handle.ParentSessionID) != "" {
		fmt.Fprintf(&b, "Parent session: %s\n", task.Handle.ParentSessionID)
	}
	if strings.TrimSpace(task.Handle.JobID) != "" {
		fmt.Fprintf(&b, "Job: %s\n", task.Handle.JobID)
	}
	if len(task.DependsOn) > 0 {
		fmt.Fprintf(&b, "Depends on: %s\n", strings.Join(task.DependsOn, ", "))
	}
	if strings.TrimSpace(task.Error) != "" {
		fmt.Fprintf(&b, "Error: %s\n", task.Error)
	} else if strings.TrimSpace(task.Result) != "" {
		fmt.Fprintf(&b, "Result: %s\n", task.Result)
	}
	if len(task.Relations) > 0 {
		fmt.Fprintf(&b, "Relations: %d\n", len(task.Relations))
	}
	return strings.TrimRight(b.String(), "\n")
}
