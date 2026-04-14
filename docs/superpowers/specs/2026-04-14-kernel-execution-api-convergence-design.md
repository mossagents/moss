# Kernel Execution API Convergence Design

## Problem

After P0 public assembly convergence, P1 execution substrate owner convergence, and P2 execution policy plane convergence, the next largest remaining execution split is the kernel execution API itself.

Today, execution is still fragmented across three overlapping contracts:

1. **Legacy sync loop execution remains the production default.**
   - `kernel/kernel.go`
     - `Run(...)`
     - `RunWithUserIO(...)`
     - `RunWithTools(...)`
   - production callers still depend on `*loop.SessionResult`
     - `apps/mosscode/commands_exec.go`
     - `contrib/tui/app.go`
     - `gateway/gateway.go`
     - `runtime/scheduled_runner.go`
     - `agent/tools.go`
     - `apps/mosswork/chatservice.go`

2. **The new agent-path API already exists, but is not the real owner yet.**
   - `kernel/kernel.go`
     - `BuildLLMAgent(...)`
     - `RunAgent(...)`
   - `RunAgent(...)` is already documented as the new primary API, but there are no production callers.

3. **`Runner` is a second orchestration shell with partially different behavior.**
   - `kernel/runner.go`
     - `Runner`
     - `Runner.Run(...)`
   - `Runner.Run(...)` still performs hidden input append and creates a parallel execution shell instead of being a thin alias of `RunAgent(...)`.

This leaves the repo with three different execution shapes at once:

- sync loop execution returning `*loop.SessionResult`
- event-stream execution through `Runner`
- event-stream execution through `RunAgent`

That fragmentation creates several architectural problems:

- the supposed canonical API (`RunAgent`) is not actually canonical
- outer layers still depend on `kernel/loop` result types instead of a lower-level session-owned result contract
- caller migration would duplicate result-folding logic unless a shared path is defined
- `Runner` preserves hidden behavior such as implicit user-message append, which conflicts with explicit session ownership
- `runSupervisor` still carries wrapper-shaped distinctions (`foreground`, `with_userio`, `delegated`) even though those wrapper APIs are what should disappear

The user explicitly chose a hard-cut direction:

- make `Kernel.RunAgent` the only canonical execution entry
- remove `Run(...)`, `RunWithUserIO(...)`, and `RunWithTools(...)`
- merge `Runner` into the agent path rather than keep a second execution shell

## Goals

- Make `Kernel.RunAgent(...)` the only canonical kernel execution entry.
- Keep `BuildLLMAgent(...)` as the canonical root-agent factory.
- Remove loop-shaped result contracts from app/runtime/gateway/agent outer layers.
- Provide one shared synchronous result-collection path for callers that still need `Output / Steps / TokensUsed`.
- Make session input mutation explicit at the call site instead of hiding it inside a runner shell.
- Delete the wrapper APIs rather than leave them as deprecated forwarders.

## Non-Goals

- Redesigning the loop engine itself.
- Redesigning agent transfer semantics beyond what is needed to converge execution entrypoints.
- Changing P2 tool-policy ownership or turn-planning semantics.
- Introducing a second public kernel execution facade after `RunAgent(...)` becomes canonical.
- Preserving backward compatibility for `Run(...)`, `Runner`, or `*loop.SessionResult`.

## User-Approved Design Decisions

- Compatibility posture: **hard cut; do not preserve backward compatibility**
- Selected direction: **`Kernel.RunAgent(...)` is the only canonical execution entry**
- Surface model: **kernel keeps build-agent + run-agent surfaces only**
- Result migration: **shared collector for sync callers, returning `session.LifecycleResult` instead of `*loop.SessionResult`**
- Data-flow rule: **callers own user-message append explicitly; execution APIs do not hide it**
- Deletion posture: **remove `Run(...)`, `RunWithUserIO(...)`, `RunWithTools(...)`, and `Runner` instead of keeping compatibility shims**

## Rejected Approaches

### 1. Keep `Run(...)` as the primary sync facade and treat `RunAgent(...)` as secondary

Rejected because it preserves loop-shaped execution as the real owner and leaves the agent-path API permanently non-canonical.

### 2. Force every caller to consume the raw event stream directly

Rejected because the current production callers mostly need the same final summary shape (`Output`, `Steps`, `TokensUsed`). Making every caller hand-roll its own collector would recreate the same duplication P3 is supposed to remove.

### 3. Keep `Runner` as an app-facing orchestration layer above `RunAgent(...)`

Rejected because `Runner` is not just a naming alias; it currently owns hidden input append and a second orchestration contract. Retaining it would preserve the split rather than close it.

## Target Architecture

After this migration, execution ownership is:

- **`kernel`** — canonical root-agent factory, canonical execution request, and canonical run/result helpers
- **`kernel/session`** — canonical synchronous summary type (`LifecycleResult`)
- **`kernel/loop`** — internal loop engine only
- **outer layers (`agent`, `gateway`, `runtime`, apps, examples`)** — consumers of `RunAgent(...)` or the shared result collector, but not owners of parallel execution contracts

### 1. Kernel keeps only build-agent and run-agent execution surfaces

Retain:

- `BuildLLMAgent(name string) *LLMAgent`
- `RunAgent(...)`

Remove:

- `Run(...)`
- `RunWithUserIO(...)`
- `RunWithTools(...)`
- `NewRunnerFromKernel(...)`
- `Runner`
- `NewRunner(...)`
- `RunnerConfig`

The kernel execution story becomes:

1. build the root agent
2. prepare or mutate the session explicitly
3. execute through `RunAgent(...)`
4. either consume events directly or fold them through the shared collector

No second runner/orchestration shell remains.

### 2. `RunAgent(...)` becomes request-shaped and absorbs the remaining wrapper variants

`RunAgent(...)` should move from positional arguments to a request-shaped API so the wrapper variants disappear without losing capability.

Recommended shape:

- `type RunAgentRequest struct`
  - `Session *session.Session` (required)
  - `Agent Agent` (required)
  - `UserContent *model.Message` (optional invocation trigger content)
  - `IO io.UserIO` (optional override; defaults to kernel IO)
  - `Tools tool.Registry` (optional override; defaults to kernel registry)

Responsibilities:

- validate required request fields
- normalize optional IO/tool overrides onto one canonical execution path
- normalize the effective IO through the same goroutine-safe wrapping currently provided by the legacy wrappers
- populate `InvocationContext.UserContent()` from the explicit request field when present
- create the invocation context once
- execute the supplied agent through the one shared event-stream path

This request shape absorbs the only real reasons the legacy wrappers existed:

- `RunWithUserIO(...)` becomes `RunAgent(..., RunAgentRequest{IO: ...})`
- `RunWithTools(...)` becomes `RunAgent(..., RunAgentRequest{Tools: ...})`

The tool-override rule must be exact, because `RunWithTools(...)` today is not merely a different registry input; it is the delegated execution path used by `agent.Delegator`, and it also runs non-interactively with `NoOpIO`.

P3 therefore requires request-scoped tool rebinding rather than a passive unused field:

- when `RunAgentRequest.Tools` is nil, the supplied agent runs with its normal tool set
- when `RunAgentRequest.Tools` is non-nil, the canonical implementation must execute a request-scoped agent instance that actually uses that registry
- for the canonical root `*LLMAgent` path, this means cloning or rebinding the agent for the duration of the request rather than mutating shared kernel state
- if a caller supplies `Tools` for an agent type that cannot honor a request-scoped tool override, `RunAgent(...)` returns an explicit error instead of silently ignoring the override

Delegated agent runs also preserve the current non-interactive behavior explicitly:

- `agent.Delegator` migration uses `RunAgentRequest{Tools: scopedTools, IO: &io.NoOpIO{}}`

The wrapper-specific run kinds should not survive as public concepts. If `runSupervisor` still needs an internal distinction for delegated runs, it may derive it from the request shape; otherwise the dead run-kind split should be removed entirely.

### 3. Sync callers use a shared collector that returns `session.LifecycleResult`

P3 should not keep a second kernel execution entry just to preserve sync callers. Instead, add one shared collector/helper on the canonical agent path that:

1. executes `RunAgent(...)`
2. consumes the event stream to completion
3. reads the authoritative terminal run result from the canonical run path
4. returns `session.LifecycleResult`

The collector returns `session.LifecycleResult`, not `*loop.SessionResult`.

That is the canonical sync summary because:

- it already exists at the lower `kernel/session` layer
- it removes outer-layer dependency on `kernel/loop`
- the only field lost from `loop.SessionResult` is duplicate `SessionID`, which the caller already has from `session.Session`

Collector semantics are **not** a best-effort guess over arbitrary agent events.

Instead:

- the canonical run path must expose an authoritative terminal result for loop-backed execution
- `LLMAgent` / loop-backed runs remain the primary supported sync-collector path because they already produce `session.LifecycleResult`
- if a caller asks to collect a sync result for an agent that does not produce an authoritative terminal result, the collector returns an explicit error instead of synthesizing one from partial stream heuristics

Collector result semantics for the supported loop-backed path:

- `Output` is the final assistant output from the terminal run result
- `Steps` comes from the final run result
- `TokensUsed` comes from the final run result
- failures still return `error` through the normal error channel rather than being silently converted into success-shaped summaries

This collector is the only sanctioned sync bridge for callers that do not want to iterate events themselves.

### 4. Input mutation becomes explicit caller behavior

`Runner.Run(...)` currently hides a behavioral decision by appending the passed user message into the session before execution.

That hidden behavior should be deleted.

After P3:

- callers append the user message to the session explicitly before execution
- callers that need top-level invocation trigger context also pass that same message through `RunAgentRequest.UserContent`
- `RunAgent(...)` executes the session it is given
- `RunAgentRequest.UserContent` populates `InvocationContext.UserContent()` only; it does **not** append or mutate session history
- there is no shadow input path inside a runner shell

This keeps session state ownership deterministic across:

- CLI flows
- TUI flows
- gateway message delivery
- scheduled jobs
- delegation/sub-agent runs

### 5. Outer-layer interfaces migrate off `loop.SessionResult`

The following public or semi-public contracts should be updated together:

1. `gateway.Kernel`
   - from `Run(ctx, sess) (*loop.SessionResult, error)`
   - to canonical `RunAgent(...)` or a smaller adapter around the shared collector

2. `runtime.ScheduledRunnerConfig`
   - `OnComplete`
   - `RunScheduledJob(...)`
   - any stored callback signatures returning `*loop.SessionResult`
   - all move to `session.LifecycleResult`

3. `agent.Delegator`
   - remove `RunWithTools(...)`
   - migrate delegation to `RunAgent(...)` with tool override in the request
   - return `session.LifecycleResult` from the shared collector rather than `*loop.SessionResult`

4. app/product/example callers
   - `apps/mosscode`
   - `contrib/tui`
   - `apps/mosswork`
   - examples
   - all stop importing `kernel/loop` for run-result consumption

The intent is not merely renaming result types. The intent is that outer layers stop depending on loop-owned execution contracts at all.

### 6. `kernel/loop` becomes an internal engine, not an outer-layer result owner

After migration, `kernel/loop` should no longer be part of the app/runtime/gateway execution contract surface.

Expected end state:

- `loop.SessionResult` has no outer-layer production callers
- if no internal callers still require it, delete it
- otherwise keep it strictly internal to loop and agent execution implementation

This is important because P3 is not complete if callers merely switch from `Run(...)` to a new wrapper while still depending on `loop.SessionResult`.

## Data Flow

### Canonical event-stream flow

1. Caller creates or restores a session.
2. Caller explicitly appends the new user turn if needed.
3. Caller builds or selects the root agent.
4. Caller executes `RunAgent(ctx, RunAgentRequest{...})`.
5. Events are streamed and materialized through the agent path.
6. Caller either:
   - handles events directly, or
   - invokes the shared collector to obtain `session.LifecycleResult`

### Canonical sync-summary flow

1. Caller prepares session state explicitly.
2. Caller calls the shared collector over `RunAgent(...)`.
3. Collector consumes the stream to completion.
4. Collector returns `session.LifecycleResult`.
5. Caller reads `sess.ID` from the session itself when it still needs session identity.

## Error Handling

- Missing required request fields in `RunAgentRequest` are explicit errors.
- Missing tool registry or IO overrides fall back only to the canonical kernel-owned defaults; no wrapper-specific behavior is preserved.
- The canonical `RunAgent(...)` path must preserve goroutine-safe IO normalization for both default IO and request-scoped IO overrides.
- `InvocationContext.UserContent()` is populated only from `RunAgentRequest.UserContent`; `RunAgent(...)` must not infer it by peeking at or mutating session history.
- Supplying `RunAgentRequest.Tools` for an agent that cannot honor request-scoped tool rebinding is an explicit error.
- If a sync caller requests a collected result and the run fails, the caller receives the run error explicitly.
- If a sync caller requests a collected result for an agent that does not produce an authoritative terminal result, the collector returns an explicit error.
- P3 must not introduce broad compatibility fallbacks such as silently recreating `Run(...)` behavior behind helper aliases.

## Migration Order

1. Introduce the request-shaped `RunAgent(...)` and the shared sync collector.
2. Migrate `gateway`, `runtime.ScheduledRunner`, and `agent.Delegator` to the new contract.
3. Migrate `apps/mosscode`, `contrib/tui`, `apps/mosswork`, and examples.
4. Delete `Run(...)`, `RunWithUserIO(...)`, `RunWithTools(...)`, `Runner`, and remaining loop-result-shaped outer interfaces.
5. Remove dead `runSupervisor` distinctions if they no longer carry behavior.

The critical rule is: **do not leave compatibility forwarders behind after migration.**

## Testing Strategy

Focused coverage should include:

1. **kernel execution API tests**
   - request validation
   - explicit `UserContent` propagation into `InvocationContext`
   - IO override path
   - IO sync-wrapping on the canonical path
   - tool-registry override path
   - explicit error on unsupported tool override
   - run supervisor / timeout behavior through the new request path
   - shared collector result folding
   - explicit error on collector use with non-result-producing agents

2. **migration call-site tests**
   - at least one migrated test each for:
     - `gateway`
     - `runtime/scheduled_runner`
     - `agent` delegation helpers
     - `contrib/tui` or `apps/mosscode`

3. **deletion-proof checks**
   - no remaining production `.go` callers of:
     - `Kernel.Run(...)`
     - `RunWithUserIO(...)`
     - `RunWithTools(...)`
     - `Runner`
     - `*loop.SessionResult` outside loop internals

Full validation remains:

- `go test ./...`
- `go build ./...`
- `Push-Location contrib\tui; go test .; Pop-Location`
