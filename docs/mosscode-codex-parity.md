# MossCode vs Codex CLI: Latest Product Review

## Context and scope

This document captures the latest parity review after re-reading the Codex CLI Features and Slash Commands documentation and re-checking the current `moss` / `mosscode` implementation.

Current constraints:

- `mosscode` targets custom model backends.
- **Auth/Login remains out of scope for now.**
- Future parity work should prioritize **concept alignment + command alignment + UI/UX alignment** with Codex CLI.
- That alignment must **not** expand `moss` kernel beyond a minimal, stable runtime core.
- `mosscode` is still in **POC stage**, so product-layer parity work may directly refactor concepts, commands, and UX flows without preserving backward compatibility.

In practice, this means:

- `moss` should own generic runtime primitives.
- `mosscode` should own Codex-style command names, product flows, interaction patterns, and TUI information architecture.

## Executive summary

### Overall judgment

`mosscode` is now **personally production-usable for a single developer, local repository, custom-model workflow**.

It is **not yet at Codex CLI product-completeness**, but the main remaining gap is now **product surface coherence**, not fundamental runtime capability.

### Why the judgment changed

Compared with the earlier review, MossCode has already closed several high-value gaps:

- trusted project / trust-aware capability loading
- shared execution policy for command + HTTP
- indexed state/history and notification surface
- profiles / task modes as runtime posture
- command-rules phase 2
- lifecycle hooks
- MCP management UX
- restore posture auto-rebuild

So the main bottleneck is no longer "missing core substrate". It is now:

1. **Codex-style concept and naming consistency**
2. **Codex-style slash command and IA consistency**
3. **Codex-style UI/UX polish for the most common workflows**

Because MossCode is still a POC, these gaps should be closed primarily through **direct product refactoring**, not long-lived compatibility layers.

## Completed parity foundation

The following work is now materially in place:

### Core safety and governance

- trust-aware loading for project config / bootstrap / skills / agents
- stronger execution policy visibility and unified command / HTTP policy
- approval / trust / profile posture persisted and enforced

### State, recovery, and durability

- file-backed session store
- indexed state/history catalog
- run trace and notification surface
- checkpoint create / fork / replay
- change apply / rollback
- restore / replay / fork auto-rebuild to recorded posture

### Extensibility and operator surfaces

- session lifecycle hooks
- pre/post tool lifecycle hooks
- MCP config management (`list` / `show` / `enable` / `disable`)
- doctor visibility for MCP and execution policy

## Codex CLI subsystem list

For parity work, Codex CLI can be decomposed into the following product subsystems:

1. **Session and workflow control**
   - interactive TUI
   - one-shot / exec mode
   - new / resume / fork / agent-thread switching

2. **Runtime posture control**
   - model selection
   - permissions / approvals
   - sandbox posture
   - trust / writable roots / plan mode

3. **Conversation and transcript control**
   - clear / compact / copy
   - status and token visibility
   - keyboard-first control surface

4. **Code review and change inspection**
   - diff
   - review
   - working tree / commit / base-branch inspection

5. **Extensibility and ecosystem**
   - MCP
   - apps / connectors
   - subagents
   - custom slash commands
   - `AGENTS.md`

6. **Diagnostics and operator tooling**
   - debug-config
   - features
   - statusline
   - theme
   - completion / install ergonomics

7. **External context and multimodal input**
   - image input
   - web search
   - file mention / fuzzy file attach
   - editor handoff

8. **Remote execution workflows**
   - cloud tasks
   - background task visibility / remote retries

## Latest subsystem-by-subsystem audit

| Codex subsystem | MossCode status | Readiness | Main remaining gap |
|---|---|---|---|
| Session and workflow control | TUI, one-shot exec, resume, checkpoint fork/replay, new session are all present | **Strong** | Codex-style thread/agent model and command naming are not unified yet |
| Runtime posture control | model / trust / approval / profile switching are implemented; posture can auto-rebuild on restore | **Strong** | needs Codex-style naming and more coherent operator UX |
| Conversation and transcript control | clear, task queueing, input history, offload/context compaction exist | **Partial** | lacks first-class Codex-compatible `/status`, `/compact`, `/copy` product surface |
| Code review and change inspection | review, git helpers, checkpoint, change apply/rollback are present | **Partial-strong** | review/diff are not yet presented with Codex-style entry points and IA |
| Extensibility and ecosystem | skills, MCP, hooks, delegated/background tasks exist | **Partial-strong** | custom slash command UX, `AGENTS.md` scaffold, and agent-thread UX remain incomplete |
| Diagnostics and operator tooling | doctor, config visibility, execution policy visibility, MCP visibility exist | **Partial** | missing Codex-style `/debug-config`, feature flags, statusline/theme/completion polish |
| External context and multimodal input | file-oriented workflows are strong, but multimodal remains limited | **Weak** | image input, web search, editor handoff, and Codex-style mention UX are not first-class yet |
| Remote execution workflows | local async tasks and schedules exist | **Partial** | no Codex-cloud-like remote task surface; thread UX also needs work |

## Personal production-readiness verdict

### Yes, for the current target use case

`mosscode` can reasonably be called **personally production-usable** when the user profile is:

- single user
- local repository
- custom model backend
- terminal-first workflow
- willing to use advanced commands rather than only polished shortcuts

This is because the high-risk production concerns are now largely covered:

- trust gating
- execution policy
- approval modes
- session durability
- checkpoint / rollback recovery
- operator diagnostics

### Not yet Codex-grade in product finish

The gap to Codex CLI is now mostly in the product shell:

- the mental model is not yet Codex-aligned
- command naming is still MossCode-native in several places
- the highest-frequency workflows are not yet surfaced through the same command and TUI grammar as Codex

## Alignment principles going forward

To reduce future divergence, parity work should follow these rules:

### 1. Directly adopt Codex-compatible concept names

If Codex and MossCode already describe the same user-facing workflow, prefer directly replacing MossCode-native naming in `mosscode` with Codex-compatible naming.

Examples:

- status
- diff
- review
- mcp
- plan
- compact
- resume
- fork
- agent

### 2. Refactor command names directly in `mosscode`

Because `mosscode` is still a POC, prefer replacing older product command names instead of maintaining compatibility aliases for long periods.

Compatibility should be preserved only where stable shared-runtime contracts, persisted data, or public reusable APIs would otherwise be put at unnecessary risk.

### 3. Align UI/UX in the product layer, not in the kernel

Codex-style command palette, help grouping, picker flows, footer/header layout, and interaction pacing should live in `mosscode`, not `moss`.

### 4. Keep `moss` kernel minimal and stable

Only sink a capability into `moss` if it is a reusable runtime primitive, not just a Codex-style product interaction.

## What should sink into `moss` vs stay in `mosscode`

| Capability area | `moss` / shared runtime | `mosscode` product layer |
|---|---|---|
| session lifecycle and persistence contracts | yes | product-specific session browsers and slash UX |
| task / background runtime abstractions | yes | agent-thread pickers and Codex-style `/agent` UX |
| execution policy / trust / approval contracts | yes | human-facing permission flows and wording |
| checkpoint / replay / rollback primitives | yes | Codex-style review, diff, and recovery navigation |
| hook/event contracts | yes | operator-facing hook management UX |
| MCP runtime lifecycle and trust-aware loading | shared runtime / appkit | `mcp` management commands, visibility, wizard UX |
| context offload / summarization primitives | yes | `/compact` UX, transcript control, copy/status flows |
| slash command registry and aliases | no | yes |
| TUI layout, statusline, picker IA, theme | no | yes |
| `AGENTS.md` scaffold and custom slash command authoring | no | yes |
| image attach, web search entry, editor handoff UX | only shared input primitives if needed | yes |

## Highest-leverage remaining gaps

### 1. TUI parity pack

The next highest-value work is a Codex-style TUI parity pack centered on:

- `/status`
- `/diff`
- `/review`
- `/mcp`
- `/plan`
- `/compact`

This is the fastest way to improve both discoverability and UX parity without destabilizing the core runtime, and it can be implemented as a direct product-shell refactor.

### 2. Thread / agent UX

MossCode already has sessions, tasks, delegation, and checkpoints, but the user-facing mental model is still not Codex-like.

The missing step is to make thread/agent navigation first-class in the product layer.

### 3. Custom command and instruction surface

Codex exposes a stronger product story around:

- `/init`
- custom slash commands
- persistent repository instructions

MossCode should align here without changing kernel abstractions unnecessarily.

### 4. Multimodal and external context

The remaining major product gap is:

- image input
- web search
- editor handoff
- mention/file attach UX

### 5. Operator polish

Still needed:

- debug-config
- feature flags
- statusline management
- theme UX
- shell completion
- install / update / release ergonomics

## Recommended next order

1. **TUI parity pack**
2. **Thread / agent UX**
3. **Custom commands + `AGENTS.md` scaffold**
4. **External context / multimodal UX**
5. **Operator polish**

## Bottom line

The current state is:

- **`moss` core: strong enough**
- **`mosscode` product: usable, but not yet Codex-consistent**

Therefore the next phase should optimize for **product convergence**, not another large round of kernel expansion.
