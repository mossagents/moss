# P1 Security & Policy Convergence Design

## Problem Statement

P0 and early P1 landed multiple security controls (policy middleware, MCP hardening, budget governance), but denial semantics and observability were not fully unified across all enforcement paths.

Primary gaps addressed in this P1 slice:

- deny/approval/allow audit fields were partially inconsistent (`reason_code`, `policy_rule`).
- middleware denial errors were not uniformly structured for downstream execution consumers.
- MCP guard boundary existed in design intent, but explicit `ToolGuard` ownership was not fully codified.
- execution events on failure paths did not always carry machine-readable `error_code`.
- RBAC deny path returned generic deny signal without structured policy reason.

## Design Goals

- Unify policy decision output shape across deny/require_approval/allow paths.
- Preserve backward compatibility (`errors.Is(err, ErrDenied)` remains valid).
- Ensure execution/audit surfaces expose stable machine-readable fields.
- Make MCP guard responsibility explicit and isolated from policy decision logic.
- Keep changes local to runtime/middleware/loop; no kernel contract break.

## Scope

In-scope:

- `kernel/middleware/builtins` policy and RBAC denial semantics.
- `mcp` guard interface and invocation boundary.
- `kernel/loop` error metadata propagation into `ExecutionEvent` and `OutputToolResult`.
- Regression tests for policy, loop, and MCP guard behavior.

Out-of-scope:

- Reworking policy DSL/rule language.
- Replacing observer/event types.
- New external telemetry backend.

## Architecture Decisions

### 1) Unified Denial Error Model

- Introduce `PolicyDeniedError` with structured fields:
  - `tool_name`
  - `reason_code`
  - `reason`
  - `enforcement`
- Keep compatibility:
  - `errors.Is(err, ErrDenied)` remains true through `Unwrap()`.
- Provide conversion to kernel structured error:
  - `AsKernelError() -> errors.ErrPolicyDenied` with metadata.

### 2) Policy Decision + Audit Consistency

- Ensure deny/approval/allow matched-rule paths consistently carry:
  - `reason_code`
  - `reason`
  - `policy_rule` (when rule-matched)
- Keep strictness precedence unchanged:
  - `deny > require_approval > allow`.
- Improve same-decision merge preference to preserve metadata-bearing results.

### 3) MCP ToolGuard Boundary

- Codify `ToolGuard` interface in `mcp` package:
  - `ValidateInput(ctx, tool, input)`
  - `ValidateOutput(ctx, tool, output)`
- Default implementation wraps existing MCP validation limits.
- Ownership boundary:
  - policy middleware: decision + approval + audit semantics.
  - MCP ToolGuard: transport-adjacent input/output guardrail checks.

### 4) Execution Event Standardization

- Introduce shared execution error metadata enrichment in loop:
  - add `error_code` from kernel error code classification.
  - include structured error meta when available.
- Apply on key failure paths:
  - `llm.completed` (error case)
  - `tool.completed` (middleware/tool failure)
  - `run.failed` / `run.cancelled`.
- Ensure `OutputToolResult` meta includes consistent machine-readable fields where available (`error_code`, `reason_code`, etc.).

### 5) RBAC Convergence

- RBAC deny now returns `PolicyDeniedError` with code:
  - `rbac.role_denied`
- This aligns RBAC with policy-deny semantics without changing rule evaluation behavior.

## Data / Event Contract

Required event fields on error paths:

- `error` (human-readable)
- `data.error_code` (machine-readable)

When policy-derived:

- `reason_code`
- `reason`
- `enforcement` (if available)
- `policy_rule` (if rule matched)

### Normative Field Contract

| Surface | Field path | Type | Requiredness | Population rule | Example |
|---|---|---|---|---|---|
| `execution_event` (`policy.rule_matched`, allow decision) | `reason_code` | string | required | policy result reason code for allow/require/deny when present | `"command.rule_allow"` |
| `execution_event` (`policy.rule_matched`, allow decision) | `data.policy_rule` | string | required iff concrete rule matched | rule name or match pattern | `"git-read"` |
| `execution_event` (`policy.rule_matched`, require_approval decision) | `reason_code` | string | required | policy reason code for approval requirement | `"tool.requires_approval"` |
| `execution_event` (`approval.requested`) | `reason_code` | string | required | copied from approval request reason code | `"tool.requires_approval"` |
| `execution_event` (`approval.requested`) | `data.policy_rule` | string | required iff concrete rule matched | propagated from matched policy rule metadata | `"git-push"` |
| `execution_event` (`approval.resolved`, approved=true) | `reason_code` | string | required | same as approval.requested | `"tool.requires_approval"` |
| `execution_event` (`approval.resolved`, approved=true) | `data.approved` | boolean | required | resolved approval decision | `true` |
| `execution_event` (`approval.resolved`, approved=false) | `reason_code` | string | required | same as approval.requested | `"tool.requires_approval"` |
| `execution_event` (`approval.resolved`, approved=false) | `data.approved` | boolean | required | resolved approval decision | `false` |
| `execution_event` (`llm.completed`, error case) | `error` | string | required | set from underlying error string | `"llm failed"` |
| `execution_event` (`llm.completed`, error case) | `data.error_code` | string | required | `kernel/errors.GetCode(err)` | `"LLM_CALL"` |
| `execution_event` (`tool.completed`, error case) | `error` | string | required | set from tool/middleware error string | `"tool call denied by policy"` |
| `execution_event` (`tool.completed`, error case) | `data.error_code` | string | required | normalized kernel code | `"POLICY_DENIED"` |
| `execution_event` (`tool.completed`, policy-derived) | `data.reason_code` | string | required | from policy denial metadata | `"tool.denied"` |
| `execution_event` (`tool.completed`, policy-derived) | `data.reason` | string | optional | from policy denial metadata | `"tool is denied by policy"` |
| `execution_event` (`tool.completed`, policy-derived) | `data.enforcement` | string | optional | from policy decision | `"hard_block"` |
| `execution_event` (`tool.completed`, rule-matched) | `data.policy_rule` | string | required | from matched policy rule name/match | `"git-push"` |
| `execution_event` (`run.failed`) | `error` | string | required | run terminal error string | `"llm failed"` |
| `execution_event` (`run.failed`) | `data.error_code` | string | required | normalized kernel code | `"LLM_CALL"` |
| `execution_event` (`run.cancelled`) | `error` | string | required | cancel reason string | `"context canceled"` |
| `execution_event` (`run.cancelled`) | `data.error_code` | string | required | normalized kernel code | `"INTERNAL"` |
| `output_message` (`tool_result`, error case) | `meta.error_code` | string | required | normalized kernel code | `"POLICY_DENIED"` |
| `output_message` (`tool_result`, policy-derived) | `meta.reason_code` | string | required | policy-derived reason code | `"tool.denied"` |
| `output_message` (`tool_result`, policy-derived) | `meta.reason` | string | optional | policy-derived reason | `"tool is denied by policy"` |
| `output_message` (`tool_result`, policy-derived) | `meta.enforcement` | string | optional | policy-derived enforcement | `"hard_block"` |

Policy rule presence rule:

- `policy_rule` is required iff decision source is a concrete matched rule.
- `policy_rule` must be absent for default/fallback decisions without a concrete matched rule.

Canonical code formats:

- `error_code`: must be uppercase kernel error code from `kernel/errors` (e.g. `POLICY_DENIED`, `LLM_CALL`).
- `reason_code`: must be lowercase dotted policy reason namespace (e.g. `tool.denied`, `rbac.role_denied`, `command.rule_denied`).

## Testing Strategy

Regression tests must validate:

- deny path error + event reason consistency.
- approval request/resolved branch consistency for approved and denied outcomes.
- allow rule match emits `policy_rule` metadata.
- MCP custom guard is invoked for input/output and can block before remote call.
- loop tool denied path carries `error_code` and `reason_code` in tool-result/event metadata.
- loop LLM and run failure events include `error_code`.

Validation gate:

- `go test ./kernel/... ./mcp ./appkit/runtime/...`
- `go test ./...`

## Migration / Compatibility

- No feature flag or dual path.
- Existing callers checking `ErrDenied` continue to work.
- New structured metadata is additive and backward compatible for observers/IO consumers.

### Compatibility Matrix

| Surface / Behavior | Before | After | Compatibility |
|---|---|---|---|
| Denial identity | generic `ErrDenied` | `PolicyDeniedError` wrapping `ErrDenied` | compatible via `errors.Is(err, ErrDenied)` |
| Policy code access | mostly string message parsing | structured `reason_code` + `error_code` | additive |
| RBAC deny | returned `ErrDenied` | returns `PolicyDeniedError` (`rbac.role_denied`) | compatible via `errors.Is(err, ErrDenied)` |
| Execution error events | `error` often present, `error_code` uneven | `error` + required `data.error_code` on listed failure surfaces | additive |
| Tool result IO meta | partial keys | standardized `error_code` and policy metadata when available | additive |

No existing integration must change to keep basic deny detection, but consumers may optionally adopt the new structured metadata.

### Deployment Safety

- Pre-rollout check: baseline event parser tolerates unknown/additional fields.
- Post-rollout check: verify sampled events include required keys:
  - `llm.completed(error) -> data.error_code`
  - `tool.completed(error) -> data.error_code`
  - `run.failed/run.cancelled -> data.error_code`
- Consumer check: ensure dashboards/alerts relying on `error` string continue to function.
- Rollback trigger: missing required keys in sampled events or consumer parse failures.
- Rollback method: revert the convergence commits for this slice as a single batch.

## Risks & Mitigations

- Risk: downstream consumers may assume sparse event data.
  - Mitigation: only additive metadata; existing keys unchanged.
- Risk: over-coupling loop to middleware error types.
  - Mitigation: conversion via interface (`AsKernelError`) and generic code extraction fallback.

## Acceptance Criteria

- `errors.Is(err, ErrDenied)` remains true for both policy deny and RBAC deny paths.
- Policy deny and RBAC deny expose expected reason codes in tests:
  - policy deny: `tool.denied`
  - RBAC deny: `rbac.role_denied`
- Approval branch consistency is asserted:
  - `approval.requested.reason_code == approval.resolved.reason_code`
  - `approval.resolved.data.approved` equals actual branch outcome for approved and denied cases.
- Allow branch consistency is asserted:
  - for concrete allow rule match, `policy.rule_matched.data.policy_rule` is present.
  - for default/fallback allow (no rule match), `policy_rule` is absent.
- For policy-derived tool errors, `execution_event(tool.completed).data.error_code` and `output_message(tool_result).meta.error_code` are non-empty and equal to normalized kernel code.
- `execution_event` includes `data.error_code` on:
  - `llm.completed` (error case)
  - `run.failed`
  - `run.cancelled`
- Custom MCP `ToolGuard` can block input before remote MCP transport call, and output validation runs after remote call result creation.
- Validation gate passes:
  - `go test ./kernel/... ./mcp ./appkit/runtime/...`
  - `go test ./...`
