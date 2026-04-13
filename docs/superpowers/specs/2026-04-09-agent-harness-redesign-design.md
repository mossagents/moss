# Agent Harness Redesign

> **Archived historical design note.** This file records a past design iteration and is not the canonical source for the current architecture.

## Problem

Moss already has a strong agent harness foundation, but four core surfaces are still too loosely coupled for a production-grade execution model:

1. Tool execution semantics are too coarse for planner-led concurrency.
2. Persistent memory is still partly file-shaped rather than record-governed.
3. Multi-agent orchestration has strong substrate primitives but not a strong supervisor control plane.
4. Context, execution, memory, and orchestration do not yet share one stable fact model.

This redesign intentionally does **not** preserve backward compatibility. The goal is to replace the current execution, memory, and multi-agent semantics with a cleaner architecture.

## Scope

This is an **umbrella design** that defines one target architecture and three **sequential subprojects**:

1. **Phase 1 - Execution-first**
2. **Phase 2 - Structured memory**
3. **Phase 3 - Supervisor control plane**

Implementation planning must be done **per phase**, not as one large implementation plan.

## Goals

- Make planner-led concurrency explicit and runtime-governed.
- Promote tool metadata from display/risk hints to true execution semantics.
- Make structured memory records the only source of truth.
- Make multi-agent execution supervisor-owned instead of session-mesh-owned.
- Give context, execution, memory, and orchestration one shared fact layer.

## Non-Goals

- Backward compatibility with current tool scheduling behavior.
- Long-term dual-stack support for markdown-first memory.
- Preserving the current loose multi-agent collaboration semantics.
- Rewriting unrelated product/UI layers.

## User-Approved Design Decisions

- Overall approach: **Execution-first**
- Concurrency policy: **Planner may propose concurrency; runtime keeps hard constraints**
- Memory model: **Structured memory records are the source of truth**
- Multi-agent model: **Strong supervisor orchestration**

## Target Architecture

The redesign introduces one control substrate and three ordered subprojects.

### Control substrate

The common substrate is:

- **ToolSpec V2** for execution semantics
- **Execution plan contract** between planner and runtime
- **Scheduler / arbiter** that admits or rejects planner concurrency
- **Execution event ledger** as the shared fact stream

Everything after that consumes stable execution facts rather than inferring state from prompt text or ad hoc session state.

### Ordered subprojects

#### Phase 1 - Execution-first

Build the execution substrate first:

- Replace the current risk-first tool routing model with an effect-first model.
- Let the planner emit an execution plan with dependencies, proposed parallel groups, and intended scopes.
- Add a runtime scheduler that arbitrates the plan and applies hard constraints.
- Emit normalized execution events for all admitted, denied, started, finished, and committed actions.

#### Phase 2 - Structured memory

Rebuild memory on top of execution facts:

- Store structured records as the only canonical memory format.
- Treat markdown outputs (`MEMORY.md`, `memory_summary.md`, rollout summaries) as generated projections.
- Consolidate summary, snapshot, and promotion paths into one governance pipeline.
- Make memory updates event-derived, not prompt-text-derived.

#### Phase 3 - Supervisor control plane

Build strong orchestration on top of execution and memory:

- Make the supervisor the owner of task graph, budgets, retry policy, cancellation, escalation, and synthesis.
- Constrain child agents through explicit task contracts rather than prompt-only conventions.
- Use mailbox and A2A as transport primitives, not final scheduling authority.

## Core Components

### 1. ToolSpec V2

Tool metadata must become executable policy and scheduling input.

Field distinction:

- `effects` are fine-grained execution labels used by planner, scheduler, and execution plans
- `side_effect_class` is the coarse-grained routing/approval bucket used for policy, persistence, and governance defaults

Required fields:

- `effects`
- `resource_scope`
- `lock_scope`
- `idempotent`
- `side_effect_class`
- `approval_class`
- `planner_visibility`
- `commutativity_class`

Representative effect classes:

- `read_only`
- `writes_workspace`
- `writes_memory`
- `external_side_effect`
- `graph_mutation`

Canonical enum values:

- `side_effect_class`
  - `none`
  - `workspace`
  - `memory`
  - `network`
  - `process`
  - `task_graph`
- `approval_class`
  - `none`
  - `policy_guarded`
  - `explicit_user_approval`
  - `supervisor_only`
- `planner_visibility`
  - `visible`
  - `visible_with_constraints`
  - `hidden`
- `commutativity_class`
  - `non_commutative`
  - `target_commutative`
  - `fully_commutative`

`planner_visibility` semantics:

- `visible`: planner may schedule the tool normally.
- `visible_with_constraints`: planner may schedule the tool, but must emit explicit `resource_scope` and `lock_scope`; runtime rejects plans that omit them.
- `hidden`: planner cannot schedule the tool directly.

`resource_scope` should be a tagged resource selector rather than free text. Allowed roots:

- `workspace:<path-or-glob>`
- `memory:<namespace-or-record-prefix>`
- `network:<scheme-or-host>`
- `process:<executor-surface>`
- `graph:<task-or-session-scope>`

`lock_scope` must be normalized before scheduling and should follow the same root model, but without globs after normalization.

`risk` may remain as a UI/approval hint, but it is no longer the primary scheduling primitive.

### 2. Execution Plan Contract

The planner should no longer return only a flat list of tool calls. It should return an execution plan that includes:

- tool calls
- dependency edges
- proposed parallel groups
- expected scopes/effects
- rationale for parallelization

This preserves planner agency while giving runtime a structure it can validate.

Minimum schema:

```json
{
  "calls": [
    {
      "call_id": "string",
      "tool_name": "string",
      "arguments": {},
      "declared_effects": ["read_only"],
      "resource_scope": ["workspace:docs/**"],
      "lock_scope": ["workspace:docs/architecture.md"],
      "depends_on": [],
      "parallel_group": "read-batch-1",
      "rationale": "safe parallel reads"
    }
  ]
}
```

Contract rules:

- `call_id`, `tool_name`, and `arguments` are required.
- `declared_effects`, `resource_scope`, and `lock_scope` must be present even if empty.
- `depends_on` forms a DAG; cycles are invalid.
- `parallel_group` is optional and only meaningful when the planner proposes parallel execution.
- `rationale` is optional for single-call execution and required when `parallel_group` is set.
- `parallel_group` is advisory only; runtime may split or serialize it.
- unknown effect or scope tags are invalid in no-compat mode.

Planner/runtime boundary:

- the planner must emit this contract as structured JSON output inside turn planning, using the runtime's structured-output path rather than freeform text parsing
- runtime must validate the JSON contract before any scheduling step
- runtime must not infer missing scopes/effects after the fact; invalid contracts are rejected as planner errors
- no tool execution starts before contract validation succeeds

### 3. Scheduler / Arbiter

Runtime remains the final authority.

The scheduler:

- validates execution plans
- detects scope conflicts
- admits safe batches
- serializes or rejects conflicting work
- enforces budget and approval ceilings

Examples of hard constraints:

- overlapping `workspace` write scopes cannot run concurrently
- `writes_memory` commit operations cannot overlap with conflicting memory writes
- `external_side_effect` calls are serialized unless `commutativity_class` permits target-safe parallelism
- graph-mutation operations are serialized

Conflict algorithm:

1. Normalize each `resource_scope` and `lock_scope` into canonical tags.
2. Build a conflict set per call:
   - any `writes_*` effect conflicts with another call touching the same normalized scope
   - `external_side_effect` conflicts with any other call sharing the same `network:*` or `process:*` lock scope unless both calls are `idempotent=true` and `commutativity_class=target_commutative` with distinct normalized targets, or `commutativity_class=fully_commutative`
   - `graph_mutation` conflicts with all other `graph:*` mutations
3. Admit calls in dependency order.
4. Within each ready set, pick the conflict-free subset using deterministic ordering: topological-ready order first, then original planner order, then lexical `call_id`.
5. Remaining ready calls are deferred to later batches.

Child-contract widening detection:

- every child-submitted execution plan must be validated against the issued child task contract before scheduler admission
- if any declared effect, writable scope, or memory scope exceeds the contract, runtime emits a supervisor error event and rejects the plan version before execution starts

Denied-call protocol:

- a denied call is terminal for the current admitted plan version
- runtime emits a `denied` event with structured reason data
- the planner may produce a new plan version in a later turn using the denial event as input
- runtime never auto-retries a denied call inside the same plan version

Budget signals available to the scheduler:

- session-level step budget
- session-level token budget
- child-contract budget, when execution is supervisor-managed

If no supervisor-managed child contract exists yet, Phase 1 scheduling uses session budgets only.

Partial execution policy:

- Partial admission is allowed.
- Once a batch is admitted, it is not rolled back automatically by the scheduler.
- Compensation, if any, belongs to higher-level policy or supervisor logic, not the scheduler.

### 4. Execution Event Ledger

Every tool execution becomes a normalized event stream.

Required event stages:

- `planned`
- `admitted`
- `denied`
- `started`
- `finished`
- `committed`

Each event should carry:

- `event_id`
- `session_id`
- `task_id`
- `tool_name`
- `phase`
- `effects`
- `resource_scope`
- `lock_scope`
- `approval_state`
- `error`, if any

This ledger is the shared fact model used by memory and supervisor layers.

Event stage semantics:

- `finished`: the tool handler returned a result or error
- `committed`: all runtime-visible side effects of the call were accepted as canonical outcomes and may be consumed by downstream systems

For `read_only` calls, `finished` and `committed` may occur in the same runtime step.
For write or external-side-effect calls, `committed` is emitted only after scheduler/policy/runtime bookkeeping accepts the outcome as durable fact.

Commit trigger and crash semantics:

- the runtime execution coordinator is responsible for emitting `committed`
- `committed` is emitted only after the result and all required bookkeeping are durably written to the ledger store
- if a process crash occurs after `finished` but before `committed`, downstream consumers must treat the call as non-canonical and replay/recovery logic must decide whether to mark it failed or resubmit it

Phase 1 persistence contract:

- The ledger must be durable across process boundaries.
- The default implementation should be an append-only SQLite-backed store under the runtime data directory.
- Memory governance and supervisor layers may rely on replay from the durable ledger.
- In-memory ledgers are only acceptable in tests.

Default schema sketch:

- `events(event_id, session_id, task_id, tool_name, phase, ts, payload_json)`
- indexes on `(session_id, ts)`, `(task_id, ts)`, `(tool_name, ts)`

### 5. Memory Governance Service

Memory consumes execution facts and owns persistence rules.

Canonical record fields should include:

- `record_id`
- `scope`
- `kind`
- `status`
- `canonical_subject`
- `source_event_id`
- `summary`
- `content`
- `confidence`
- `supersedes`
- `expires_at`
- `promoted_by`
- `workspace`
- `cwd`
- `git_branch`

Canonical enum values:

- `scope`
  - `session`
  - `task`
  - `agent`
  - `workspace`
  - `project`
  - `user`
- `kind`
  - `event_snapshot`
  - `working_summary`
  - `promoted_fact`
  - `policy_note`
  - `execution_artifact`
- `status`
  - `active`
  - `superseded`
  - `expired`
  - `rejected`

Memory responsibilities:

- snapshot generation
- consolidation
- promotion
- supersession
- expiry handling
- derived view rendering

Governance triggers:

- **snapshot generation**: emitted when either (a) a `committed` event has effect class `writes_workspace`, `writes_memory`, `external_side_effect`, or `graph_mutation`, or (b) at least 3 `committed` events in the same task/session window share the same `canonical_subject`
- **consolidation**: merges multiple event snapshots into a shorter working summary
- **promotion**: moves a working summary or event snapshot into a `promoted_fact` when `confidence >= 0.8` and the fact is corroborated by at least 2 distinct source events or 2 distinct task/session windows
- **supersession**: marks prior active records as replaced by a newer canonical record
- **expiry**: deactivates records whose `expires_at` has elapsed

Confidence and corroboration rules:

- `confidence` is a normalized float in `[0.0, 1.0]`
- default confidence scoring:
  - base `0.4` for one successful source event
  - `+0.2` if the same fact appears in another distinct source event
  - `+0.2` if corroborated in another distinct task/session window
  - `+0.1` if the source event reached `committed`
  - `+0.1` if the record survived one consolidation pass without contradiction
  - cap at `1.0`
- `corroborated` means two candidate records normalize to the same `(scope, kind, canonical_subject)` tuple; summary text is explanatory output only and is not the identity key

Deterministic summary normalization:

- trim whitespace
- lowercase ASCII tokens
- collapse repeated spaces
- strip stable boilerplate prefixes added by the memory pipeline

`canonical_subject` derivation:

- `event_snapshot`: `canonical_subject = tool_name + "|" + normalized primary resource scope`
- `working_summary`: `canonical_subject = source window key + "|" + dominant resource scope`
- `promoted_fact`: `canonical_subject = promoted fact type + "|" + normalized subject key`
- `policy_note`: `canonical_subject = policy code + "|" + resource scope`
- `execution_artifact`: `canonical_subject = artifact type + "|" + source event id`

`canonical_subject` must be derived by the memory pipeline from structured event metadata, not freeform semantic similarity.

Derived views:

- `MEMORY.md` is a rendered projection of active promoted records and selected working summaries
- `memory_summary.md` is a compact rendered projection of active summaries
- rollout summaries are filtered projections by scope/kind/status

Phase 2 session-loop interaction:

- the existing session loop remains in place
- startup and dynamic prompt fragments must read from structured memory records and derived projections produced by the memory governance service
- the session loop must not treat markdown files as canonical state once Phase 2 lands

### 6. Supervisor Control Plane

The supervisor is the orchestration authority.

Responsibilities:

- own the task graph
- assign child contracts
- propagate budget, timeout, and approval ceilings
- own retry and escalation policy
- own cancellation propagation
- own final synthesis

Child agents become bounded executors rather than semi-autonomous coordinators.

Child task contract schema:

```json
{
  "task_id": "string",
  "goal": "string",
  "input_context": "string",
  "budget": {
    "max_steps": 20,
    "max_tokens": 50000,
    "timeout_sec": 600
  },
  "approval_ceiling": "policy_guarded",
  "writable_scopes": ["workspace:docs/**"],
  "memory_scope": "task",
  "allowed_effects": ["read_only", "writes_workspace"],
  "return_artifacts": ["summary", "structured_result"]
}
```

Rules:

- child agents cannot widen their own contract
- supervisor is the only authority that can reissue a broader contract
- attempted contract widening must emit a supervisor error event and fail the child task
- synthesis consumes child outputs plus ledger and memory state, not just child transcript text

Supervisor synthesis contract:

- inputs: child result artifacts, relevant ledger slices, relevant memory records
- output: one structured synthesis artifact plus optional human-readable summary

## Data Flow

The target runtime flow is:

`planner -> execution plan -> scheduler/arbiter -> tool execution -> execution event ledger -> memory governance -> supervisor synthesis`

This is the critical architectural change: later phases consume execution facts rather than reconstructing behavior from prompt history.

This flow is logically cyclic at runtime:

- supervisor may trigger new planner turns
- planner may emit new execution plans after prior ledger events
- memory projections may influence later planner context

The arrow above describes the primary fact path, not a one-pass-only pipeline.

## Phase Details

### Phase 1 - Execution-first

Primary code areas:

- `kernel/tool`
- `kernel/loop`
- `kernel/middleware/builtins/policy.go`
- `appkit/runtime/execution_policy.go`

Required changes:

- introduce ToolSpec V2
- redesign turn planning around execution plans
- replace unconditional parallel tool execution with scheduler-mediated admission
- align approval logic with effect/approval class instead of risk only

Success criteria:

- planner can propose concurrency
- runtime can reject or serialize conflicting work deterministically
- execution facts are emitted consistently for every tool call
- existing session loop remains the synthesis surface until Phase 3 introduces supervisor-owned synthesis

### Phase 2 - Structured memory

Primary code areas:

- `appkit/runtime/memory.go`
- `appkit/runtime/memory_pipeline.go`
- `appkit/runtime/context.go`
- `appkit/runtime/context_manager.go`
- memory record/store implementations

Required changes:

- treat structured records as canonical
- move summary/snapshot/projection outputs behind one governance pipeline
- render markdown views from records
- remove split ownership between old summary paths and new structured paths

Success criteria:

- one canonical memory source of truth
- deterministic regeneration of derived markdown views
- no parallel memory truth models

### Phase 3 - Supervisor control plane

Primary code areas:

- `agent/tools.go`
- `kernel/task`
- supervisor/runtime integration points in `appkit/runtime`

Required changes:

- define explicit child task contracts
- centralize retry/cancel/escalation/synthesis in supervisor
- reduce mailbox/A2A to transport and signaling primitives
- bind orchestration to execution ledger and memory state

Success criteria:

- supervisor owns task graph truth
- child agents operate within explicit resource contracts
- orchestration decisions are derived from runtime state, not prompt convention

## Error Handling

Errors must be phase-aware rather than generic execution failures.

Categories:

- **planner errors**: invalid plan, missing scopes, dependency cycles
- **scheduler errors**: scope conflict, approval ceiling failure, budget admission failure
- **execution errors**: tool handler/runtime execution failure
- **memory governance errors**: record write failure, projection failure, consolidation failure
- **supervisor errors**: invalid child contract, cancellation propagation failure, synthesis dependency failure

All errors should carry enough metadata to trace them through the new control substrate:

- `phase`
- `event_id`
- `session_id`
- `task_id`
- `tool_name`, if applicable
- `scope/effect metadata`

## Testing Strategy

### Execution tests

- planner proposes unsafe concurrency and runtime rejects it
- conflicting scopes are serialized deterministically
- non-conflicting plans can run in admitted parallel batches

### Memory tests

- one event produces one canonical memory update path
- derived markdown views can be rebuilt from records
- summary/snapshot/promotion outputs do not fork truth

### Supervisor tests

- child budget inheritance
- cancel propagation
- retry and escalation policy
- synthesis based on task graph + execution ledger + memory state

### Integration tests

- full flow from execution admission to memory update to supervisor synthesis
- approval and denial paths remain observable and deterministic

## Replace-in-Place Strategy

Because backward compatibility is out of scope, the implementation should use **replace-in-place** rather than dual-stack migration.

Replace-in-place means:

- no long-lived support for the old risk-first scheduler
- no long-lived support for markdown-first canonical memory
- no long-lived support for the old loose multi-agent coordination model

At most, short-lived import or conversion helpers may exist during implementation, but the target architecture is single-path.

## Planning Boundary

This spec is ready for planning, but **planning must happen one phase at a time**.

Recommended planning order:

1. Phase 1 implementation plan
2. Phase 2 implementation plan
3. Phase 3 implementation plan

Each phase plan should explicitly state the interfaces it consumes from the previous phase.

Umbrella interface handoff sketch:

- Phase 2 consumes:
  - ledger replay/query interface
  - committed-event schema
  - normalized scope/effect tags
- Phase 3 consumes:
  - ledger replay/query interface
  - structured memory record schema
  - child task contract schema

Minimal interface shapes:

```go
type LedgerQuery struct {
    SessionID    string
    TaskID       string
    EventStage   string
    AfterEventID string
    Limit        int
}

type LedgerStore interface {
    Append(event ExecutionEvent) error
    List(query LedgerQuery) ([]ExecutionEvent, error)
}

type MemoryRecordStore interface {
    Upsert(record MemoryRecord) error
    List(scope string, kind string, status string, limit int) ([]MemoryRecord, error)
}
```

