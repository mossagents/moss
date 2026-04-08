# mosscode TUI Redesign Design

## Problem

`mosscode` already has the runtime features needed for a strong operator-facing TUI, but the current shell still feels visually busy and interaction-heavy in the wrong places:

- the header and frame chrome take more attention than the transcript
- status and composer surfaces expose too much control detail at once
- tool / progress / reasoning output do not read as one coherent execution timeline
- overlays feel like separate mini-apps instead of one consistent secondary workflow layer
- power features are available, but their entry points compete with the main coding flow

This leaves the product short of the terminal UX target shown in the reference screenshots: darker, quieter, denser, and more transcript-first.

## Goals

1. Make the default `mosscode` TUI feel chat-first and execution-centric.
2. Reduce chrome and visual noise without removing advanced operator capability.
3. Unify tool calls, progress, reasoning, and assistant output into a more coherent timeline.
4. Make overlays and dialogs feel like one system with shared interaction rules.
5. Keep the redesign bounded to the shared TUI plus `mosscode` integration, without introducing a second product shell.

## Non-Goals

1. No parallel "classic" and "new" TUI modes in this phase.
2. No new backend runtime capability is required for the redesign.
3. No removal of existing operator features such as review, checkpoint, agent, or MCP workflows.
4. No broad rewrite of runtime event semantics outside what the TUI already consumes.
5. No attempt to fully mimic the screenshots pixel-for-pixel.

## Candidate Approaches

### A. Chat-first minimal terminal shell (recommended)

Rebuild the default TUI around a single-column transcript, a thin shell header, a compact runtime-first status bar, and a prompt-first composer. Move advanced controls behind consistent overlays and hotkeys.

Pros:

- best fit for the screenshot direction
- keeps the main coding loop dominant
- lowest long-term UX complexity
- works with the existing `contrib/tui` composition model

Cons:

- requires more visible change to the default workflow
- some existing affordances become less always-visible

### B. Two-layer operator workstation

Keep more persistent management chrome while refining styling and reducing noise. The chat stays central, but operator panes remain more visible.

Pros:

- lower behavior shock for current users
- less restructuring of persistent layout

Cons:

- weaker match to the target aesthetic
- higher risk of staying visually crowded

### C. Terminal-plain compaction

Minimize styling aggressively and rely on mostly textual separators, simple list rows, and lighter framing.

Pros:

- implementation-light
- works well in plain terminal environments

Cons:

- undershoots the intended premium terminal feel
- leaves less room for hierarchy between transcript, events, and overlays

## Recommendation

Choose **Approach A**.

The redesign should make the main screen feel like a focused coding transcript rather than a dashboard. Operator workflows still matter, but they should appear as temporary layers entered on demand and exited quickly.

## Scope

### In-scope surfaces

1. `contrib/tui/styles.go`
2. `contrib/tui/theme.go`
3. `contrib/tui/chat_view.go`
4. `contrib/tui/chat_components.go`
5. `contrib/tui/message.go`
6. `contrib/tui/progress.go`
7. `contrib/tui/statusline.go`
8. `contrib/tui/shell.go`
9. `contrib/tui/layout.go`
10. `contrib/tui/overlay.go`
11. `contrib/tui/ask_form.go`
12. `contrib/tui/selection_list.go`
13. `contrib/tui/slash_popup.go`
14. `contrib/tui/chat.go`
15. `examples/mosscode/commands_exec.go` for launch-time TUI configuration wiring only

### Out-of-scope surfaces

- backend runtime protocols
- model execution semantics
- tool call business logic
- MCP server implementations
- non-TUI product surfaces

## Design

## 1. Visual direction

The new default should shift from framed application UI toward a quieter terminal shell:

- darker background, lower-contrast borders, fewer accent colors
- typography hierarchy driven by spacing, weight, and muted color, not heavy boxes
- transcript as the primary visual column
- accents reserved for selection, critical status, approvals, and actionable focus

The intended feeling is "calm terminal control surface", not "windowed desktop app rendered in a terminal".

## 2. Main layout

The root layout should become a three-part stack:

1. **thin header**
2. **single-column transcript body**
3. **bottom runtime bar + composer**

### Thin header

The header should shrink to a lightweight product/session strip. It may include:

- product name or profile label
- current session or workspace context
- a small current-mode indicator when relevant

It should not act as a control center. Large framed title blocks and dense key hints should be removed from the default view.

### Transcript body

The transcript becomes the dominant vertical surface. It should render:

- user turns
- assistant turns
- tool / progress / reasoning event blocks
- inline system notices
- compact approval and review prompts before they escalate into overlays

There should be no persistent side pane in the default view.

#### Inline-to-overlay escalation rules

Transcript-native prompts should remain inline only when all of the following are true:

1. the action is a simple confirm / reject or one-step acknowledgement
2. no list navigation is required
3. no structured form fields are required
4. no large diff, review body, or extended explanation is required to act safely

The interaction should escalate into an overlay immediately when any of the following is true:

1. the user needs to choose from more than two options
2. the action requires form input or multiple fields
3. the action includes diff/review content that is too large for a compact transcript block
4. the user explicitly requests more detail or expanded context
5. the viewport is too narrow to present the prompt safely inline

### Bottom runtime bar + composer

The footer becomes two coordinated surfaces:

1. a compact runtime bar describing current state
2. a prompt-first composer for input

This keeps the active control surfaces near the user's typing area while preserving a quiet top-of-screen.

## 3. Status bar redesign

The status bar should move from broad `key=value` inspection toward a short runtime summary.

Recommended content priority:

1. run state: idle / streaming / tool active / approval waiting / error
2. active model or profile when the user has selected a non-default or explicitly switched runtime
3. active session or workspace hint when space allows
4. one short contextual hint, not a wall of key bindings

Behavior rules:

- keep the line short and scannable
- prefer separators over labeled boxes
- only surface unusual or actionable state changes prominently
- when idle, show a minimal hint set
- when busy, status should pivot to live execution state first

The bar should answer "what is happening right now?" before "what are all the controls?".

## 4. Composer redesign

The composer should become prompt-first and thinner.

Recommended behavior:

- visually anchor the input area with a restrained border or filled block
- keep primary emphasis on typed content, not on surrounding controls
- move long help copy and secondary affordances out of the default composer
- preserve multiline input, slash entry, and context-aware hints

### Composer states

1. **idle**: minimal prompt, optional short hint
2. **slash active**: popup anchored close to the composer
3. **busy**: send affordance replaced by cancel/working state language
4. **approval pending**: composer visually de-emphasized while the approval surface is active

The composer should feel like a modern terminal command bar, not a form panel.

## 5. Unified timeline event blocks

Tool calls, progress, reasoning, and other execution metadata should be rendered as one family of event blocks instead of multiple unrelated styles.

Shared characteristics:

- compact left-edge identity marker
- single-line summary first
- expandable detail region
- muted framing compared with user/assistant messages
- stable visual ordering inside the transcript

### Event block hierarchy

1. **summary row**
   - event type
   - short name or action
   - current state or result
2. **detail body**
   - arguments
   - logs
   - reasoning text
   - structured output

### Default folding policy

- summaries visible by default
- verbose details collapsed by default
- failures and approval-blocked states may auto-expand more aggressively
- repeated progress updates may coalesce into the latest visible summary only when they describe the same in-flight activity key and no older row contains terminal, failure, approval, or distinct tool-call state

This keeps the transcript dense and legible while preserving inspectability.

#### Coalescing rules

Coalescing is allowed only for repeated progress-style updates that are all:

1. scoped to the same session and active run
2. derived from the same activity key or block identity
3. non-terminal
4. not carrying an approval request, error, or final outcome

Coalescing must never merge away:

1. tool start/end boundaries
2. approval request or approval resolution states
3. failures, cancellations, or completion states
4. different event identities that happen to have similar labels

#### Fold-state policy

Fold state should be view-local and session-local to the current TUI process:

1. it survives normal scrolling and redraws during the live UI session
2. it does not need to persist across full app restart or later resume
3. newly created failure and approval-blocked rows may ignore prior collapse defaults and open by default

## 6. Overlay and dialog system

All secondary workflows should move onto one shared overlay language.

Affected workflows include:

- selection lists
- ask-user forms
- approvals
- review flows
- checkpoint or resume pickers
- help and slash-adjacent discovery surfaces
- agent and MCP management surfaces

### Overlay visual model

Overlays should be:

- narrower than the full viewport
- centered or bottom-biased depending on type
- framed with subtle borders and low-noise title treatment
- consistent in padding, spacing, and footer hints

Selection should read like a focused terminal row, not a large highlighted card.

### Overlay classes

1. **picker overlays**  
   For lists, commands, checkpoints, sessions, agents, and MCP choices.
2. **form overlays**  
   For ask-user style forms, approval capture, and structured confirmations.
3. **review overlays**  
   For larger diffs, fork/resume choices, or multi-step decision surfaces.

All three classes should share the same shell primitives even if their body widgets differ.

### Overlay lifecycle

The user should enter a temporary management layer, complete an action, and return to the transcript with minimal context loss.

Rules:

1. only one primary overlay is active at a time
2. overlays may replace each other, but should not stack into deep nesting
3. closing an overlay returns focus to the composer or relevant transcript location
4. important resulting messages are written back into the timeline after overlay completion

## 7. Keyboard and interaction model

The keyboard model should be reorganized around three mental zones:

### Chat flow

- type and send
- insert newline
- cancel active run
- recall recent input
- trigger slash completion

### Timeline flow

- move through transcript focus targets when a focus mode is active
- expand / collapse event blocks
- copy or inspect recent tool results where supported
- jump to the latest active event

### Overlay flow

- navigate items
- confirm
- cancel / close
- use a small set of high-frequency single-key shortcuts when appropriate

The redesign should reduce the feeling that each overlay is a separate application with separate rules.

## 8. Discoverability model

The current surface should stop trying to teach every shortcut all the time.

Recommended model:

1. the default status/composer area exposes only a few high-value hints: send, newline, cancel when active, and help/slash discovery
2. the full key map and command language live in a dedicated help overlay
3. slash popup and help overlay use the same terminology and grouping
4. contextual hints appear when they are relevant, not permanently

This keeps the main screen quiet while making learning paths explicit.

## 9. Component boundaries

The redesign should preserve clear unit responsibilities inside `contrib/tui`.

### Theme and token layer

- `styles.go`
- `theme.go`

Purpose: define color, spacing, border, emphasis, and shared style primitives.

### Shell and layout layer

- `shell.go`
- `layout.go`
- `chat_view.go`
- `chat_components.go`

Purpose: define frame composition, header/body/footer structure, overlay placement, and shared shell helpers.

Boundary note:

- `chat_view.go` and `chat_components.go` belong here only for structural composition, viewport stacking, and shell assembly

### Transcript rendering layer

- `message.go`
- `progress.go`

Purpose: render user messages, assistant messages, and unified event blocks with consistent hierarchy and folding behavior.

### Status and composer layer

- `statusline.go`
- `chat_components.go`
- `chat_view.go`
- `chat.go`

Purpose: render current runtime state and prompt entry with minimal noise.

Boundary note:

- `chat_components.go` and `chat_view.go` belong here only for status/composer rendering surfaces
- `chat.go` belongs here only for composer-facing state and focus routing, not for shell layout assembly

### Overlay interaction layer

- `overlay.go`
- `selection_list.go`
- `ask_form.go`
- `slash_popup.go`

Purpose: provide shared modal shell, focus rules, input routing, and specialized body widgets.

### State orchestration layer

- `chat.go`

Purpose: own focus state, overlay state, event folding defaults, composer behavior, and interaction routing.

Boundary note:

- `chat.go` remains the orchestration owner for interaction state, while view files consume that state for rendering

The redesign should avoid pushing presentation policy deep into unrelated runtime adapters.

### mosscode product integration boundary

`examples/mosscode` is in scope only for launch-time product wiring, not for custom rendering forks.

Allowed integration work:

1. pass new shared-TUI config defaults or labels through `mosstui.Run(...)`
2. align welcome/banner/session wording with the new shell tone if required
3. expose any new shared TUI options needed to keep the product entrypoint coherent

Not allowed in this phase:

1. product-only layout branches that bypass shared `contrib/tui` rendering
2. duplicate overlay or transcript rendering logic under `examples/mosscode`
3. mosscode-only interaction rules that diverge from the shared TUI without a shared abstraction

## 10. Error handling and degraded behavior

The redesign should stay behavior-safe under narrow terminals and noisy runtime conditions.

### Layout degradation

1. in narrow widths, header metadata should collapse before transcript content does
2. status hints should shorten before wrapping into clutter
3. overlays should clamp width and fall back to tighter padding
4. event summaries should remain readable even when detail panes are unavailable

### Interaction safety

1. if an overlay cannot render its richer layout, it should still preserve navigation and confirmation behavior
2. if event detail rendering fails for a block, the summary row should still render
3. failures, approvals, and cancellations should remain explicit and not be hidden behind collapsed-only affordances

### Migration safety

1. preserve existing operator workflows even when their visual entry points move
2. keep slash-driven and hotkey-driven access paths aligned during rollout
3. avoid silent removal of capabilities that existing users rely on

## 11. Testing and validation

Validation should focus on behavior and legibility, not only compilation.

### Code-level checks

1. run the existing repo formatting/build/test workflow already used for TUI work
2. add or update focused TUI tests where current files already have unit coverage seams
3. keep rendering helpers deterministic enough for snapshot-style assertions where the repo already follows that pattern

### Manual acceptance checks

1. idle screen reads as transcript-first with visibly reduced chrome
2. active tool/progress/reasoning output reads as one coherent event family
3. status bar answers current runtime state in one glance
4. composer remains fast for normal chat entry and slash command flow
5. help, picker, ask-form, and review overlays feel visually related
6. keyboard flow between transcript, composer, and overlays feels predictable
7. narrow terminal behavior remains usable

## Acceptance criteria

1. The default `mosscode` TUI uses the new quieter visual language rather than exposing it as an optional theme.
2. The main screen is a single-column, transcript-first layout with a thin header and bottom-focused controls.
3. Tool, progress, and reasoning output render as unified summary-first event blocks.
4. Status and composer surfaces are shorter, denser, and more runtime-focused than before.
5. Overlay workflows share one coherent shell and interaction model.
6. Existing operator capabilities remain reachable without persistent dashboard clutter.
7. The redesign lands within the shared TUI architecture instead of introducing product-specific duplication without need.
