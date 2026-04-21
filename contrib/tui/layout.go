package tui

type chatUILayout struct {
	Width        int
	Height       int // 终端高度，用于 overlay 尺寸计算
	MainWidth    int
	BodyHeight   int // Height - StatusHeight，用于 overlay 定位
	EditorHeight int
}

func (m chatModel) generateLayout() chatUILayout {
	layout := chatUILayout{
		Width:     max(1, m.width),
		Height:    max(1, m.height),
		MainWidth: m.mainWidth(),
	}
	layout.BodyHeight = max(3, layout.Height-1) // -1 for status bar
	layout.EditorHeight = m.editorPaneHeight(layout.MainWidth)
	return layout
}

func (m chatModel) hasActiveOverlay() bool {
	return m.activeOverlay() != nil
}

func (m chatModel) editorPaneHeight(width int) int {
	if m.hasActiveOverlay() {
		return 0
	}
	height := m.inputBoxHeight() + 1 // hint + composer
	if len(m.queuedInputs) > 0 {
		height++
	}
	return height
}
