package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// transcriptOverlayState 保存全屏对话历史覆盖层的状态。
type transcriptOverlayState struct {
	viewport viewport.Model
	ready    bool
	width    int
	height   int
}

// newTranscriptOverlayState 创建新的 transcript overlay 状态。
func newTranscriptOverlayState(messages []chatMessage, width, height int, toolCollapsed bool) *transcriptOverlayState {
	const headerHeight = 2
	vpHeight := height - headerHeight
	if vpHeight < 5 {
		vpHeight = 5
	}
	vp := viewport.New(width, vpHeight)
	content := renderAllMessages(messages, width, toolCollapsed)
	vp.SetContent(content)
	vp.GotoBottom()
	return &transcriptOverlayState{
		viewport: vp,
		ready:    true,
		width:    width,
		height:   height,
	}
}

// refreshContent 更新 viewport 内容（streaming 时实时调用）。
func (t *transcriptOverlayState) refreshContent(messages []chatMessage, toolCollapsed bool) {
	if t == nil {
		return
	}
	atBottom := t.viewport.AtBottom()
	content := renderAllMessages(messages, t.width, toolCollapsed)
	t.viewport.SetContent(content)
	if atBottom {
		t.viewport.GotoBottom()
	}
}

// openTranscriptOverlay 打开全屏对话历史覆盖层。
func (m *chatModel) openTranscriptOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayTranscript)
}

// initTranscriptOverlay 初始化 transcript overlay 状态（需要布局信息）。
func (m chatModel) initTranscriptOverlay() chatModel {
	layout := m.generateLayout()
	m.transcriptOverlay = newTranscriptOverlayState(m.messages, layout.MainWidth, layout.BodyHeight, m.toolCollapsed)
	return m
}

// closeTranscriptOverlay 关闭全屏对话历史覆盖层。
func (m chatModel) closeTranscriptOverlay() chatModel {
	m.transcriptOverlay = nil
	if m.overlays != nil {
		m.overlays.Close(overlayTranscript)
	}
	m.refreshViewport()
	return m
}

// handleTranscriptOverlayKey 处理 transcript overlay 键盘事件。
func (m chatModel) handleTranscriptOverlayKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.transcriptOverlay == nil {
		return m.closeTranscriptOverlay(), nil
	}
	switch msg.String() {
	case "esc", "q", "ctrl+t":
		return m.closeTranscriptOverlay(), nil
	case "up", "k":
		m.transcriptOverlay.viewport.LineUp(1)
	case "down", "j":
		m.transcriptOverlay.viewport.LineDown(1)
	case "pgup", "ctrl+b":
		m.transcriptOverlay.viewport.HalfViewUp()
	case "pgdown", "ctrl+f":
		m.transcriptOverlay.viewport.HalfViewDown()
	case "g":
		m.transcriptOverlay.viewport.GotoTop()
	case "G":
		m.transcriptOverlay.viewport.GotoBottom()
	}
	return m, nil
}

// renderTranscriptOverlay 渲染全屏对话历史覆盖层。
func (m chatModel) renderTranscriptOverlay(width, height int) string {
	if m.transcriptOverlay == nil {
		return ""
	}
	t := m.transcriptOverlay

	// 更新 viewport 尺寸（如有变化）
	const headerHeight = 2
	vpHeight := height - headerHeight
	if vpHeight < 5 {
		vpHeight = 5
	}
	if t.viewport.Width != width || t.viewport.Height != vpHeight {
		t.viewport.Width = width
		t.viewport.Height = vpHeight
	}

	// 刷新内容（live tail when running）
	t.refreshContent(m.messages, m.toolCollapsed)

	// 标题行
	scrollPct := ""
	if t.viewport.TotalLineCount() > 0 {
		pct := int(float64(t.viewport.ScrollPercent()) * 100)
		scrollPct = mutedStyle.Render(fmt.Sprintf(" %d%%", pct))
	}
	title := titleStyle.Render("Transcript")
	shortcuts := mutedStyle.Render("  ↑↓/jk scroll • PgUp/PgDn half page • g/G top/bottom • Esc close")
	titleLine := lipgloss.NewStyle().Width(width).Render(title + shortcuts + scrollPct)
	divider := strings.Repeat("─", width)

	return lipgloss.JoinVertical(lipgloss.Left,
		titleLine,
		mutedStyle.Render(divider),
		t.viewport.View(),
	)
}

// transcriptOverlayDialog 实现 overlayDialog 接口。
type transcriptOverlayDialog struct{}

func (transcriptOverlayDialog) ID() overlayID { return overlayTranscript }
func (transcriptOverlayDialog) View(m chatModel, width, height int) string {
	return m.renderTranscriptOverlay(width, height)
}
func (transcriptOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	return m.handleTranscriptOverlayKey(msg)
}
