# Runtime Capability Pack P0 Design

> **Archived historical design note.** This file records a past design iteration and is not the canonical source for the current architecture.

## Problem Statement

Current runtime assembly and execution paths in `moss` expose four P0 risks:

- Cancellation semantics are inconsistent in queue execution paths (not all tasks honor caller context).
- Scheduler persistence runs in lock-sensitive paths and can affect runtime progress under storage slowness.
- Runtime setup (`appkit/runtime`) tolerates key bootstrap failures with warning-only behavior, weakening startup consistency.
- MCP bridge currently trusts remote tool contracts too much (input/output boundary controls are not strict enough).

Given accepted constraints:

- Scope: one planning slice, limited to the five P0 units in this spec.
- Delivery target: finish P0 in one iteration.
- Change style: one-shot switch (no long-lived feature flag dual path).
- Risk posture: high-risk acceptable for structural cleanup.
- Test mode: add tests while implementing.

## Design Goal

Evolve runtime assembly to a **modular monolith using capability packs** while preserving `kernel` as a minimal stable core.

P0 introduces three packs with strict responsibilities:

- `concurrency-pack`
- `bootstrap-pack`
- `mcp-pack`

## Architecture (Section 1 - Approved)

### Boundary Principle

- Keep `kernel/*` boundaries unchanged (`kernel/port` contract-first remains).
- Move operational governance and safety behavior into `appkit/runtime` capability packs.
- Keep `cmd/*` as entry only; no policy logic in CLI layer.

### Capability Pack Responsibilities

#### concurrency-pack

- Owns queue cancellation correctness and scheduler persistence decoupling.
- Defines runtime concurrency budgets and lane behavior contracts.

#### bootstrap-pack

- Owns startup phase orchestration and failure policy.
- Implements staged setup lifecycle: `register -> validate -> activate`.

#### mcp-pack

- Owns MCP bridge guardrails.
- Enforces schema/size/type limits and policy integration before remote execution.

## Capability Interfaces and Entrypoints

To keep packs independently testable, each pack exposes one entrypoint and one options struct in `appkit/runtime`:

- `setupConcurrencyPack(ctx, k, opts) error`
- `setupBootstrapPack(ctx, k, opts) error`
- `setupMCPPack(ctx, k, opts) error`

Contract rules:

- Entrypoints may depend on `kernel.Kernel` + `kernel/port` contracts only.
- Packs do not import `cmd/*`.
- Cross-pack calls go through explicit runtime orchestration order, not direct cyclic imports.

Observability contract:

- Runtime setup publishes capability status through:
  - `type CapabilityReporter interface { Report(ctx context.Context, capability string, critical bool, state string, err error) }`
- Default implementation logs via existing logger; tests inject a fake reporter to assert fail/degrade signals.
- MCP-policy integration contract:
  - `type ToolGuard interface { ValidateInput(ctx context.Context, tool string, input []byte) error; ValidateOutput(ctx context.Context, tool string, output []byte) error }`
  - owner: `mcp-pack`; invoked immediately before and after MCP `CallTool`.

## Critical vs Non-Critical Capability Classification

P0 classification is fixed to avoid ambiguity:

- **Critical (fail-fast on setup failure)**:
  - builtin runtime tools registration
  - runtime setup lifecycle validation
  - MCP server marked `required: true` (new optional config flag)

- **Non-critical (degraded mode with warning)**:
  - optional MCP server (`required: false` or unset)
  - optional skill load failures from discovered manifests
  - optional agent directory load failures

Degraded mode requirements:

- Emit warning log with capability name and failure cause.
- Emit runtime event `runtime.capability.degraded`.
- Continue startup only if no critical capability failed.

Config contract for MCP criticality:

- New config key under MCP-backed skill item only: `required` (boolean, default `false`).
- Behavior:
  - `required: true` and MCP init failure => setup fails.
  - `required: false` or omitted and MCP init failure => degraded warning/event.

Schema snippet:

```yaml
skills:
  - name: my-mcp
    transport: stdio
    command: npx
    args: ["-y", "@example/mcp"]
    required: true   # optional, default false
```

Migration behavior:

- Existing configs without `required` are treated as `required: false` (backward compatible default).
- For non-MCP skills, `required` is ignored by runtime setup validation and should be flagged by config linter as unsupported key.
- Validation ownership:
  - parse-time schema check in `config` package.
  - runtime enforcement in `appkit/runtime` MCP pack setup.

## Components and Data Flow (Section 2 - Approved)

### 1) Gateway / LaneQueue Flow

Current target flow:

`Inbound -> LaneQueue.Enqueue(lane, task, parentCtx) -> task(parentCtx) -> Kernel.Run(parentCtx, sess)`

P0 change:

- Lane task execution must inherit caller context (cancellation and deadlines propagate).
- Avoid `context.Background()` in async lane worker execution path for request-bound operations.

### 2) Scheduler Persistence Flow

Current pain:

- Persistence is triggered in lock-protected flows and may block progress under slow storage.

P0 target:

- Lock scope only protects in-memory job state.
- Persistence payload snapshot is built under lock; actual store write happens lock-free.
- Persistence failures are surfaced via observable error channel/log hook (no silent drop).

### 3) Runtime Setup Lifecycle Flow

P0 staged flow in `appkit/runtime`:

1. **register**: load providers/agents/capabilities.
2. **validate**: check required capability health + config validity.
3. **activate**: expose tools/hooks and mark runtime ready.

Policy:

- Critical capability failure => setup returns error (fail-fast).
- Non-critical capability failure => explicit degraded-mode signal + warning.

### 4) MCP Invocation Guarded Flow

Target guarded chain:

`tool input -> schema pre-validation -> policy check -> remote call -> response type/size validation -> local tool result`

P0 controls:

- Reject malformed JSON input before remote invocation.
- Enforce max serialized response size.
- Enforce allowed result content/type projection for local runtime consumers.

Concrete limits for deterministic behavior:

- `max_mcp_tool_input_bytes = 64 KiB`
- `max_mcp_tool_output_bytes = 1 MiB`
- `allowed_output_content_types = {"text","json"}`

If remote result exceeds limits or includes unsupported content type, return validation error and do not forward payload to downstream tool consumers.

Boundary semantics:

- input accepted iff `size_bytes <= 64 KiB`; reject iff `size_bytes > 64 KiB`.
- output accepted iff `size_bytes <= 1 MiB`; reject iff `size_bytes > 1 MiB`.
- textual output must be valid UTF-8; invalid UTF-8 => `ErrValidation`.
- JSON nesting depth must be `<= 64`; depth `> 64` => `ErrValidation`.

Validation sequence (authoritative order):

1. parse and validate input JSON schema/size
2. run policy guard (`ToolGuard.ValidateInput`)
3. call remote MCP tool
4. validate raw remote response size
5. project/normalize payload
6. validate normalized payload type + size + UTF-8/depth
7. run output guard (`ToolGuard.ValidateOutput`) and return

Byte measurement points:

- input size is measured on raw request bytes before JSON normalization.
- raw response size is measured on raw MCP response bytes before projection.
- normalized size is measured on final local payload bytes after projection/envelope serialization.

Projection rules from MCP results:

- MCP textual blocks map to local `"text"`.
- MCP structured JSON object/array/primitive (`string/number/bool/null`) maps to local `"json"`.
- Mixed payloads are normalized to a JSON envelope:
  - `{ "text": "...", "json": {...} }` when both exist.
- Binary/blob-like content is rejected in P0 with `ErrValidation`.

## Error Handling, Testing, and Migration (Section 3 - Approved)

## Error Strategy

- One-shot switch for P0 paths.
- Fail-fast on critical bootstrap integrity failures.
- Degraded behavior only for explicitly non-critical capabilities, with mandatory warning.

Error taxonomy (P0):

- `ErrValidation`: schema/type/size violations (MCP input/output guardrails).
- `ErrDependency`: required capability bootstrap failure.
- `ErrUnavailable`: optional capability failed, runtime continues in degraded mode.
- `ErrTimeout`: context deadline exceeded on queue/scheduler/remote MCP calls.
- `ErrCanceled`: caller-driven cancellation on queue/scheduler/remote MCP calls.
- `ErrRemote`: remote MCP transport/protocol/internal failure (non-timeout, non-cancel).

Handling contract:

- `ErrValidation`/`ErrDependency` in critical path => return error to caller and abort setup/operation.
- `ErrUnavailable` in optional path => warn + emit degraded event + continue.
- `ErrTimeout`/`ErrCanceled` => surface to caller; no silent retry loops in P0 unless existing bounded retry policy already applies.
- `ErrRemote` => surface to caller with reason (`transport` | `protocol` | `internal`); retry only when existing bounded retry policy marks retryable.

Timeout mapping rules:

- `context.DeadlineExceeded` => `ErrTimeout` with reason `deadline_exceeded`.
- `context.Canceled` => `ErrCanceled` with reason `canceled`.
- Tests must assert both mappings explicitly and separately.

## Testing Strategy

Add/extend tests in three groups:

1. **Concurrency and cancellation**
   - LaneQueue task cancellation propagation.
   - Ensure canceled parent context stops pending/running lane work as expected.
   - Mid-flight cancellation of queued task must complete within 200ms in test harness.

2. **Scheduler persistence safety**
   - Verify scheduler lock is not held during slow/failing store writes.
   - Verify persistence failure does not deadlock/stop in-memory scheduling.
   - Backpressure case: repeated save failures still allow next tick scheduling.

3. **MCP boundary hardening**
   - Invalid input schema rejection.
   - Oversized remote response rejection.
   - Type/content projection assertions.
   - Remote timeout/cancel path preserves context error and no goroutine leak.

Observability assertions:

- Required log markers:
  - `runtime.setup.failed` (critical)
  - `runtime.capability.degraded` (non-critical)
  - `mcp.validation.rejected` (guardrail hit)
- Required counters (or observer events if metrics backend absent):
  - `runtime_capability_failures_total{capability,critical}`
  - `mcp_validation_rejections_total{reason}`
  - `scheduler_persist_failures_total`
  - `scheduler_persist_error_drops_total`

CI observability rule:

- In CI, at least one backend must be asserted:
  - metrics counters asserted when metrics sink exists; or
  - observer events asserted when metrics sink is absent.
- It is invalid to skip both assertions.

Unit-to-observability mapping:

- P0-2 scheduler persistence:
  - emits `scheduler_persist_failures_total` and `scheduler.persist.error.drop` log on callback overflow.
- P0-3 bootstrap policy:
  - emits `runtime_capability_failures_total` and `runtime.setup.failed` / `runtime.capability.degraded`.
- P0-4 MCP hardening:
  - emits `mcp_validation_rejections_total` and `mcp.validation.rejected`.

P0 acceptance rule for observability backend:

- If metrics sink is available, assert counters.
- If metrics sink is not available, assert observer events with equivalent semantic keys.
- At least one backend must be asserted in CI (cannot skip both).

Validation gate:

- `go test ./...`

## Migration Strategy

- No dual-path feature flag for P0.
- Replace old paths directly with tests as guardrails.
- Add CI dependency-direction guard to prevent architecture regression (feature packages must not import `cmd/*`).

## Approach Alternatives Considered

### A) Capability Pack Assembly (Chosen)

Pros:

- Aligns with modular monolith target.
- Gives clean ownership per risk domain.
- Keeps kernel stable.

Cons:

- Requires medium structural edits in `appkit/runtime`.

### B) Middleware-only Governance

Pros:

- Minimal code movement.

Cons:

- Runtime assembly complexity remains centralized.
- Long-term maintainability weak.

### C) Push Governance Into Kernel

Pros:

- Strong central enforcement.

Cons:

- Expands kernel responsibility, weakens minimal-core principle.

## P0 Execution Units

### Unit P0-1: LaneQueue Context Semantics

- Files: `gateway/lanequeue.go`, `gateway/gateway.go`, related tests.
- Outcome: task context inheritance and cancellation correctness.

### Unit P0-2: Scheduler Persistence Decoupling

- Files: `scheduler/scheduler.go`, `scheduler/store.go`, tests.
- Outcome: lock-free persistence writes and explicit failure observability.
- Observability channel contract:
  - `Scheduler` gets optional `OnPersistError func(error)` callback.
  - callback runs lock-free and must be non-blocking from scheduler perspective.
  - callback invocation policy: async fire-and-forget with bounded channel size 64; on overflow, drop newest, increment `scheduler_persist_error_drops_total`, and emit warning log `scheduler.persist.error.drop`.
  - if callback is nil, fallback to logger warning.

### Unit P0-3: Runtime Setup Criticality Policy

- Files: `appkit/runtime/runtime.go` (+ helper split if needed), tests.
- Outcome: staged setup lifecycle with critical fail-fast policy.
- Acceptance:
  - builtin tool registration failure causes `Setup(...)` error.
  - runtime validation failure causes `Setup(...)` error.
  - when required MCP fails, `Setup(...)` returns error.
  - when optional skill fails, startup continues and degraded event is emitted.

### Unit P0-4: MCP Contract Hardening

- Files: `mcp/mcp.go`, policy/middleware integration touchpoints, tests.
- Outcome: strict input/output guards at MCP boundary.
- Acceptance:
  - input >64KiB rejected with `ErrValidation`.
  - output >1MiB rejected with `ErrValidation`.
  - unsupported content type rejected with `ErrValidation`.
  - mixed text+json payload normalized into envelope consistently.

### Unit P0-5: Dependency Direction Guardrail

- Files: `.github/workflows/architecture-guard.yml` + `testing/arch_guard.ps1` (single rule implementation path).
- Outcome: automated prevention of reverse dependency drift.
- Acceptance:
  - CI fails if non-`cmd/*` packages import `cmd/*`.
  - workflow invokes exactly `pwsh ./testing/arch_guard.ps1`.

## Out of Scope (P0)

- Full runtime package re-layout beyond capability ownership boundaries.
- Kernel public API redesign.
- Large multi-iteration refactors for non-critical legacy modules.

## Success Criteria

- All P0 units merged with passing tests.
- Startup consistency improved: critical setup failures abort startup deterministically.
- Request cancellation behavior is preserved through queue paths.
- Scheduler remains responsive under persistence slowness/failure.
- MCP bridge rejects invalid/unsafe payloads by policy.
- CI blocks dependency-direction regressions.
