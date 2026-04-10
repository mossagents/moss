package tui

type chatUILayout struct {
	Width          int
	Height         int
	HeaderHeight   int
	StatusHeight   int
	MainWidth      int
	BodyHeight     int
	MainHeight     int
	MetaHeight     int
	ViewportHeight int
	EditorHeight   int
}

func (m chatModel) generateLayout() chatUILayout {
	layout := chatUILayout{
		Width:        max(1, m.width),
		Height:       max(1, m.height),
		HeaderHeight: 1,
		StatusHeight: 1,
		MainWidth:    m.mainWidth(),
	}

	if m.hasHeaderMetaContent() {
		layout.MetaHeight = 1
	} else {
		layout.MetaHeight = 0
	}
	layout.BodyHeight = layout.Height - layout.HeaderHeight - layout.StatusHeight
	if layout.BodyHeight < layout.MetaHeight+3 {
		layout.BodyHeight = layout.MetaHeight + 3
	}

	layout.EditorHeight = m.editorPaneHeight(layout.MainWidth)
	layout.MainHeight = layout.BodyHeight - layout.EditorHeight
	minMainHeight := layout.MetaHeight + 3
	if layout.MainHeight < minMainHeight {
		shortfall := minMainHeight - layout.MainHeight
		if shortfall >= layout.EditorHeight {
			layout.EditorHeight = 0
		} else {
			layout.EditorHeight -= shortfall
		}
		layout.MainHeight = layout.BodyHeight - layout.EditorHeight
	}
	if layout.MainHeight < minMainHeight {
		layout.MainHeight = minMainHeight
	}
	layout.ViewportHeight = layout.MainHeight - layout.MetaHeight
	if layout.ViewportHeight < 3 {
		layout.ViewportHeight = 3
	}

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
