---
goal: Consolidate extension orchestration into appkit/runtime and retire thin extensions adapters
version: 1.0
date_created: 2026-03-27
last_updated: 2026-03-27
owner: Core Runtime Team
status: Planned
tags: [architecture, refactor, migration, runtime]
---

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

This implementation plan executes the approved architecture decision to move extension orchestration from `extensions/*` into `appkit/runtime`, while preserving runtime behavior for skills, MCP, agents, context/planning/session-store flows, and then removing deprecated shims at the defined release milestone.

## 1. Requirements & Constraints

- **REQ-001**: Create canonical runtime composition package at `appkit/runtime` and make it the only internal orchestration path.
- **REQ-002**: Preserve behavior parity for skills discovery/progressive activation (`list_skills`, `activate_skill`), MCP loading, and agent registry loading.
- **REQ-003**: Replace internal imports from `extensions/*` to runtime APIs in `appkit`, `cmd`, `examples`, and `userio/tui`.
- **REQ-004**: Provide migration-compatible symbol mapping from old `extensions/*` entry points to runtime equivalents during transition.
- **SEC-001**: Keep explicit error propagation with no broad catch/silent failure behavior changes.
- **CON-001**: Keep `kernel` feature ownership unchanged; consolidation is app-layer only.
- **CON-002**: Follow release timeline contract: N=`v0.4.0`, N+1=`v0.5.0`, N+2=`v0.6.0` for shim removal.
- **GUD-001**: Use deterministic option conflict rules:
  - `WithSkills(false)` + `WithProgressiveSkills(true)` => fatal setup error.
  - repeated identical option keys => last-write-wins.
  - `WithSessionStore(nil)` => fatal setup error.
- **PAT-001**: Reuse existing behavior and tests from `extensions/defaults`, `extensions/skillsx`, `extensions/agentsx`, `extensions/toolbuiltins`, `extensions/contextx`, `extensions/planningx`, `extensions/sessionstore`.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Create `appkit/runtime` with complete orchestration APIs and parity-focused tests.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Create package `appkit/runtime` and add `setup.go` implementing `Setup(ctx context.Context, k *kernel.Kernel, workspaceDir string, opts ...Option) error` with deterministic option resolution and conflict validation. |  |  |
| TASK-002 | Implement runtime option model in `appkit/runtime/options.go` including `WithBuiltinTools`, `WithMCPServers`, `WithSkills`, `WithProgressiveSkills`, `WithAgents`, `WithSessionStore(session.SessionStore)`, `WithPlanning`, and explicit validation helpers. |  |  |
| TASK-003 | Port `extensions/skillsx/skills.go` into `appkit/runtime/skills.go` and preserve progressive tool names and behavior (`list_skills`, `activate_skill`). |  |  |
| TASK-004 | Port `extensions/agentsx/agents.go` into `appkit/runtime/agents.go` and preserve registry behavior. |  |  |
| TASK-005 | Port `extensions/toolbuiltins/builtins.go` integration into `appkit/runtime/builtintools.go` (runtime wiring layer, not kernel ownership change). |  |  |
| TASK-006 | Implement `appkit/runtime/mcp.go` by lifting MCP load logic currently in `extensions/defaults/setup.go` and preserving enabled/MCP filtering and warning behavior. |  |  |
| TASK-007 | Implement `appkit/runtime/context.go`, `planning.go`, `sessionstore.go`, `bootstrap.go` by consolidating thin adapters from `extensions/contextx`, `extensions/planningx`, `extensions/sessionstore`, `extensions/bootstrapctx`, `extensions/compactx`, `extensions/scheduling`, `extensions/gatewayx` where applicable. |  |  |
| TASK-008 | Port and adapt tests into `appkit/runtime/*_test.go`, reusing cases from `extensions/skillsx/skills_test.go`, `extensions/defaults/setup_test.go`, `extensions/contextx/context_test.go`, `extensions/planningx/planning_test.go`, `extensions/compactx/compact_test.go`. |  |  |

### Implementation Phase 2

- GOAL-002: Switch all internal call-sites to runtime APIs and keep compile/test parity.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-009 | Update `appkit/builder.go` to replace `extensions/defaults.Setup` dependency with `appkit/runtime.Setup` integration and runtime options mapping. |  |  |
| TASK-010 | Update `appkit/extensions.go` helper functions to forward to runtime APIs and remove direct imports of thin `extensions/*` packages. |  |  |
| TASK-011 | Update `appkit/deepagent.go` and `appkit/serve.go` to consume runtime option surfaces and remove remaining `extensions/*` direct composition dependencies. |  |  |
| TASK-012 | Update import/wiring in `cmd/moss/main.go`, `cmd/moss/tui.go`, and `userio/tui/app.go` to runtime package APIs where extension-specific APIs are used (`skillsx` replacement paths). |  |  |
| TASK-013 | Update all repository examples referencing `extensions/*` imports to runtime/appkit canonical APIs (`examples/**/main.go`, `examples/**/chatservice.go`). |  |  |
| TASK-014 | Run `go test ./...` and `go build ./...`; capture and fix all breakages from import/API migration before proceeding. |  |  |

### Implementation Phase 3

- GOAL-003: Provide deterministic compatibility gate and controlled shim deprecation window.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-015 | Convert `extensions/defaults`, `extensions/skillsx`, `extensions/agentsx`, `extensions/toolbuiltins`, `extensions/contextx`, `extensions/planningx`, `extensions/sessionstore`, `extensions/bootstrapctx`, `extensions/compactx`, `extensions/scheduling`, `extensions/gatewayx` into forwarding shims to runtime APIs with `Deprecated:` package comments. |  |  |
| TASK-016 | Add migration notes to `docs/changelog.md`, `docs/architecture.md`, and any extension-facing docs (`docs/skills.md`) including old->new symbol mapping. |  |  |
| TASK-017 | Add CI enforcement script at `scripts/check-no-extensions-imports.ps1` (or equivalent existing scripts location): fail if non-shim production packages import `github.com/mossagents/moss/extensions/`; allowlist only shim tests for N/N+1. |  |  |
| TASK-018 | Add/adjust tests validating shim behavior parity and deprecation window policy; ensure no internal production package newly imports `extensions/*`. |  |  |

### Implementation Phase 4

- GOAL-004: Remove compatibility shims at N+2 and finalize architecture.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-019 | At release N+2 branch, delete shim packages under `extensions/*` that only forward to runtime. |  |  |
| TASK-020 | Remove deprecated-path tests and allowlist exceptions from CI enforcement. |  |  |
| TASK-021 | Run full validation (`go test ./...`, `go build ./...`) and fix regressions after physical removal. |  |  |
| TASK-022 | Update final docs to remove transition language and present `appkit/runtime` as sole supported composition path. |  |  |

## 3. Alternatives

- **ALT-001**: Keep `extensions/*` unchanged and only document architecture intent; rejected because it does not reduce package sprawl or duplicate composition paths.
- **ALT-002**: Partial merge (only thin wrappers) while keeping `skillsx/agentsx`; rejected because high-traffic orchestration paths remain split and cognitive overhead stays high.
- **ALT-003**: Move orchestration into `kernel`; rejected because it violates established ownership boundary (kernel should remain minimal/runtime-agnostic).

## 4. Dependencies

- **DEP-001**: Existing session store interfaces in `kernel/session` (`SessionStore`) must remain stable for runtime option typing.
- **DEP-002**: Existing behavior contracts in `extensions/defaults/setup.go` and tests must be ported before import migration.
- **DEP-003**: Documentation updates in `docs/architecture.md` and `docs/changelog.md` are required for release timeline communication.
- **DEP-004**: CI workflow must support running the new import enforcement script.

## 5. Files

- **FILE-001**: `appkit/runtime/setup.go` — canonical runtime setup entrypoint.
- **FILE-002**: `appkit/runtime/options.go` — runtime option model and conflict validation.
- **FILE-003**: `appkit/runtime/skills.go` — skill state/progressive tool orchestration.
- **FILE-004**: `appkit/runtime/agents.go` — agent registry orchestration.
- **FILE-005**: `appkit/runtime/builtintools.go` — builtin tools runtime wiring.
- **FILE-006**: `appkit/runtime/mcp.go` — MCP loading integration.
- **FILE-007**: `appkit/runtime/context.go` — context/offload orchestration.
- **FILE-008**: `appkit/runtime/planning.go` — planning hooks/tools orchestration.
- **FILE-009**: `appkit/runtime/sessionstore.go` — session store and persistence integration.
- **FILE-010**: `appkit/runtime/bootstrap.go` — bootstrap context load wiring.
- **FILE-011**: `appkit/builder.go` — switch from defaults setup to runtime setup.
- **FILE-012**: `appkit/extensions.go` — forward helper APIs to runtime package.
- **FILE-013**: `userio/tui/app.go` — replace `extensions/skillsx` references with runtime skill APIs.
- **FILE-014**: `extensions/**` — temporary forwarding shims + deprecation comments.
- **FILE-015**: `scripts/check-no-extensions-imports.ps1` — enforcement script.
- **FILE-016**: `docs/architecture.md`, `docs/changelog.md`, `docs/skills.md` — migration docs.

## 6. Testing

- **TEST-001**: Runtime parity tests for skills discovery precedence (project/global paths), activation state, and progressive tool idempotency.
- **TEST-002**: Runtime parity tests for MCP enabled/MCP filtering and per-server warning behavior.
- **TEST-003**: Runtime parity tests for agent directory scanning and malformed config warning behavior.
- **TEST-004**: Runtime parity tests for bootstrap/context/planning/sessionstore behavior against existing extension tests.
- **TEST-005**: Option conflict tests (`WithSkills(false)+WithProgressiveSkills(true)`, `WithSessionStore(nil)`).
- **TEST-006**: TUI smoke tests validating `/skills` output semantics remain unchanged after runtime API switch.
- **TEST-007**: CI import guard tests ensuring non-shim production packages do not import `extensions/*`.
- **TEST-008**: Repository-wide validations: `go test ./...` and `go build ./...`.

## 7. Risks & Assumptions

- **RISK-001**: Import-path churn may break external consumers during N/N+1 if migration guidance is missed.
- **RISK-002**: Subtle behavior drift in progressive skills or MCP load path if parity tests are incomplete.
- **RISK-003**: Shim-window enforcement can be bypassed without strict CI gating.
- **ASSUMPTION-001**: The release train can honor N=`v0.4.0`, N+1=`v0.5.0`, N+2=`v0.6.0`.
- **ASSUMPTION-002**: Existing tests in `extensions/*` accurately capture intended behavior and are suitable parity baselines.
- **ASSUMPTION-003**: Runtime package naming (`appkit/runtime`) is accepted as stable public surface.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-03-27-extensions-consolidation-design.md`
- `appkit/builder.go`
- `extensions/defaults/setup.go`
- `extensions/skillsx/skills.go`
