# Extensions Consolidation Design (Option C)

## Problem Statement

The current `extensions/*` subtree mostly acts as composition glue. This creates package sprawl, duplicate wiring surfaces, and extra cognitive load when tracing runtime behavior (skills, agents, MCP, context, planning, session store).  

Goal: keep `kernel` minimal, but move extension orchestration into a single app-facing runtime layer with clearer ownership.

## Scope

In scope:

- Consolidate extension orchestration into `appkit/runtime`.
- Migrate internal imports (`appkit`, `cmd`, `examples`, `userio/tui`) to the new runtime path.
- Keep `extensions/*` as temporary compatibility shims for exactly two releases (N and N+1), then remove at N+2.
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
- Remove at Release N+2.

## API Surface Plan

Introduce canonical app APIs:

- `appkit.BuildKernelWithRuntime(ctx context.Context, flags *AppFlags, io port.UserIO, opts ...runtime.Option) (*kernel.Kernel, error)`
- `appkit/runtime.Setup(ctx context.Context, k *kernel.Kernel, workspaceDir string, opts ...runtime.Option) error` (canonical replacement for `extensions/defaults.Setup`)
- `runtime.Option` model:
  - `runtime.WithBuiltinTools(enabled bool)`
  - `runtime.WithMCPServers(enabled bool)`
  - `runtime.WithSkills(enabled bool)`
  - `runtime.WithProgressiveSkills(enabled bool)`
  - `runtime.WithAgents(enabled bool)`
  - `runtime.WithSessionStore(store session.SessionStore)`
  - `runtime.WithPlanning(enabled bool)`
- appkit forwarding helpers:
  - `appkit.WithRuntimeDefaults()`
  - `appkit.WithRuntimeProgressiveSkills()`
  - `appkit.WithRuntimeNoMCPServers()`

Legacy paths:

- Existing `extensions/*` entrypoints continue through shims during transition.

## Data Flow and Behavior Preservation

### Skills

- Discovery paths and precedence remain unchanged (including `.agents`, `.agent`, app dir, legacy `.moss`, global paths).
- Progressive semantics unchanged:
  - `list_skills` lists discovered manifests and loaded state.
  - `activate_skill` loads by name with explicit errors on invalid input.
- Interface boundary:
  - Input: `workspace`, runtime config, kernel skill manager state.
  - Output: manifest set + registered/activated providers.
  - Errors: invalid manifest parse, duplicate/unknown activation target, registration failures.

### MCP

- Continue merged config load from global + project config.
- Preserve enabled/isMCP filtering behavior.
- Interface boundary:
  - Input: merged config skill entries.
  - Output: registered MCP-backed providers.
  - Errors: non-fatal per-server load warnings, fatal only on runtime bootstrap contract violations.

### Agents

- Preserve default project/global agent directory loading behavior.
- Interface boundary:
  - Input: workspace + home directory roots.
  - Output: loaded agent descriptors in registry.
  - Errors: per-directory load warnings without hard-failing kernel boot unless registry contract fails.

### Runtime Contracts (explicit unit boundaries)

| Unit | Inputs | Outputs | Fatal Errors | Non-fatal Errors |
|------|--------|---------|--------------|------------------|
| `setup` | app flags + runtime options + workspace | fully wired kernel runtime | invalid option combinations, missing required deps | none |
| `skills` | workspace paths + skill manager + progressive flag | manifests + registered/activated skill providers + progressive tools | progressive tool registration failure | parse/register warnings for individual skills |
| `mcp` | merged config skills entries | registered MCP providers | config read/merge contract failure | per-server load warnings |
| `agents` | workspace/home agent directories | populated agent registry | registry initialization failure | per-directory parse/load warnings |
| `sessionstore` | session store config/instance | attached store + optional context/offload hooks | store creation/attach failure | none |
| `bootstrap` | workspace + app name | loaded bootstrap context | context loader contract failure | missing optional files |
| `context/planning` | enabled flags + store/session deps | registered runtime tools/hooks | tool registration failure | none |

## Error Handling

- Keep explicit error propagation and contextual wrapping.
- No broad catches.
- Preserve current warning behavior for non-fatal load failures (parse/load warnings).
- Fatal/non-fatal definitions used in this document:
  - Fatal: returns non-nil error from runtime setup/build and blocks kernel boot/session creation.
  - Non-fatal: logs warning and continues runtime setup.

## Testing Strategy

### Unit and Integration

- Port and adapt existing tests from `extensions/defaults`, `extensions/skillsx`, and adjacent glue packages into `appkit/runtime`.
- Add runtime integration tests for:
  - skills discovery + progressive activation
  - MCP loading
  - agent registry load
- Required parity matrix before shim removal:
  - Skills:
    - discovered list includes project/global paths and precedence ordering
    - `/skills`-facing data includes active/inactive states
    - progressive activation idempotency (`already_loaded`) preserved
  - MCP:
    - only enabled + MCP-typed entries register
    - invalid server configs warn and continue
  - Agents:
    - project/global directories scanned
    - malformed agent configs warn and continue
  - Defaults setup:
    - built-in core tools registration parity
    - `WithoutXxx`/`WithProgressiveSkills` option parity

### Stability/Regression

- Keep `/skills` TUI behavior validated (user skill listing + activation state + runtime built-in distinction).
- Validate with:
  - `go test ./...`
  - `go build ./...`

### Migration Safety

- Compile-time checks for new import surfaces in internal call-sites.
- Temporary shim tests to ensure parity during deprecation window.
- Deprecation warning mechanism:
  - During N and N+1, each shim package includes `Deprecated:` doc comments and changelog migration entries.
  - CI enforcement:
    - Add a repository check script using `go list -deps` + grep to fail if non-shim packages import `github.com/mossagents/moss/extensions/`.
    - Allowlist only shim package tests during N and N+1.
    - Remove allowlist entries at N+2.

### Legacy-to-Runtime Symbol Mapping (authoritative)

- `extensions/defaults.Setup` -> `appkit/runtime.Setup`
- `extensions/defaults.WithoutBuiltin` -> `appkit/runtime.WithBuiltinTools(false)`
- `extensions/defaults.WithoutMCPServers` -> `appkit/runtime.WithMCPServers(false)`
- `extensions/defaults.WithoutSkills` -> `appkit/runtime.WithSkills(false)`
- `extensions/defaults.WithProgressiveSkills` -> `appkit/runtime.WithProgressiveSkills(true)`
- `extensions/skillsx.Manager` -> `appkit/runtime.SkillsManager`
- `extensions/skillsx.Manifests` -> `appkit/runtime.SkillManifests`
- `extensions/skillsx.SetManifests` -> `appkit/runtime.SetSkillManifests`
- `extensions/skillsx.EnableProgressive` -> `appkit/runtime.EnableProgressiveSkills`
- `extensions/skillsx.RegisterProgressiveTools` -> `appkit/runtime.RegisterProgressiveSkillTools`
- `extensions/agentsx.Registry` -> `appkit/runtime.AgentRegistry`

## Migration Plan

Version binding:

- N = first release containing `appkit/runtime` (planned `v0.4.0`)
- N+1 = migration grace release (planned `v0.5.0`)
- N+2 = shim removal release (planned `v0.6.0`)

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
- Timeline contract:
  - Release N: ship `appkit/runtime` + deprecation shims.
  - Release N+1: keep shims and publish migration warnings in docs/changelog.
  - Release N+2: remove shims and legacy imports.

Exit criteria:

- Users have migration guidance and equivalent runtime APIs.

### Phase 4 — Remove Shims

- Delete deprecated shims at Release N+2.
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
