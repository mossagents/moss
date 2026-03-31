# MossCode Codex Alignment: Requirement Breakdown and Implementation Plan

## Goal

Drive `mosscode` toward stronger parity with Codex CLI by aligning:

1. **concept model**
2. **command names**
3. **UI/UX and TUI information architecture**

while keeping `moss` kernel minimal, stable, and reusable.

## Non-goals

- Auth/Login is still out of scope for this phase.
- This plan does **not** aim to copy every Codex CLI feature literally.
- This plan should **not** move product-only UX concerns into `moss` kernel.
- Preserving backward compatibility for `mosscode` product commands and UX is **not** a goal while it remains in POC stage.

## Planning principles

### Principle 1: Direct refactor over compatibility

When MossCode already has a working product command under a different name, prefer directly renaming or restructuring it toward the Codex model instead of carrying both surfaces.

### Principle 2: Product UX stays in `mosscode`

Slash command grouping, picker flows, footer/header layout, wording, and interaction pacing should be implemented in `mosscode`.

### Principle 3: Only sink generic primitives

Only move something into `moss` when it is a reusable runtime contract such as:

- session / task / checkpoint contracts
- execution policy
- hook/event surfaces
- attachment or transcript primitives

### Principle 4: Replace redundant product surfaces aggressively

Because `mosscode` is still a POC, existing MossCode-native product commands and UX flows may be removed or renamed if the Codex-aligned replacement is clearer.

Compatibility should be preserved mainly for:

- `moss` kernel and shared runtime contracts
- persisted state formats that would be risky to break casually
- temporary single-change migration steps

## Requirement breakdown

| Workstream | Desired outcome | `moss` / shared runtime work | `mosscode` work |
|---|---|---|---|
| Command and IA alignment | users can learn MossCode with a Codex-like command mental model | minimal or none | canonical command registry, direct renames, grouped help, discoverability improvements |
| Status / review / diff / mcp surfaces | high-frequency product surfaces are first-class | shared data/query helpers only if needed | slash commands, TUI renderers, summaries, picker flows |
| Session / thread / agent UX | session and background work feel like navigable threads | maybe shared task/session query contracts | `/agent`, resume/fork/new flow polish, thread list UX |
| Permissions / trust / approval UX | runtime posture is understandable and easy to switch | no major new primitives expected | Codex-style posture summaries, prompts, wording, command flow cleanup |
| Custom commands and `AGENTS.md` | repo-level workflow reuse becomes first-class | none or very light bootstrap helpers | `/init`, custom slash command loading, trust-aware command discovery |
| External context and multimodal | closer parity for image/web/editor workflows | input attachment primitives if needed | image attach UX, web search UX, editor handoff, mention/file attach |
| Product operations polish | better operator confidence and day-1 usability | no kernel expansion required | `/debug-config`, feature flags, statusline/theme, completion/install/update |

## Command alignment matrix

| Codex-style surface | Current MossCode surface | Plan |
|---|---|---|
| `/status` | `/session`, `/budget`, `doctor` | make `/status` the canonical runtime summary and collapse redundant user-facing surfaces where possible |
| `/diff` | `/git diff`, `review status`, change/checkpoint views | make `/diff` the canonical change inspection entry and simplify overlapping product wording |
| `/review` | `mosscode review`, partial git review helpers | make `/review` the canonical local review flow in TUI and CLI wording |
| `/mcp` | `config mcp ...`, doctor MCP visibility | promote `/mcp` to the primary user-facing entry and keep lower-level management structure only where it still adds value |
| `/plan` | profile/task-mode concepts + normal prompt flow | add explicit plan-mode command and UX |
| `/compact` | `/offload` | replace `/offload` as the primary user-facing concept with `/compact`; retain old naming only if temporarily required during refactor |
| `/resume` | CLI `resume`, `/session restore`, `/sessions` | replace session-restore-heavy wording with a Codex-like resume flow |
| `/fork` | `/checkpoint fork` | promote `/fork` to the primary user-facing branch workflow instead of checkpoint-first wording |
| `/agent` | tasks/delegation/background runtime | build thread/agent browser on current primitives |
| `/init` | none | scaffold `AGENTS.md` in product layer |

## Proposed phases

### Phase 0: Naming and command foundation

**Goal:** create a stable Codex-aligned command map before adding more UX.

Deliverables:

- central slash command registry metadata
- Codex-compatible primary commands
- removal or collapse of redundant product command names
- grouped help output
- terminology guide for session / task / agent / thread / checkpoint

Expected core changes:

- none, unless a shared command metadata helper becomes clearly reusable

### Phase 1: TUI parity pack

**Goal:** make the most common Codex CLI workflows visible and easy to reach.

Deliverables:

- `/status`
- `/diff`
- `/review`
- `/mcp`
- `/plan`
- `/compact`
- better help grouping and top-level discoverability

Expected core changes:

- none or very small query/helper additions

### Phase 2: Session / thread / agent UX

**Goal:** make background work and conversation branching feel like a first-class thread model.

Deliverables:

- `/agent`
- improved `/resume`
- improved `/fork`
- clearer distinction between session, thread, background task, and checkpoint

Expected core changes:

- only if current task/session query surfaces are insufficient for a clean product UX

### Phase 3: Custom workflows and instructions

**Goal:** align with Codex around reusable repository instructions and commands.

Deliverables:

- `/init` to scaffold `AGENTS.md`
- trust-aware loading/discovery of product-level custom slash commands
- user/project command organization

Expected core changes:

- likely none

### Phase 4: External context and multimodal UX

**Goal:** close the biggest remaining product-surface gap.

Deliverables:

- image input support
- web search workflow
- mention/file attach flow
- editor handoff

Expected core changes:

- only shared attachment/input abstractions if product implementation proves they are needed

### Phase 5: Product operations polish

**Goal:** improve operator confidence and day-1 usability.

Deliverables:

- `/debug-config`
- feature flag management
- statusline/footer controls
- theme management
- shell completion
- install/update/release ergonomics

Expected core changes:

- none

## Detailed implementation backlog

### Workstream A: command and IA alignment

1. Introduce a canonical command registry for `mosscode` TUI.
2. Define primary command names and remove redundant product-level names where appropriate.
3. Reorganize `/help` output by workflow category instead of historical growth order.
4. Normalize user-visible wording around session / thread / agent / checkpoint / task.

### Workstream B: status / review / diff / mcp parity pack

1. Add `/status` as the main runtime summary entry.
2. Add `/diff` as the main change inspection entry.
3. Add `/review` as the main local review entry.
4. Add `/mcp` as the main product-facing MCP visibility entry.
5. Replace `/offload` as the main transcript compaction concept with `/compact`.
6. Add `/plan` as an explicit planning-mode transition.

### Workstream C: session / thread / agent UX

1. Define how current sessions, tasks, delegation, and checkpoints map to a Codex-like thread model.
2. Add `/agent` browsing and switching.
3. Replace older restore/fork wording with `/resume` and `/fork` as the primary product surfaces.
4. Improve task/thread state summaries in the TUI.

### Workstream D: permissions and posture UX

1. Normalize status summaries for trust / approval / profile / execution policy.
2. Reduce overlap between `/permissions`, `/trust`, `/approval`, and `/profile`.
3. Align prompts and wording with a more consistent Codex-like posture model.

### Workstream E: custom workflows

1. Add `/init` for `AGENTS.md` scaffolding.
2. Design trust-aware custom slash command discovery.
3. Refactor overlapping skill/bootstrap entry points where a Codex-like product surface is clearer.

### Workstream F: multimodal and external context

1. Add image attachment support.
2. Add first-class web search workflow.
3. Add editor handoff.
4. Improve file mention / attach UX.

### Workstream G: product operations polish

1. Add `/debug-config`.
2. Add feature flag visibility and editing.
3. Add footer/statusline customization.
4. Add theme management.
5. Improve completion/install/update ergonomics.

## Recommended implementation order

1. **Phase 0 + Phase 1 together**
   - This gives the fastest visible parity gain with minimal kernel risk.
2. **Phase 2**
   - Needed to make session/task/delegation feel Codex-like.
3. **Phase 3**
   - Important for long-term product ecosystem and team workflows.
4. **Phase 4**
   - Major parity gain, but more expensive and more likely to require new shared primitives.
5. **Phase 5**
   - Important for polish and adoption, but lower leverage than command/UX convergence.

## Recommended immediate next step

Start with **Phase 0 + Phase 1: command naming refactor and TUI parity pack**.

Reason:

- highest UX payoff
- lowest kernel risk
- directly implements the new "concept + command + UI/UX alignment" principle
- takes advantage of the current POC window to simplify the product surface instead of carrying migration complexity
