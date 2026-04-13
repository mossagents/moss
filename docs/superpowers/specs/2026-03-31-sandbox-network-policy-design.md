# Sandbox and Network Policy Strengthening Design

> **Archived historical design note.** This file records a past design iteration and is not the canonical source for the current architecture.

## Problem

Phase 1 made workspace trust affect whether project-owned assets are allowed to load.

Phase 2 needs to strengthen **execution isolation** so that safety is enforced by shared runtime policy instead of by tool conventions alone.

Today the repository already has useful pieces:

- `port.ExecRequest` can express command timeout, working directory, allowed paths, environment behavior, and network policy
- `sandbox.LocalSandbox` can enforce timeout, allowed working directory roots, environment shaping, and a soft network-disable fallback
- approval middleware can deny or require approval before tool calls

But those pieces are not yet connected into one coherent control plane:

- `run_command` only forwards `command` and `args`, ignoring most of the existing execution policy surface
- `http_request` enforces basic URL validity, but not host/method/network policy
- restricted trust defaults are stronger for file/process tools than for direct network tools
- `doctor`, audit, and traces do not clearly report the effective sandbox / network posture

This leaves MossCode short of the stronger sandbox + network control expected for production use and for Codex CLI parity.

## Goal

Deliver a production-safer Phase 2 baseline that makes command execution and network access depend on a shared **effective execution policy**.

## Goals

- unify network and command-execution policy into a shared runtime policy model
- make `http_request` and `run_command` consume that policy consistently
- strengthen restricted-trust defaults without removing existing trusted workflows
- surface effective sandbox / network posture through `doctor`
- record enforcement and degradation details through existing audit / trace plumbing
- keep the phase small enough to ship before Phase 3 layered governance

## Non-goals

- no container / VM / OS-level sandbox redesign in this phase
- no per-tenant managed policy layering in this phase
- no repo-wide migration of every bespoke example tool or external extension
- no hard guarantee of network blocking on local execution when the platform cannot provide it
- no replacement of the existing approval middleware model

## Candidate Approaches

### A. Tool-local hardening only

Patch `http_request` and `run_command` independently.

Pros:

- fastest to ship
- smallest code delta

Cons:

- policy remains fragmented
- `doctor` and audit still lack a single source of truth
- Phase 3 would need to unwind duplicated logic

### B. Shared execution policy baseline (recommended)

Introduce a small shared execution-policy layer, then have approval rules, built-in tools, sandbox execution, and `doctor` consume that effective policy.

Pros:

- consistent behavior across network + process execution
- reuses existing `ExecRequest` and sandbox capabilities instead of replacing them
- creates the right seam for Phase 3 layering

Cons:

- touches several packages
- slightly broader validation surface

### C. Full isolation redesign first

Jump directly to task/workspace isolation plus stronger system-level network enforcement.

Pros:

- most complete end state

Cons:

- too large for this phase
- higher implementation and platform risk
- mixes baseline control-plane work with longer-term isolation work

## Recommendation

Choose **Approach B**.

Phase 2 should connect the runtime policy, tool handlers, sandbox enforcement, approval flow, and diagnostics into one coherent baseline, while explicitly leaving container-grade isolation and managed layering to later phases.

## Scope of Phase 2

### In-scope behavior

1. `run_command` consumes effective timeout / working-dir / allowed-path / env / network defaults instead of forwarding only `command` and `args`
2. `http_request` consumes effective network rules for scheme / method / host / timeout / redirect posture
3. restricted trust defaults become stronger for direct network access as well as command execution
4. approval-mode policy rules can express network-sensitive decisions, not only generic tool-level decisions
5. `doctor` reports effective execution-policy posture and whether enforcement is hard, soft, or degraded
6. audit / trace surfaces include network / enforcement metadata already available through approval and execution events

### In-scope entrypoints

1. `appkit/runtime/builtintools.go`
   - `httpRequestHandler()`
   - `runCommandHandler(...)`
2. `sandbox/local.go`
3. `kernel/middleware/builtins/`
   - granular policy helpers
   - approval / deny rules for network-sensitive inputs
4. `appkit/deepagent.go`
5. `appkit/product/approval.go`
6. `appkit/product/runtime.go`
7. shared runtime state in `appkit/runtime`

### Out-of-scope behavior

- containerization or OS-native hard egress blocking
- generalized policy layering from global config + project config + managed defaults
- retrofitting every example-specific tool beyond the shared built-in execution paths
- introducing a rules DSL or enterprise policy engine

## Design

## 1. Effective execution policy state

Phase 2 should introduce a small shared runtime state that represents the **effective execution policy** for the current kernel.

It should cover two surfaces only:

1. **command execution defaults**
   - command access posture (`allow`, `require-approval`, `deny`)
   - default timeout
   - maximum timeout
   - allowed working-directory roots
   - environment posture
   - command network policy

2. **direct HTTP request defaults**
   - HTTP access posture (`allow`, `require-approval`, `deny`)
   - allowed methods
   - allowed schemes
   - allowed hosts
   - maximum timeout
   - redirect posture

The goal is not to build a full policy language. The goal is to have one resolved policy object that approval setup, `doctor`, built-in tools, and sandbox / executor execution can all read without inferring enable/disable state from separate sources.

### Ownership

`appkit/runtime` owns the effective execution policy state for this phase.

- product entrypoints provide trust / approval inputs
- runtime resolves and stores the effective policy
- tool handlers and doctor read the resolved policy
- sandbox enforces the execution part of the policy when command execution happens

This keeps Phase 2 aligned with Phase 1, where shared runtime behavior—not only product shell code—owns the policy boundary.

## 2. Policy resolution inputs

Phase 2 should resolve effective policy from the inputs already present in the product:

- trust level
- approval mode
- runtime defaults

Optional operator knobs may be added as a thin flag layer if needed, but the core design must not depend on managed config layering yet.

### Single installation path

Phase 2 must have exactly one shared installation path for the effective execution policy:

1. resolve the effective execution policy once from trust + approval mode + runtime defaults
2. store it in runtime-owned state
3. derive approval rules from that same resolved policy
4. have built-in tool handlers read that same resolved policy at execution time

`appkit/product` approval setup and `appkit/deepagent` restricted defaults must both delegate to that single shared install path instead of each resolving policy independently. This keeps Phase 3 free to add layered sources without creating parallel policy-resolution code paths.

### Baseline defaults

Recommended baseline:

- default allowed HTTP schemes: `http`, `https`
- default allowed HTTP methods: `GET`, `HEAD`, `POST`
- default redirect posture: do not follow redirects automatically
- default host posture: no host allowlist is enforced unless explicit policy adds one; approval rules may still require confirmation for all network access

- **trusted + full-auto**
  - `http_request` allowed
  - `run_command` allowed
  - command execution uses bounded timeout + workspace-root path restrictions
  - command network defaults to enabled

- **trusted + confirm**
  - `http_request` requires approval
  - `run_command` requires approval
  - same bounded command defaults apply
  - command network defaults to enabled

- **restricted**
  - `http_request` requires approval
  - `run_command` requires approval
  - command execution remains path-bounded and timeout-bounded
  - command network defaults to `disabled`
  - local sandbox may degrade this to soft-limit mode and must report that degradation

- **read-only**
  - `http_request` denied
  - `run_command` denied
  - doctor reports command and direct-network execution as disabled by approval mode

## 3. Network-aware approval rules

Approval middleware already supports structured deny / require-approval decisions.

Phase 2 should add network-sensitive granular rules in `kernel/middleware/builtins`, such as:

- require approval for HTTP methods outside a safe baseline
- require approval for URL hosts outside an allowed-host list
- optionally deny hosts explicitly blocked by effective policy

These rules should operate on the tool input payload in the same way `RequireApprovalForPathPrefix(...)` and `DenyCommandContaining(...)` operate today.

### Important boundary

Approval rules decide **whether a request may proceed without human approval**.

They do **not** replace execution-time enforcement:

- `http_request` must still validate effective method / host / timeout constraints
- `run_command` must still pass the resolved `ExecRequest` restrictions into sandbox execution

## 4. `run_command` policy wiring

`run_command` should stop constructing a nearly-empty `ExecRequest`.

Instead, it should build the request from:

- the tool input (`command`, `args`)
- effective command defaults from runtime policy

That means Phase 2 should wire at least:

- timeout
- allowed working-directory roots
- environment posture
- command network policy

The existing sandbox surface already supports these fields, so the main work here is runtime/tool integration rather than sandbox redesign.

This applies to **both** built-in execution paths:

- sandbox-backed `runCommandHandler(...)`
- executor-backed `runCommandHandlerExec(...)`

The same effective policy must be forwarded into whichever execution backend is active.

### Default command posture

For this phase:

- commands should stay rooted to the workspace by default
- timeouts should always be bounded
- environment shaping should be explicit and conservative
- restricted mode should disable command network access by default

## 5. `http_request` policy wiring

`http_request` should consume the effective network policy before issuing a request.

Phase 2 should enforce:

- scheme allowlist (`http`, `https`)
- method allowlist
- timeout cap
- redirect posture
- host allowlist / approval behavior when configured

This gives direct network tools a first-class policy boundary instead of relying only on coarse tool-level approval.

### Redirect validation

If redirect following is enabled by policy, every redirect hop must be revalidated against:

- allowed scheme policy
- allowed host policy
- method / redirect posture rules

Redirects must not be able to bypass host or scheme policy by validating only the first request URL.

## 6. Doctor / audit / trace visibility

`doctor` should report the effective execution-policy posture, including:

- normalized trust
- approval mode
- whether command execution is enabled at all
- whether direct HTTP access is enabled at all
- whether each surface is `allow`, `require-approval`, or `deny`
- effective command-network mode
- whether command network enforcement is hard, soft, degraded, or unknown for the active execution backend
- effective HTTP policy summary (methods / host policy / timeout cap / redirect posture)

Phase 2 should also ensure that approval and execution events carry enough structured metadata for:

- audit logs to explain why a request was denied / approved / degraded
- traces to show that network or execution policy affected a step

## Proposed API / Surface Changes

These names are illustrative and may be adjusted to match repository style.

### runtime

- `WithExecutionPolicy(...)`
- `ExecutionPolicyOf(k *kernel.Kernel) ...`
- `ResolveExecutionPolicy(trust, approvalMode, ...) ...`

### builtins policy helpers

- `RequireApprovalForURLHost(...)`
- `RequireApprovalForHTTPMethod(...)`
- `DenyURLHost(...)`

### product / doctor

- doctor report fields for effective command and network posture

### tool handlers

- `run_command` passes resolved `ExecRequest` defaults
- `http_request` validates effective method / host / timeout / redirect policy

## Behavioral Semantics

The following semantics apply to the shared built-in execution paths covered by Phase 2.

### Trusted + full-auto

- `http_request` proceeds without approval unless denied by explicit policy
- `run_command` proceeds without approval unless denied by explicit policy
- command execution remains bounded by timeout and allowed roots
- direct HTTP defaults are `GET`/`HEAD`/`POST`, no auto-redirect, no host allowlist unless explicit policy adds one

### Trusted + confirm

- `http_request` requires approval
- `run_command` requires approval
- bounded execution defaults still apply after approval
- command network defaults to enabled
- the same default HTTP posture still applies after approval

### Restricted

- `http_request` requires approval
- `run_command` requires approval
- command execution network defaults to disabled
- if the local sandbox cannot hard-block network, it must surface soft-limit degradation explicitly
- the same default HTTP posture still applies after approval unless explicit policy narrows it further

### Read-only

- `http_request` is denied
- `run_command` is denied
- doctor reports command execution and direct HTTP access as disabled by approval mode

## Edge Cases

### Local hard network isolation unavailable

If `PreferHardBlock` is requested but the local sandbox cannot provide it:

- return an explicit error when soft fallback is not allowed
- otherwise continue with soft-limit enforcement and mark the result as degraded

### Empty allowed-host policy

An empty host allowlist means no host-level allowlist is being enforced yet. Approval rules may still require confirmation for all network access.

### Redirects

If redirects are disallowed, the request should stop at the first redirect response instead of silently following it. If redirects are enabled, each hop must be revalidated.

### Future layering

Phase 3 may add managed defaults and layered config, but it should reuse the same execution-policy state instead of introducing a parallel model.

## Testing Strategy

Add tests for:

1. **policy resolution**
   - effective policy differs correctly across `trusted/full-auto`, `trusted/confirm`, `restricted`, and `read-only`
   - approval rules are derived from the same installed policy object rather than a parallel code path

2. **network approval rules**
   - host-based approval / denial
   - method-based approval

3. **`http_request` enforcement**
   - allowed scheme / method / timeout behavior
   - redirect posture
   - redirect revalidation across every hop
   - host policy enforcement

4. **`run_command` enforcement**
   - timeout is forwarded into sandbox execution
   - timeout is forwarded into executor-backed execution
   - working directory remains within allowed roots
   - environment posture is forwarded correctly
   - restricted mode forwards disabled command-network policy

5. **local sandbox behavior**
   - disabled command network falls back to soft-limit with degradation markers when allowed
   - hard-block-only requests fail explicitly when unavailable

6. **doctor reporting**
   - read-only mode renders command / network disabled state correctly
   - effective command-network mode renders correctly
   - degraded / soft / unknown enforcement state renders correctly
   - effective HTTP policy summary renders correctly

7. **audit / trace integration**
   - approval / execution events include enough metadata to explain network-policy decisions

## Implementation Steps

### Step 1

Introduce effective execution-policy state and resolution helpers in shared runtime code.

### Step 2

Add network-sensitive granular policy rules and wire them into approval-mode / restricted-trust defaults.

### Step 3

Update `http_request` and `run_command` to consume the resolved policy.

### Step 4

Extend doctor / audit / trace visibility for sandbox and network posture.

### Step 5

Add tests and validate with targeted package tests plus full repository tests.

## Success Criteria

This phase is done when:

- `run_command` uses bounded shared execution defaults instead of forwarding only raw command + args
- both sandbox-backed and executor-backed `run_command` paths use the same resolved execution defaults
- `http_request` is governed by effective method / host / timeout / redirect policy
- restricted trust has materially stronger direct-network posture than today
- local-soft fallback is explicit and observable when hard network blocking is unavailable
- read-only mode clearly disables direct network and command execution
- `doctor` explains the effective sandbox / network posture
- audit and trace surfaces explain network-policy approvals, denials, and degraded execution outcomes
- tests cover policy resolution, enforcement, degradation, and reporting
