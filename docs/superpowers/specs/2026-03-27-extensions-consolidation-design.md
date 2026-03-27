# Extensions Consolidation Design (Option C)

## Problem Statement

The current `extensions/*` subtree mostly acts as composition glue. This creates package sprawl, duplicate wiring surfaces, and extra cognitive load when tracing runtime behavior (skills, agents, MCP, context, planning, session store).  

Goal: keep `kernel` minimal, but move extension orchestration into a single app-facing runtime layer with clearer ownership.

## Scope

In scope:

- Consolidate extension orchestration into `appkit/runtime`.
- Migrate internal imports (`appkit`, `cmd`, `examples`, `userio/tui`) to the new runtime path.
- Keep `extensions/*` as temporary compatibility shims for one release window, then remove.
- Preserve current behavior for skills discovery/progressive activation, MCP loading, and agent registry loading.
- Update architecture/changelog/migration docs.

Out of scope:

- Changing `kernel` core ownership boundaries.
- Reworking session model semantics.
- New end-user features unrelated to consolidation.

## Design Principles

- Preserve behavior, reduce indirection.
- Keep feature boundaries explicit and testable.
- Prefer one canonical runtime composition API.
- No silent fallback behavior changes.

## Target Architecture

### 1) Runtime Composition Boundary

Create `appkit/runtime` as the sole orchestration package for app-level runtime capabilities.

`kernel` remains feature-agnostic and unchanged.

### 2) Package Map

New package structure:

- `appkit/runtime/setup.go`
  - Replaces `extensions/defaults.Setup` responsibilities.
- `appkit/runtime/skills.go`
  - Replaces `extensions/skillsx` state management and progressive tools (`list_skills`, `activate_skill`).
- `appkit/runtime/agents.go`
  - Replaces `extensions/agentsx` registry glue.
- `appkit/runtime/builtin_tools.go`
  - Replaces `extensions/toolbuiltins` wiring wrapper.
- `appkit/runtime/mcp.go`
  - MCP load path from merged config.
- `appkit/runtime/bootstrap.go`
- `appkit/runtime/context.go`
- `appkit/runtime/planning.go`
- `appkit/runtime/sessionstore.go`
  - Merge thin `*x` extension packages currently used only as assembly adapters.

`appkit/extensions.go` remains a high-level helper surface, but its internals target `appkit/runtime`.

### 3) Transitional Compatibility

`extensions/*` becomes a short-lived shim layer:

- Forward calls/types to `appkit/runtime`.
- Mark as deprecated in comments/docs.
- Remove after one release window.

## API Surface Plan

Introduce canonical app APIs:

- `appkit.BuildKernelWithRuntime(...)`
- `appkit.WithRuntime...` helper options

Legacy paths:

- Existing `extensions/*` entrypoints continue through shims during transition.

## Data Flow and Behavior Preservation

### Skills

- Discovery paths and precedence remain unchanged (including `.agents`, `.agent`, app dir, legacy `.moss`, global paths).
- Progressive semantics unchanged:
  - `list_skills` lists discovered manifests and loaded state.
  - `activate_skill` loads by name with explicit errors on invalid input.

### MCP

- Continue merged config load from global + project config.
- Preserve enabled/isMCP filtering behavior.

### Agents

- Preserve default project/global agent directory loading behavior.

## Error Handling

- Keep explicit error propagation and contextual wrapping.
- No broad catches.
- Preserve current warning behavior for non-fatal load failures (parse/load warnings).

## Testing Strategy

### Unit and Integration

- Port and adapt existing tests from `extensions/defaults`, `extensions/skillsx`, and adjacent glue packages into `appkit/runtime`.
- Add runtime integration tests for:
  - skills discovery + progressive activation
  - MCP loading
  - agent registry load

### Stability/Regression

- Keep `/skills` TUI behavior validated (user skill listing + activation state + runtime built-in distinction).
- Validate with:
  - `go test ./...`
  - `go build ./...`

### Migration Safety

- Compile-time checks for new import surfaces in internal call-sites.
- Temporary shim tests to ensure parity during deprecation window.

## Migration Plan

### Phase 1 — Introduce Runtime Package

- Create `appkit/runtime` and copy logic with tests.

Exit criteria:

- New runtime package tests pass.
- No behavior regressions in targeted integration tests.

### Phase 2 — Switch Internal Call-Sites

- Update imports and wiring in:
  - `appkit`
  - `cmd/*`
  - `examples/*`
  - `userio/tui`

Exit criteria:

- Internal code no longer depends on `extensions/*` directly.

### Phase 3 — Compatibility Shim Window

- Keep `extensions/*` forwarding to runtime.
- Mark as deprecated in code/docs/changelog.

Exit criteria:

- Users have migration guidance and equivalent runtime APIs.

### Phase 4 — Remove Shims

- Delete deprecated shims after the defined window.
- Remove dead code/tests.

Exit criteria:

- `extensions/*` thin layers fully removed.
- Documentation reflects final architecture.

## Risks and Mitigations

- Risk: import churn breaks downstream users.
  - Mitigation: compatibility shim window + migration guide.
- Risk: subtle behavior drift in skills/MCP.
  - Mitigation: parity tests lifted from current packages.
- Risk: broad refactor destabilizes CI.
  - Mitigation: phased rollout with phase exit criteria and full test/build gates.

## Acceptance Criteria

- `appkit/runtime` is the canonical runtime composition layer.
- Internal import graph no longer requires thin `extensions/*` packages.
- Skills/MCP/agents behavior remains equivalent.
- All tests/build pass.
- Docs include migration and deprecation/removal notes.
