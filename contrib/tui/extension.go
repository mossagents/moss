// Package tui provides the Bubble Tea–based terminal chat interface for MOSS
// applications. It renders a multi-pane TUI (header, body, composer, footer)
// and drives the full conversation lifecycle: streaming LLM responses, slash
// commands, tool-approval overlays, session management, and more.
//
// # Extension System
//
// The Extension type lets third-party code add capabilities to the TUI without
// forking or modifying the core. Pass one or more extensions in Config.Extensions
// when calling Run():
//
//	mosstui.Run(mosstui.Config{
//	    ...
//	    Extensions: []*mosstui.Extension{myExt},
//	})
//
// An Extension bundles five independent customization points:
//
//   - SlashCommands – register /command handlers evaluated before workspace
//     custom commands and the generic /<skill> fallback, but after all
//     built-in slash commands (built-ins always take precedence).
//
//   - KeyBindings – handle key presses before the built-in dispatch loop.
//     Core keys (ctrl+c, esc, enter, tab, up/down, shift+tab, shift+enter,
//     alt+enter, ctrl+t, ctrl+o, ctrl+x, ctrl+y, ctrl+s) cannot be overridden.
//
//   - StatusWidgets – contribute text segments appended to the left side of
//     the footer status bar. Only rendered in idle (non-streaming) state.
//
//   - HeaderMetaWidgets – contribute text segments appended to the header
//     meta line (the row that shows agent posture / context mode).
//
//   - Overlays – register factory functions keyed by a string ID. Each call
//     to OpenOverlayCmd(id) invokes the factory, producing a fresh
//     CustomOverlay instance. Only one custom overlay may be active at a
//     time; opening a new one replaces any existing one.
//
// The Extension is validated at startup (installExtensions): duplicate slash
// command names or reserved key bindings cause an error before the TUI opens.
//
// See _examples/snippets/main.go for a self-contained example that exercises
// all five extension points.
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

// AppendSystemMessageCmd returns a Cmd that appends a system info message to the chat.
// For use inside async Cmd closures, return SystemMsg(text) directly.
func AppendSystemMessageCmd(text string) tea.Cmd {
	return func() tea.Msg { return appendSystemMessageMsg{text: text} }
}

// AppendErrorMessageCmd returns a Cmd that appends a system error message to the chat.
// For use inside async Cmd closures, return ErrorMsg(text) directly.
func AppendErrorMessageCmd(text string) tea.Cmd {
	return func() tea.Msg { return appendErrorMessageMsg{text: text} }
}

// SystemMsg returns a tea.Msg that appends a system info message. Use inside async Cmd closures:
//
//	return func() tea.Msg {
//	    result, err := doWork()
//	    if err != nil { return mosstui.ErrorMsg(err.Error()) }
//	    return mosstui.SystemMsg(result)
//	}
func SystemMsg(text string) tea.Msg { return appendSystemMessageMsg{text: text} }

// ErrorMsg returns a tea.Msg that appends a system error message. Use inside async Cmd closures.
func ErrorMsg(text string) tea.Msg { return appendErrorMessageMsg{text: text} }

// Internal message types for custom overlay lifecycle.
type closeCustomOverlayMsg struct{}
type sendFromOverlayMsg struct{ text string }
type openCustomOverlayMsg struct{ id string }

// Internal message types for extension-initiated chat messages.
type appendSystemMessageMsg struct{ text string }
type appendErrorMessageMsg struct{ text string }

// SlashCommandDef describes a slash command registered by an Extension.
type SlashCommandDef struct {
	// Handler processes the slash command. Required.
	Handler SlashHandlerFunc

	// Summary is the one-line description shown in /help. Optional.
	Summary string

	// Section is the /help group header (e.g. "Review and recovery"). Optional.
	Section string

	// Usage is the full usage string shown as detail in /help when selected. Optional.
	// Defaults to Summary if empty.
	Usage string

	// HiddenInNav hides this command from the /help picker and autocomplete. Optional.
	HiddenInNav bool
}

// coreKeys lists key strings that extensions may not override.
var coreKeys = map[string]bool{
	"ctrl+c":      true,
	"esc":         true,
	"ctrl+t":      true,
	"ctrl+o":      true,
	"ctrl+x":      true,
	"ctrl+y":      true,
	"ctrl+p":      true,
	"ctrl+n":      true,
	"up":          true,
	"down":        true,
	"pgup":        true,
	"pgdown":      true,
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

	// SlashCommands maps "/command" strings to SlashCommandDef descriptors.
	// Commands must start with "/" and be lowercase.
	// Built-in commands take precedence over extension commands with the same name.
	// Use SlashCommandDef.Summary/Section to make the command appear in /help.
	SlashCommands map[string]SlashCommandDef

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

	// OnSessionStart is called when the kernel and session become ready (e.g. on
	// startup, after a model switch, or after a profile/trust switch).
	// Return a non-nil Cmd for side effects (e.g. AppendSystemMessageCmd).
	OnSessionStart func(ctx TUIContext) tea.Cmd

	// OnSessionEnd is called just before the TUI exits (ctrl+c / /exit).
	// Return a non-nil Cmd for cleanup side effects.
	OnSessionEnd func(ctx TUIContext) tea.Cmd

	// OnModelSwitch is called after the active model changes.
	// prevModel is the old model ID; nextModel is the new one.
	// Return a non-nil Cmd for side effects.
	OnModelSwitch func(ctx TUIContext, prevModel, nextModel string) tea.Cmd
}
