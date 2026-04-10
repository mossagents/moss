// Package main demonstrates the contrib/tui Extension API.
//
// This example registers a "snippets" extension that adds all five extension
// points to a mosscode-style TUI:
//
//   - Slash command  /snippet  — opens the snippet picker
//   - Key binding    ctrl+p    — opens the snippet picker
//   - Status widget             — shows "(N snippets)" in the footer
//   - Header meta widget        — shows "ctrl+p: snippets"
//   - Custom overlay            — interactive list picker
//
// Run from this directory (requires a configured LLM provider):
//
//	go run . --provider openai --model gpt-4o
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	appkit "github.com/mossagents/moss/appkit"
	appruntime "github.com/mossagents/moss/appkit/runtime"
	mosstui "github.com/mossagents/moss/contrib/tui"
	"github.com/mossagents/moss/kernel"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/spf13/pflag"
)

// ----- snippets data --------------------------------------------------------

var snippets = []string{
	"Write unit tests for the selected code",
	"Explain what this function does, step by step",
	"Find potential bugs and edge-cases in this file",
	"Add proper error handling to this function",
	"Refactor this code to improve readability",
}

// ----- CustomOverlay implementation -----------------------------------------

// snippetOverlay is a keyboard-navigable list picker rendered as a modal
// overlay inside the mosscode TUI.
type snippetOverlay struct {
	items  []string
	cursor int
}

func newSnippetOverlay(items []string) *snippetOverlay {
	return &snippetOverlay{items: items}
}

// ID returns the overlay identifier registered in Extension.Overlays.
func (o *snippetOverlay) ID() string { return "snippets" }

// View renders the picker box inside the allocated overlay dimensions.
func (o *snippetOverlay) View(ctx mosstui.OverlayContext) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99")).
		Padding(0, 1)

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)

	activeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("212")).
		Bold(true)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Snippet Picker") + "\n\n")

	for i, item := range o.items {
		prefix := "  "
		label := normalStyle.Render(item)
		if i == o.cursor {
			prefix = "▶ "
			label = activeStyle.Render(item)
		}
		sb.WriteString(prefix + label + "\n")
	}

	sb.WriteString("\n" + hintStyle.Render("↑↓ navigate  •  Enter send  •  Esc cancel"))

	return sb.String()
}

// HandleKey navigates the list, sends the selected snippet, or closes.
func (o *snippetOverlay) HandleKey(_ mosstui.OverlayContext, key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "up", "k":
		if o.cursor > 0 {
			o.cursor--
		}
	case "down", "j":
		if o.cursor < len(o.items)-1 {
			o.cursor++
		}
	case "enter":
		// SendMessageCmd closes the overlay and dispatches the text to the agent.
		return mosstui.SendMessageCmd(o.items[o.cursor])
	case "esc", "ctrl+c":
		return mosstui.CloseOverlayCmd()
	}
	return nil
}

// ----- Extension construction -----------------------------------------------

func buildSnippetsExtension() *mosstui.Extension {
	return &mosstui.Extension{
		Name: "snippets",

		// /snippet — type in the composer to open the picker
		SlashCommands: map[string]mosstui.SlashHandlerFunc{
			"/snippet": func(_ mosstui.TUIContext, _ []string) tea.Cmd {
				return mosstui.OpenOverlayCmd("snippets")
			},
		},

		// ctrl+p — global hotkey to open the picker from anywhere
		KeyBindings: map[string]mosstui.KeyHandlerFunc{
			"ctrl+p": func(_ mosstui.TUIContext) (bool, tea.Cmd) {
				return true, mosstui.OpenOverlayCmd("snippets")
			},
		},

		// Footer status bar: shows snippet count when idle
		StatusWidgets: []mosstui.WidgetFunc{
			func(_ mosstui.TUIContext) string {
				return fmt.Sprintf("%d snippets", len(snippets))
			},
		},

		// Header meta line: reminds the user of the hotkey
		HeaderMetaWidgets: []mosstui.WidgetFunc{
			func(_ mosstui.TUIContext) string {
				return "ctrl+p: snippets"
			},
		},

		// Overlay factory: called fresh each time the picker opens
		Overlays: map[string]func() mosstui.CustomOverlay{
			"snippets": func() mosstui.CustomOverlay {
				return newSnippetOverlay(snippets)
			},
		},
	}
}

// ----- Main / TUI wiring ----------------------------------------------------

func main() {
	var provider, model string
	pflag.StringVar(&provider, "provider", "openai", "LLM provider (openai, anthropic, …)")
	pflag.StringVar(&model, "model", "", "Model name (defaults to provider default)")
	pflag.Parse()

	workspace, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := mosstui.Run(mosstui.Config{
		Provider:  provider,
		Model:     model,
		Workspace: workspace,
		Trust:     "full",

		// Register our custom extension.
		Extensions: []*mosstui.Extension{buildSnippetsExtension()},

		BuildKernel: func(wsDir, trust, approvalMode, profile, prov, mod, apiKey, baseURL string, io kernio.UserIO) (*kernel.Kernel, error) {
			return appkit.BuildKernel(context.Background(), &appkit.AppFlags{
				Provider:  prov,
				Model:     mod,
				Workspace: wsDir,
				Trust:     trust,
				Profile:   profile,
				APIKey:    apiKey,
				BaseURL:   baseURL,
			}, io)
		},

		BuildSystemPrompt: func(workspace, trust string) string {
			return "You are a helpful coding assistant."
		},

		BuildSessionConfig: func(workspace, trust, approvalMode, profile, systemPrompt string) session.SessionConfig {
			resolved, _ := appruntime.ResolveProfileForWorkspace(appruntime.ProfileResolveOptions{
				Workspace:    workspace,
				Trust:        trust,
				ApprovalMode: approvalMode,
			})
			return appruntime.ApplyResolvedProfileToSessionConfig(session.SessionConfig{
				Goal:         "interactive coding assistant",
				Mode:         "interactive",
				TrustLevel:   trust,
				SystemPrompt: systemPrompt,
				MaxSteps:     200,
			}, resolved)
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
