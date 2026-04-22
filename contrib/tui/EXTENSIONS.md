# TUI Extension System

The `contrib/tui` package provides an extension API that lets you add custom
behaviour to the mosscode terminal chat interface without modifying core code.

---

## Quick start

```go
import mosstui "github.com/mossagents/moss/contrib/tui"

ext := &mosstui.Extension{
    Name: "my-ext",
    // ... extension points ...
}

mosstui.Run(mosstui.Config{
    // ... standard config ...
    Extensions: []*mosstui.Extension{ext},
})
```

A working example that exercises the core interactive extension points is in
[`_examples/snippets/main.go`](./_examples/snippets/main.go).

---

## Extension points

### 1. Slash commands

Register `/command` handlers. Type `/mycommand arg1 arg2` in the composer to
trigger them.

```go
SlashCommands: map[string]mosstui.SlashCommandDef{
    "/greet": {
        Handler: func(ctx mosstui.TUIContext, args []string) tea.Cmd {
            // args = ["arg1", "arg2"] (words after the command name)
            // return a tea.Cmd for side effects, or nil
            return mosstui.SendMessageCmd("Hello from extension!")
        },
        Summary: "Send a greeting through the chat pipeline",
    },
},
```

**Dispatch order** (highest priority first):

1. Built-in commands (`/help`, `/clear`, `/thread`, `/model`, …)
2. **Extension commands** ← registered here
3. Workspace custom commands (from config)
4. Generic `/<skill> <task>` fallback

Built-ins always win; your `/help` will never fire if the user types `/help`.
Duplicate names across extensions cause a startup error.

---

### 2. Key bindings

Handle key presses **before** the built-in dispatch loop.

```go
KeyBindings: map[string]mosstui.KeyHandlerFunc{
    "ctrl+g": func(ctx mosstui.TUIContext) (consumed bool, cmd tea.Cmd) {
        return true, mosstui.OpenOverlayCmd("my-overlay")
        // consumed=true prevents the built-in handler from also firing
        // consumed=false lets the built-in handle it after your handler
    },
},
```

Key strings use the [Bubble Tea](https://github.com/charmbracelet/bubbletea)
`KeyMsg.String()` format: `"ctrl+k"`, `"alt+enter"`, `"f1"`, etc.

**Protected core keys** that extensions cannot bind:

| Key | Purpose |
|-----|---------|
| `ctrl+c` | Quit |
| `esc` | Cancel / close overlay |
| `enter` | Send message |
| `shift+enter` / `alt+enter` | Insert newline |
| `tab` / `shift+tab` | Navigation |
| `up` / `down` | History navigation |
| `ctrl+t` | Toggle transcript overlay |
| `ctrl+o` | Toggle tool output visibility |
| `ctrl+x` | Remove the latest attachment |
| `ctrl+y` | Open the copy picker |
| `ctrl+p` / `ctrl+n` | Reserved internal navigation keys |
| `pgup` / `pgdown` | Reserved paging keys |

Attempting to bind a protected key causes `installExtensions` to return an
error at startup.

---

### 3. Status widgets

Contribute segments to the left side of the **footer status bar**. Rendered
only when the agent is idle (not streaming).

```go
StatusWidgets: []mosstui.WidgetFunc{
    func(ctx mosstui.TUIContext) string {
        if ctx.IsStreaming {
            return "" // return "" to contribute nothing
        }
        return "my-status-info"
    },
},
```

Segments from all widgets are joined with `"  •  "` after the built-in
keyboard shortcut hints.

---

### 4. Header meta widgets

Contribute segments to the **header meta line** (the row below the shell
header that shows agent posture and context mode).

```go
HeaderMetaWidgets: []mosstui.WidgetFunc{
    func(ctx mosstui.TUIContext) string {
        return fmt.Sprintf("branch: %s", currentBranch())
    },
},
```

Returns `""` to contribute nothing for the current state. Segments are joined
with `"  •  "` after the built-in posture label.

---

### 5. Custom overlays

Register modal overlay dialogs identified by a string ID. Use a **factory
function** so each open produces a fresh instance with clean state.

```go
Overlays: map[string]func() mosstui.CustomOverlay{
    "my-overlay": func() mosstui.CustomOverlay {
        return &myOverlay{} // fresh instance every time
    },
},
```

Implement the `CustomOverlay` interface:

```go
type myOverlay struct{ ... }

func (o *myOverlay) ID() string { return "my-overlay" }

func (o *myOverlay) View(ctx mosstui.OverlayContext) string {
    // ctx.OverlayWidth / ctx.OverlayHeight are the available dimensions
    return lipgloss.NewStyle().Width(ctx.OverlayWidth).Render("Hello overlay!")
}

func (o *myOverlay) HandleKey(ctx mosstui.OverlayContext, key tea.KeyMsg) tea.Cmd {
    switch key.String() {
    case "enter":
        return mosstui.SendMessageCmd("text sent from overlay")
    case "esc":
        return mosstui.CloseOverlayCmd()
    }
    return nil
}
```

Only one custom overlay can be active at a time. Opening a second one
replaces the first.

---

### 6. Lifecycle hooks

Extensions can also react to runtime lifecycle events:

```go
ext := &mosstui.Extension{
    OnSessionStart: func(ctx mosstui.TUIContext) tea.Cmd {
        return mosstui.AppendSystemMessageCmd("Session ready.")
    },
    OnSessionEnd: func(ctx mosstui.TUIContext) tea.Cmd {
        return nil
    },
    OnModelSwitch: func(ctx mosstui.TUIContext, prevModel, nextModel string) tea.Cmd {
        return mosstui.AppendSystemMessageCmd("Model switched: " + prevModel + " -> " + nextModel)
    },
}
```

- `OnSessionStart` runs when the kernel/session becomes ready.
- `OnSessionEnd` runs before the TUI exits.
- `OnModelSwitch` runs after the active model changes.

---

## Helper commands

Three `tea.Cmd` constructors are provided for use inside key handlers and
overlay `HandleKey` methods:

| Function | Effect |
|----------|--------|
| `mosstui.OpenOverlayCmd(id)` | Open the registered overlay with the given ID |
| `mosstui.CloseOverlayCmd()` | Close the currently active custom overlay |
| `mosstui.SendMessageCmd(text)` | Close the overlay and send `text` to the agent (or dispatch as slash command if `text` starts with `/`) |

---

## TUIContext

Every handler receives a `TUIContext` snapshot capturing the TUI state at the
moment of the call:

```go
type TUIContext struct {
    Workspace   string  // active workspace directory
    Provider    string  // LLM provider name
    Model       string  // model identifier
    CollaborationMode string // current collaboration mode
    Trust       string  // trust level
    SessionID   string  // current session / thread ID
    IsStreaming bool    // true while LLM is generating
    InputValue  string  // current composer text
    Width       int     // terminal width in columns
}
```

`OverlayContext` embeds `TUIContext` and adds:

```go
type OverlayContext struct {
    TUIContext
    OverlayWidth  int // usable overlay width
    OverlayHeight int // usable overlay height
}
```

> **Note:** `TUIContext` is a point-in-time snapshot. It becomes stale inside
> `tea.Cmd` goroutines. Use it for rendering and synchronous decisions only.

---

## Validation

`installExtensions` is called during TUI startup and returns an error for:

- Duplicate slash command names across two or more extensions
- A key binding that targets a protected core key

The TUI will not open if validation fails; the error propagates to the
`mosstui.Run` caller.

---

## Example: snippets picker

See [`_examples/snippets/main.go`](./_examples/snippets/main.go) for a
complete runnable example registering the interactive extension points.

```
cd contrib/tui
go build ./_examples/snippets/
./snippets --provider openai --model gpt-4o
```
