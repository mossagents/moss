# Review / Apply / Rollback v1 Design

## Problem

`moss` and `mosscode` already have most of the raw primitives needed for production-safe code change control:

- repository state capture
- structured patch apply / revert
- git-backed patch journal
- session persistence
- checkpoint / fork / replay
- review-oriented repo and snapshot reporting

What is still missing is a product-level closed loop that turns those primitives into an operator-friendly workflow:

1. inspect candidate changes
2. explicitly apply a patch
3. record what changed and how to recover
4. roll back a specific applied change later

The goal of this milestone is not to make the agent auto-write code into the worktree. The goal is to make code application and rollback explicit, auditable, and production-safe.

## Goals

- Add a first-class product workflow for `review`, `apply`, and `rollback`
- Keep apply and rollback explicit operator actions
- Persist a change record for every successful apply
- Capture a pre-apply recovery point for every change
- Prefer precise rollback through reverse patch application
- Expose degraded recovery clearly when exact rollback is unavailable
- Reuse existing substrate instead of introducing a second recovery stack

## Non-goals

- No agent auto-apply by default
- No worktree/branch promotion workflow in v1
- No hidden or implicit "apply current dirty tree" behavior
- No replacement of checkpoint or snapshot systems
- No attempt to make rollback silently succeed when exact reconstruction is unavailable

## Recommended Approach

The recommended approach is a lightweight `change operation` layer above the existing kernel and sandbox primitives.

This layer lives in product/runtime and `mosscode` surfaces. It does not alter the core agent loop semantics. Instead, it coordinates existing capabilities into a controlled lifecycle:

1. `review` inspects repo state and recovery artifacts
2. `apply` takes an explicit patch payload
3. the system captures a recovery point before changing the repo
4. the patch is applied through the existing structured patch API
5. a durable change record is written
6. `rollback` targets that specific change record

This gives `mosscode` a production-usable operator workflow without forcing a heavier branch/worktree orchestration model yet.

## Alternatives Considered

### A. Patch-journal first

Expose existing patch apply/revert primitives directly and let users operate on `patch_id`.

Pros:

- fastest to ship
- minimal code churn
- directly aligned with current sandbox capabilities

Cons:

- too low-level for product use
- weak operator UX
- poor discoverability and auditing
- little room for richer policy later

### B. Change operation layer (recommended)

Wrap patch application and recovery in a product-level change record.

Pros:

- balanced UX and implementation cost
- preserves explicit operator control
- auditable and scriptable
- reuses existing primitives cleanly
- leaves room for richer policy and approvals later

Cons:

- introduces a new persisted record type
- requires CLI/TUI surface work in addition to core helpers

### C. Checkpoint-first transaction

Treat every apply as a checkpoint transaction and use checkpoint restore as the primary rollback path.

Pros:

- strong safety story
- naturally integrates with existing checkpoint work

Cons:

- heavier than needed for v1
- too coarse for routine patch rollback
- makes simple code application feel expensive

### D. Branch/worktree-first

Apply changes in an isolated branch or worktree, then promote.

Pros:

- closest to large-team production workflows
- excellent isolation

Cons:

- highest implementation cost
- requires more product surface design
- unnecessary for first production-safe milestone

## Architecture Boundaries

The v1 architecture introduces a new product-facing concept: `change operation`.

The change operation layer:

- belongs in product/runtime plus `mosscode` CLI/TUI surfaces
- coordinates existing repo, patch, and checkpoint primitives
- does not change the agent message loop
- does not let the model mutate the repo implicitly

The existing layers continue to own their current responsibilities:

- `sandbox`: repo capture, patch apply, patch revert, patch journal
- `kernel`: session persistence, checkpoint creation, replay/fork orchestration
- `product/runtime`: review reports and new change-operation orchestration
- `examples/mosscode`: user-facing commands and rendering
- `userio/tui`: interactive slash command entrypoints and status messaging

This keeps the mutation path explicit and isolates product workflow from the core LLM execution loop.

## Data Model

Introduce a persisted `ChangeOperation` record with the following fields:

- `id`
- `session_id`
- `patch_id`
- `checkpoint_id`
- `summary`
- `target_files`
- `status`
- `degraded`
- `degraded_reason`
- `created_at`
- `rolled_back_at`

Recommended semantics:

- `id`: product-facing identifier for rollback and inspection
- `session_id`: persisted session lineage if the change originated from a session
- `patch_id`: patch journal identity when exact reverse application is possible
- `checkpoint_id`: optional pre-apply checkpoint for broader recovery
- `summary`: short operator-facing description of what was applied
- `target_files`: files touched by the patch
- `status`: `applied` or `rolled_back`
- `degraded`: whether recovery metadata is incomplete or rollback had to downgrade
- `degraded_reason`: explicit reason for downgrade or limited recovery

The record should also retain the pre-apply repository capture used for fallback rollback. This may be embedded or stored as a referenced artifact, but it must be durable enough to support later rollback in the normal product path.

## Recovery Model

Every successful apply should capture a recovery point before mutating the repo.

Recovery point strategy:

1. always capture repository state through the existing repo-state capture path
2. when a persisted session is available, attempt to create a checkpoint as well
3. do not fail apply solely because checkpoint creation is unavailable
4. fail apply if the repo capture required for fallback recovery cannot be created

This keeps exact rollback lightweight while still attaching a stronger session-aware recovery primitive whenever available.

## Command Surface

### CLI

Keep `review` as the inspection entrypoint and add explicit mutation surfaces:

- `mosscode review [status|snapshots|snapshot <id>|changes|change <id>]`
- `mosscode apply --patch-file <path> [--summary <text>] [--session <id>] [--json]`
- `mosscode rollback --change <id> [--json]`
- `mosscode changes list [--limit N] [--json]`
- `mosscode changes show <id> [--json]`

Notes:

- `apply` requires explicit patch input, initially from a file path
- `rollback` operates on `change id`, not raw `patch_id`
- `review changes` is a convenience alias over `changes list`
- `review change <id>` is a convenience alias over `changes show`

### TUI

Add slash commands that mirror the CLI:

- `/changes list [limit]`
- `/changes show <id>`
- `/apply <patch_file> [summary...]`
- `/rollback <change_id>`

TUI behavior should:

- surface clear success/failure system messages
- show whether rollback was exact or degraded
- keep transcript continuity for inspection commands
- avoid silently switching sessions during apply/rollback

## Apply Semantics

`apply` should have strict, explicit behavior:

- only accept explicit patch input
- never infer a patch from the current dirty tree
- capture the pre-apply repo state first
- attempt checkpoint creation when a persisted session is supplied or inferable
- call the existing structured patch-apply port
- persist a `ChangeOperation` only after apply succeeds

If patch apply fails:

- no change record is created
- the error is surfaced directly
- no success-shaped fallback is returned

## Rollback Semantics

`rollback` should only operate on a persisted applied change that has not already been rolled back.

Rollback order:

1. load the `ChangeOperation`
2. validate it is in `applied` status
3. try exact reverse application through `patch_id`
4. if exact patch rollback is unavailable, fall back to the saved pre-apply repo capture
5. persist the rollback result and mark the change record accordingly

Exact rollback is the preferred path. Fallback rollback is allowed, but it must set:

- `degraded = true`
- `degraded_reason` with a concrete explanation

Examples of degraded rollback causes:

- patch journal entry missing
- patch reverse application unavailable in current kernel
- recovery had to use repo capture instead of reverse patch

There must be no silent downgrade that looks identical to an exact rollback.

## Persistence

Introduce a small persisted store for change operations, aligned with the existing app-dir-backed storage style used elsewhere in the product.

Requirements:

- list recent operations
- load by ID
- create operation
- update operation after rollback
- preserve enough recovery metadata for exact or degraded rollback reporting

This store should be product-owned rather than kernel-owned because the concept is primarily an operator/product workflow layer, not a fundamental runtime execution primitive.

## Reporting and UX

The operator should always be able to answer:

- what change was applied
- where it came from
- what files it touched
- what recovery point was captured
- whether rollback is still available
- whether a rollback was exact or degraded

Recommended output details:

- change ID
- patch ID when present
- checkpoint ID when present
- session ID when present
- target files
- created / rolled back timestamps
- status
- degraded flag and reason

`review` output should expand naturally to include recent change operations so the operator can inspect repo state and mutation history from one surface.

## Failure Handling

The design intentionally avoids broad fallback behavior.

Rules:

- do not claim apply succeeded unless patch apply succeeded
- do not claim exact rollback if recovery used repo capture fallback
- do not allow rollback for already rolled-back changes
- do not accept missing patch input for apply
- do not silently skip recovery-point creation required for rollback safety

When checkpoint creation fails but repo capture succeeds, apply may continue and should record that checkpoint recovery is unavailable.

When exact rollback fails and repo capture succeeds, rollback may continue as degraded and must report that degraded state in both human-readable and JSON output.

## Implementation Slices

1. Add product-level `ChangeOperation` types and file-backed store
2. Add runtime helpers to create recovery points, apply patches, persist operations, and roll back operations
3. Extend review reporting with change-operation list/detail modes
4. Add CLI commands for `apply`, `rollback`, and `changes`
5. Add TUI slash commands for `changes`, `apply`, and `rollback`
6. Add regression tests across runtime, sandbox, CLI, and TUI surfaces

## Testing Plan

At minimum, cover four layers.

### Product/runtime

- apply creates a recovery point and persisted change operation
- rollback transitions an operation from `applied` to `rolled_back`
- degraded rollback is marked and explained
- review modes can list and show change operations

### Sandbox

- exact reverse rollback via patch journal still works
- fallback repo-capture rollback works when patch rollback is unavailable
- unavailable cases surface explicit errors

### CLI (`examples/mosscode`)

- `apply` validates required patch input
- `changes list/show` produce stable human and JSON output
- `rollback --change <id>` reports exact vs degraded outcomes correctly
- `review changes` and `review change <id>` behave as aliases

### TUI

- `/changes list` and `/changes show <id>` are discoverable and work
- `/apply` success and validation failures are surfaced clearly
- `/rollback` success and degraded rollback states are surfaced clearly
- help text and slash completion include the new commands

## Acceptance Criteria

This milestone is complete when:

- a successful apply produces a durable change record
- the record can be listed and inspected later
- rollback can target that record and revert the change
- degraded rollback is explicit in both text and JSON output
- `go test ./...` passes from repo root
- `go build ./...` passes from repo root

## Future Follow-ups

Likely follow-up work after v1:

- allow patch input from richer sources than file path
- integrate change operations with checkpoint detail views
- add policy/approval rules around who may apply or roll back
- support branch/worktree-based promotion workflows
- attach observability/cost data to mutation operations for stronger auditing
