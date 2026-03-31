# Unified State Store and Indexed History Design

## Problem

`moss` and `mosscode` already persist several important kinds of runtime state, but they do so through separate stores with separate query models:

- `kernel/session.FileStore` persists sessions as one JSON file per session
- `appkit/product.FileChangeStore` persists change operations as one JSON file per change
- `kernel/port.FileCheckpointStore` persists checkpoints as one JSON file per checkpoint
- `kernel/port.FileTaskRuntime` persists task runtime state as a single JSON file
- runtime execution events are emitted through `Observer`, but there is no shared event history store

That leaves the product with several production gaps relative to Codex CLI expectations:

- no unified history surface across sessions, changes, checkpoints, tasks, and execution events
- no indexed filtering or cursor pagination
- no shared text search across goals, notes, results, and errors
- no durable event history that later notification and profile work can build on
- no single place for `doctor`, product UX, or future APIs to ask "what happened recently?"

Today each store mostly answers its own narrow `Load` / `List` use case by scanning files and sorting in memory. That works for basic local persistence, but it does not yet provide the unified indexed history surface needed for production usability.

## Goal

Deliver a first production-safe **unified state store + indexed history** baseline for `moss` / `mosscode`.

## Goals

- keep existing file-based stores as the source of truth for their own domain objects
- add one shared indexed query surface across sessions, changes, checkpoints, tasks, and execution events
- provide cursor pagination, common filters, and lightweight text search
- persist execution events so future notification and observability work can query recent activity
- make the index rebuildable from source stores instead of introducing a risky one-shot migration
- keep the first delivery small enough to ship before notifications, profiles, and MCP management UX

## Non-goals

- no immediate replacement of every existing store with a single database-backed primary store
- no distributed watch / cross-node sync protocol in this phase
- no semantic or embedding-based search in this phase
- no analytics dashboard, billing aggregation, or tenant-level reporting in this phase
- no retention-policy engine or archival system in this phase
- no attempt to redesign scheduler persistence or every example-specific local cache

## Candidate Approaches

### A. Aggregation without indexing

Add a thin API that fans out to the existing stores and merges results in memory.

Pros:

- smallest code delta
- no new persisted artifacts

Cons:

- still scans all files for every query
- no durable event history
- pagination and search stay weak
- Phase 6 notifications would still lack a real queryable history backend

### B. Hybrid source stores + sidecar index (recommended)

Keep the existing stores as the source of truth, and add one shared sidecar index that stores normalized summary records for sessions, changes, checkpoints, tasks, and execution events.

Pros:

- low migration risk because existing stores remain authoritative
- gives product and runtime one query surface
- allows backfill / rebuild instead of one-shot data conversion
- creates the right foundation for notifications, profiles, and MCP status history

Cons:

- introduces index lifecycle and backfill logic
- touches several packages and product/runtime seams

### C. Full SQLite-first storage rewrite

Move sessions, changes, checkpoints, tasks, and events to a new shared database as the primary persistence model.

Pros:

- strongest long-term consistency story
- simplest eventual query model

Cons:

- too large for the first P1 delivery
- forces store migration and compatibility work before the product gets the user-facing value
- raises rollback risk on Windows and existing example apps

## Recommendation

Choose **Approach B**.

The first P1 delivery should keep the current file stores intact and layer a rebuildable sidecar index on top. That provides the production value we need now—unified history, search, filters, pagination, and durable event history—without coupling this phase to a full storage migration.

## Scope of the first delivery

### In-scope behavior

1. add a shared state catalog that can query the following kinds:
   - `session`
   - `change`
   - `checkpoint`
   - `task`
   - `execution_event`
2. persist execution events into a durable event journal that the index can ingest
3. expose a shared query API with:
   - kind filter
   - session filter
   - status filter when applicable
   - workspace / repo-root filter when applicable
   - time-range filter
   - free-text search over normalized searchable fields
   - cursor pagination
4. make the index rebuildable from the source stores and event journal
5. surface state-store health and paths through `doctor`
6. wire the product/runtime entrypoints needed to create and query the catalog

### In-scope entrypoints

1. `appkit/runtime`
   - new state catalog / query layer
   - runtime-owned event ingestion bridge
2. `appkit/product`
   - app-dir path helpers
   - doctor visibility
3. `kernel/port`
   - event journaling bridge built on top of existing `Observer` / `ExecutionEvent`
4. existing source stores:
   - `kernel/session`
   - `kernel/port.FileCheckpointStore`
   - `appkit/product.FileChangeStore`
   - `kernel/port.FileTaskRuntime`
5. `examples/mosscode`
   - wiring for product entrypoint usage when the catalog feature is enabled

### Out-of-scope behavior

- replacing session / checkpoint / change / task persistence with one primary database
- serving remote query APIs over HTTP in this phase
- building full notifications / subscriptions in this phase
- building profile selection UX in this phase
- indexing arbitrary prompt text, full transcripts, or tool payload bodies by default
- indexing worktree snapshots or replacing `SnapshotCountsBySession(...)` in this phase

## Design

## 1. Hybrid storage model

The first delivery keeps each domain store authoritative for its own full payload:

- sessions remain in `SessionStoreDir()`
- checkpoints remain in `CheckpointStoreDir()`
- changes remain in `ChangeStoreDir()`
- tasks remain in `TaskRuntimeDir()`
- execution events become durable through a new event journal under the product app dir

On top of those sources, the product adds one **state catalog** that stores normalized summary rows used for query.

The catalog is a **derived index**, not the primary store of truth:

- if a source record changes, the catalog is updated
- if the catalog is missing or corrupted, it can be rebuilt from source stores
- catalog entries only contain the normalized fields needed for listing, filtering, search, and drill-down

This keeps rollback simple: the product can temporarily lose indexed history quality without losing the underlying source data.

### Feature gate and rollback posture

This subsystem must ship behind an explicit product-level enable/disable switch.

Recommended controls:

- feature flag / config / env guard for catalog reads
- feature flag / config / env guard for event journaling writes
- `doctor` reports `enabled`, `disabled`, or `degraded`

Fallback-on-error is useful, but it is not enough by itself for first-rollout operations. Operators must be able to disable the feature without removing the underlying source stores.

For the first delivery, `disabled` should mean:

- no catalog reads
- no incremental catalog upserts or deletes
- no rebuild operations
- no event journal writes

The first rollout should keep this as a simple binary on/off switch rather than introducing mixed warm-index modes. If the feature is disabled, the product falls back fully to the existing source-store paths.

### Catalog backend

The catalog should use a local embedded SQLite sidecar under the app directory, while source-of-truth records stay in their existing file stores.

Recommended layout:

- `state\catalog.db`
- `state\catalog.meta.json`
- `state\events\YYYYMMDD.jsonl`

This is still a hybrid design because SQLite is used only for the derived index and query plane, not for replacing the existing source stores in this phase.

Using an embedded SQLite sidecar gives the first delivery a production-usable baseline for:

- stable sort + cursor pagination
- multi-field filtering
- simple FTS-style or normalized LIKE-based text search
- low-cost incremental upserts

The implementation should prefer a pure-Go embedded SQLite driver so Windows builds do not take on CGO requirements.

## 2. Normalized catalog model

The catalog should store one normalized row per queryable record.

Illustrative shape:

- `kind`
- `record_id`
- `session_id`
- `workspace_id`
- `repo_root`
- `status`
- `title`
- `summary`
- `search_text`
- `sort_time`
- `created_at`
- `updated_at`
- `source_version`
- `source_path`
- `metadata_json`

Each row represents a **summary**, not the full domain object.

Examples:

- a session row stores goal, mode, status, recoverability, and timestamps
- a change row stores operation type, repo root, session id, and brief file summary
- a checkpoint row stores note, lineage depth, patch count, and session id
- a task row stores agent name, goal, status, claimed-by, and dependency count
- an execution-event row stores event type, tool/model, risk, reason code, error snippet, and timestamp

### Visibility policy

The catalog must not blindly index every persisted object as user-visible history.

For `kind=session`, the first delivery should follow the same visibility boundary as current product-facing listing behavior:

- exclude hidden checkpoint snapshot sessions such as `checkpoint_snapshot_hidden`
- exclude internal recovery/offload artifacts that are marked as non-user-facing in session metadata
- only index sessions that should appear in ordinary history and resume flows

To make this enforceable, the first delivery should standardize a shared session metadata marker for non-user-facing persisted sessions, for example:

- `history_hidden=true`

Current and future internal session creators must stamp that marker when they persist synthetic sessions that should not appear in ordinary history.

For repo alignment in this phase:

- existing hidden checkpoint snapshot sessions continue to honor `checkpoint_snapshot_hidden`
- offload / compaction snapshot creators must also stamp the shared `history_hidden` marker
- the session adapter excludes either form of hidden/internal marker from user-visible `kind=session` rows

If a future phase wants internal forensic history, that should be modeled explicitly instead of leaking internal sessions into user-visible `kind=session` results.

### Checkpoint session identity

For `kind=checkpoint`, catalog `session_id` must use the **logical/original session id**, not raw `CheckpointRecord.SessionID` when those differ.

The shared normalization rule should match current product behavior:

- prefer the first lineage ref whose kind is `CheckpointLineageSession`
- fall back to raw `CheckpointRecord.SessionID` only when lineage does not contain a logical session id

This keeps session filtering and drill-down aligned with the existing checkpoint UX.

### Searchable text boundary

The first delivery should normalize a bounded `search_text` field per row.

It should include concise user-meaningful fields such as:

- session goal
- checkpoint note
- task goal / result / error
- change summary fields
- event tool name / model / error / reason code

It should **not** index full prompt transcripts, full patch bodies, or raw tool payloads in this phase.

## 3. Ownership and package boundaries

`appkit/runtime` should own the shared catalog/query layer for this phase.

Why:

- the catalog bridges multiple product/runtime stores instead of belonging to only one domain
- later phases such as notifications and profiles will also consume it from runtime/product boundaries
- this matches the Phase 1 and Phase 2 pattern where shared operational behavior is owned centrally instead of only at the shell layer

Recommended new runtime files:

- `appkit/runtime/statestore.go`
  - public construction and query surface
- `appkit/runtime/state_catalog.go`
  - catalog upsert / delete / rebuild logic
- `appkit/runtime/state_catalog_sqlite.go`
  - embedded SQLite implementation details
- `appkit/runtime/event_journal.go`
  - execution-event persistence and replay
- `appkit/runtime/state_catalog_test.go`
  - unified query / pagination / rebuild tests

Recommended product helpers:

- `appkit/product/runtime.go`
  - add `StateStoreDir()` and `StateEventDir()` helpers
  - extend `doctor` with state catalog visibility
- `appkit/product/state_observer.go`
  - helper for composing product observers with the journal observer before the final `k.SetObserver(...)`

## 4. Source adapters

Each source store should map into catalog rows through a focused adapter rather than by sharing one giant reflection-based serializer.

Recommended adapters:

1. **session adapter**
   - source: `session.SessionStore`
   - summary fields: id, goal, mode, status, recoverable, steps, created/end time
   - exclude hidden/internal sessions from user-visible catalog rows

2. **checkpoint adapter**
   - source: `port.CheckpointStore`
   - summary fields: id, logical session id, note, lineage depth, patch count, created time
   - logical session id is derived from lineage first, then falls back to raw `CheckpointRecord.SessionID`

3. **change adapter**
   - source: `product.FileChangeStore`
   - summary fields: id, session id, repo root, operation kind, affected paths summary, created time

4. **task adapter**
   - source: `port.TaskRuntime`
   - summary fields: id, agent name, goal, status, claimed by, workspace id, dependency count, created/update time
   - only persistent task-runtime implementations are indexed incrementally; memory-only task runtimes are skipped

5. **execution-event adapter**
   - source: new event journal
   - summary fields: type, session id, tool/model, risk, reason code, error, timestamp

Adapters should be responsible for normalization and for generating stable `search_text`, not for owning catalog persistence.

## 5. Event journal

This phase must make execution events durable.

The repository already emits structured `ExecutionEvent` objects through `Observer`, and checkpoint creation already emits such events. What is missing is a shared persisted event history.

The first delivery should add a new observer-backed event journal with these properties:

- append-only JSONL files under `state\events`
- one compact JSON event per line
- bounded normalized payload derived from `ExecutionEvent`
- replay support for backfill into the catalog

### Why JSONL for the journal

The journal is the **event source of truth** for execution events in this phase, while the catalog is the indexed query plane.

JSONL is a good fit here because:

- append-only writes are simple and reliable
- events remain inspectable and repairable
- the catalog can be rebuilt from journal files without replaying live sessions

### Event ingestion path

The recommended ingestion path is:

1. runtime provides a journal observer implementation plus a product helper that composes it with any existing observers
2. product bootstrap performs the final observer composition before calling the last `k.SetObserver(...)`
3. the journal observer appends normalized event records to JSONL
4. the same observer path upserts catalog rows for those events
5. if catalog upsert fails, the journal write still preserves the event for later repair

This makes event durability less fragile than tying history only to the sidecar index.

### Observer composition contract

The current repo build flow allows product entrypoints such as `examples/mosscode` to call `k.SetObserver(...)` after kernel construction.

For this phase, the contract must therefore be explicit:

- runtime does **not** rely on an earlier hidden observer attachment that a later `SetObserver(...)` could overwrite
- product wiring must call a shared helper that composes:
  - existing product observer(s)
  - pricing / trace observer(s) already in use
  - the new journal observer
- the final `k.SetObserver(...)` call installs the composed observer tree

This guarantees the journal and existing observers both receive events.

### Journal durability assumptions

The first delivery should assume **single writer per app dir**.

Within that assumption:

- the journal appender uses an in-process mutex around append writes
- replay tolerates a truncated final line by discarding the incomplete tail record
- journal files are append-only and rotated by date

Multi-process append coordination is intentionally out of scope for this phase and should be called out as a later hardening item if needed.

## 6. Query API

The first delivery should expose a runtime-owned query API that returns normalized summaries and cursors, not full raw objects.

Illustrative shape:

- `QueryState(ctx, StateQuery) (StatePage, error)`
- `RebuildStateCatalog(ctx, RebuildOptions) error`
- `CatalogHealth(ctx) (CatalogHealthReport, error)`

Illustrative query fields:

- `Kinds []StateKind`
- `SessionID string`
- `RepoRoot string`
- `WorkspaceID string`
- `Status string`
- `Text string`
- `Since time.Time`
- `Until time.Time`
- `Limit int`
- `Cursor string`

Illustrative page shape:

- `Items []StateEntry`
- `NextCursor string`
- `TotalEstimate int`

### Cursor semantics

The cursor should be based on deterministic sort order:

1. primary sort: `sort_time DESC`
2. tie-breaker: `kind ASC`
3. tie-breaker: `record_id ASC`

This avoids unstable pagination when multiple records share the same timestamp.

### Drill-down boundary

The unified query API returns summaries only.

When callers want full details, they should resolve the selected item back through the source store for that record kind.

That keeps the catalog compact and avoids duplicating every domain payload in the index.

## 7. Rebuild and consistency model

The catalog must support both incremental updates and full rebuild.

### Incremental updates

The first delivery should use **write-through wrappers / decorators** for source stores wherever possible instead of relying on scattered ad hoc post-write calls.

Recommended hook strategy:

- `IndexedSessionStore` wraps `session.SessionStore` and mirrors `Save` / `Delete`
- `IndexedChangeStore` wraps `FileChangeStore` and mirrors `Save` / `Delete`
- `IndexedTaskRuntime` wraps persistent `port.TaskRuntime` and mirrors `UpsertTask`
- checkpoint rows are updated through a small shared checkpoint-indexing helper used by checkpoint creation paths and by rebuild
- execution-event rows are updated through the composed journal observer

This makes update/delete behavior explicit and lowers the risk of index drift across different write paths.

The first delivery should therefore update the catalog at the same logical write boundaries as source records:

- session save / completion paths
- checkpoint create path
- change operation save path
- task runtime upsert path when a persistent runtime is present
- execution event observer path

### Delete handling

Delete semantics must be explicit in the first delivery:

- session delete synchronously removes its `kind=session` catalog row
- change delete synchronously removes its `kind=change` catalog row
- checkpoint delete is not applicable in current store semantics
- task delete is not applicable in current task-runtime semantics
- full rebuild remains the repair path for any stale rows caused by unexpected failures

The first delivery should prefer direct row deletion over tombstones because source-of-truth delete semantics are already hard delete.

### Rebuild

The product should also support full rebuild:

1. scan source stores
2. replay event journal files
3. repopulate catalog rows
4. swap catalog atomically once rebuild succeeds

Rebuild is important because it keeps the index repairable and gives us a safe escape hatch during rollout.

### Versioning

`catalog.meta.json` should record:

- schema version
- last rebuild time
- source scan watermark(s)
- app version / build info if available

If schema version changes, the catalog should rebuild automatically rather than trying to partially migrate in place.

## 8. Doctor visibility

`mosscode doctor` should report:

- whether the state catalog feature is enabled, disabled, or degraded
- state catalog path
- event journal path
- whether the catalog is readable / writable
- catalog schema version
- whether the catalog is healthy, rebuilding, or degraded
- whether event journaling is enabled

The goal is to make history/query failures diagnosable before later phases start depending on this subsystem.

## 9. Rollout plan

The first implementation should roll out in this order:

1. add product path helpers and catalog package
2. add event journal
3. add source adapters and full rebuild command path
4. wire incremental updates from source write points
5. extend doctor
6. switch first-party product queries that currently full-scan source stores onto the catalog where safe

### Initial product usage

The first product integration should target low-risk read paths such as:

- recent session / checkpoint history
- recent task history

`resume` should remain a **hybrid path** in the first delivery:

- session history may later read from the catalog
- snapshot counts continue to come from `SnapshotCountsBySession(...)`
- if that mixed path adds too much complexity, resume listing should continue using the current direct source-store implementation for the first rollout

The catalog should not become a hard dependency for core session execution in the first rollout. If catalog reads fail, existing direct source-store behavior should remain available for protected flows such as resume and checkpoint/history rendering where a fallback already exists.

## 10. Testing

The first delivery needs both unit and integration coverage.

### Unit tests

- catalog row normalization for each source kind
- cursor pagination stability
- text-search normalization
- rebuild from mixed source stores
- journal append + replay
- schema-version-triggered rebuild
- observer composition so journal + audit / pricing observers all receive events
- checkpoint logical-session normalization from lineage
- hidden/internal session visibility filtering
- delete propagation for session / change rows
- truncated-final-line journal replay recovery

### Integration tests

- create sessions, checkpoints, changes, tasks, and execution events; verify they appear in one unified query
- restart process and verify catalog + journal survive reload
- delete or corrupt the catalog and verify rebuild recovers from source stores + journal
- validate doctor output for healthy and degraded catalog states
- verify protected flows fall back to direct source-store reads when catalog reads are disabled or fail
- verify hybrid resume behavior if resume listing uses catalog-backed session history plus direct snapshot counts
- verify non-persistent task runtime does not produce misleading indexed task history

### Validation commands

- targeted `go test` for `appkit/runtime`, `appkit/product`, `kernel/port`, and impacted integration paths
- full `go test ./... -count=1`
- `examples\mosscode` independent `go build .`

## Open follow-on work enabled by this design

This phase intentionally prepares later P1 items without implementing them yet:

- Phase 6 notifications can subscribe to recent event history from the catalog + journal
- profiles / task modes can persist and query mode transitions and task summaries
- MCP management UX can expose provider status history through the same unified query path
- Phase 3 managed governance can audit policy changes through the same history model
