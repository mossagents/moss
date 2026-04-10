package tui

import tea "github.com/charmbracelet/bubbletea"

// TUIContext is a read-only snapshot of TUI state passed to extension handlers.
// It is captured at the moment of the call; it does not update automatically.
type TUIContext struct {
	Workspace   string
	Provider    string
	Model       string
	Profile     string
	Trust       string
	SessionID   string
	IsStreaming bool
	InputValue  string
	Width       int
}

// OverlayContext extends TUIContext with the dimensions available to an overlay.
type OverlayContext struct {
	TUIContext
	OverlayWidth  int
	OverlayHeight int
}

// SlashHandlerFunc handles a custom /command.
// Return a non-nil tea.Cmd for side effects (e.g., OpenOverlayCmd).
type SlashHandlerFunc func(ctx TUIContext, args []string) tea.Cmd

// KeyHandlerFunc handles a custom key press.
// Return consumed=true to prevent the built-in key from also firing.
type KeyHandlerFunc func(ctx TUIContext) (consumed bool, cmd tea.Cmd)

// WidgetFunc returns a display string for injection into the TUI.
// Return "" to contribute nothing for the current state.
type WidgetFunc func(ctx TUIContext) string

// CustomOverlay is an extension-provided modal overlay dialog.
type CustomOverlay interface {
	// ID returns a unique identifier string for this overlay type.
	ID() string
	// View renders the overlay content within the given dimensions.
	View(ctx OverlayContext) string
	// HandleKey processes a key press while this overlay is active.
	// Use CloseOverlayCmd, SendMessageCmd, or OpenOverlayCmd to affect TUI state.
	HandleKey(ctx OverlayContext, key tea.KeyMsg) tea.Cmd
}

// CloseOverlayCmd returns a Cmd that closes the currently active custom overlay.
func CloseOverlayCmd() tea.Cmd {
	return func() tea.Msg { return closeCustomOverlayMsg{} }
}

// SendMessageCmd returns a Cmd that sends text to the agent and closes the
// current custom overlay.  Text beginning with "/" is dispatched as a slash
// command instead of being sent verbatim.
func SendMessageCmd(text string) tea.Cmd {
	return func() tea.Msg { return sendFromOverlayMsg{text: text} }
}

// OpenOverlayCmd returns a Cmd that opens a custom overlay by its registered ID.
// The factory registered in Extension.Overlays is called to produce a fresh instance.
func OpenOverlayCmd(id string) tea.Cmd {
	return func() tea.Msg { return openCustomOverlayMsg{id: id} }
}

// Internal message types for custom overlay lifecycle.
type closeCustomOverlayMsg struct{}
type sendFromOverlayMsg struct{ text string }
type openCustomOverlayMsg struct{ id string }

// coreKeys lists key strings that extensions may not override.
var coreKeys = map[string]bool{
	"ctrl+c":      true,
	"esc":         true,
	"ctrl+t":      true,
	"ctrl+o":      true,
	"ctrl+x":      true,
	"up":          true,
	"down":        true,
	"tab":         true,
	"shift+tab":   true,
	"enter":       true,
	"alt+enter":   true,
	"shift+enter": true,
}

// Extension bundles TUI customizations provided by a developer.
// Add an Extension to Config.Extensions to activate it.
//
// Validation at startup: duplicate slash command names across extensions cause
// installExtensions to return an error.  Core key bindings cannot be overridden.
type Extension struct {
	// Name identifies this extension in error messages and debugging.
	Name string

	// SlashCommands maps "/command" strings to handler functions.
	// Commands must start with "/" and be lowercase.
	// Built-in commands take precedence over extension commands with the same name.
	SlashCommands map[string]SlashHandlerFunc

	// KeyBindings maps tea.KeyMsg.String() values to handler functions.
	// Extensions are evaluated before the built-in key dispatch.
	// Core keys (ctrl+c, esc, enter, tab, up/down, shift+tab, etc.) cannot be overridden.
	KeyBindings map[string]KeyHandlerFunc

	// StatusWidgets are called in idle state to produce additional segments
	// appended to the left side of the footer status bar.  Segments are joined
	// with "  •  ".
	StatusWidgets []WidgetFunc

	// HeaderMetaWidgets are called to produce additional segments for the header
	// meta line (the posture/context row below the shell header).
	HeaderMetaWidgets []WidgetFunc

	// Overlays registers custom overlay factories keyed by overlay ID string.
	// Open an overlay via OpenOverlayCmd(id) from a slash command or key handler.
	// The factory is invoked each time the overlay is opened, producing a fresh instance.
	Overlays map[string]func() CustomOverlay
}
