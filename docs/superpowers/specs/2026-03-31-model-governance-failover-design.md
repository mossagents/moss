# Model Governance / Failover v1 Design

## Problem

`moss` and `mosscode` already have the first layer of model governance needed for production use:

- model profile routing through `models.yaml`
- retry configuration
- circuit-breaker configuration
- pricing catalog discovery
- doctor/governance reporting
- trace and audit observer surfaces

What is still missing is runtime recovery when the selected model or provider is temporarily unavailable.

Today the router chooses a single model, and the loop retries that single choice. If the provider is degraded, the request still fails even when another configured model could satisfy the same requirement. That is not enough for a production-ready `mosscode` product surface that aims to match the core operational reliability expectations of Codex CLI-class tooling.

The goal of this milestone is to add a first production-safe failover path without rewriting the loop or introducing a second routing system.

## Goals

- Add ordered candidate failover for LLM calls
- Keep model selection and runtime recovery as separate responsibilities
- Reuse existing `models.yaml` model profiles instead of introducing a parallel routing config
- Scope retry and breaker behavior to each candidate model when failover is enabled
- Preserve existing single-model behavior when failover is disabled
- Make failover decisions visible through doctor and run trace surfaces
- Keep the first delivery small enough to validate quickly and harden with tests

## Non-goals

- No quality-based failover in v1
- No semantic "this answer looks weak, try another model" loop
- No automatic failover after user-visible output or tool side effects have begun
- No persistent cross-process breaker history in v1
- No new standalone failover YAML format
- No rewrite of `kernel/loop` into a multi-model orchestration engine

## Recommended Approach

The recommended approach is a separate `FailoverLLM` wrapper that sits above the router-selected candidate list and below the loop.

Responsibilities are split as follows:

1. `ModelRouter` remains responsible for matching task requirements to an ordered candidate chain
2. `FailoverLLM` is responsible for trying that ordered chain at runtime
3. `kernel/loop` continues to depend on a single `port.LLM`
4. product governance continues to own config discovery, doctor output, and assembly decisions

This gives us a clean reliability layer without turning routing into recovery policy or pushing multi-candidate logic into the loop.

This approach does require two small but explicit loop-facing integrations for v1:

- a way for the selected final model and failover-attempt summary to flow back through the response or error path so session-aware observer events can be emitted correctly
- a way for streaming failures to distinguish safe pre-emission startup failure from unsafe post-emission failure

## Alternatives Considered

### A. Router-internal failover

Teach `ModelRouter` to perform retries, breaker checks, and candidate switching internally.

Pros:

- fewer new types
- simplest call-site wiring

Cons:

- mixes selection policy with recovery policy
- makes router stateful in a way it is not today
- harder to test ordering logic independently from runtime error handling
- makes governance reporting less clear

### B. Failover wrapper (recommended)

Keep routing and failover separate. Router returns ordered candidates; wrapper executes them.

Pros:

- clear responsibility split
- easier unit testing
- safer incremental integration
- preserves existing router behavior when failover is off
- gives a clean place to attach observability

Cons:

- requires a new wrapper type and assembly branch
- requires loop-level retry/breaker wiring changes when wrapper is active

### C. Loop-level failover

Teach `kernel/loop` to understand model candidates and switch between them directly.

Pros:

- maximum control at the orchestration layer
- could support richer policies later

Cons:

- most invasive option
- leaks governance concerns into the loop
- increases risk to unrelated execution behavior
- too large for the first production-ready milestone

## Architecture Boundaries

The v1 architecture introduces one new runtime concept: `FailoverLLM`.

`FailoverLLM` should live close to the adapter layer because it is still an LLM transport concern, not a product workflow concern. Product governance decides whether to construct it and with what policy, but the loop still sees a single `port.LLM`.

Layer responsibilities:

- `adapters/router.go`
  - load model profiles
  - rank and return ordered candidates for a request
  - remain ignorant of runtime retries, breaker state, and fallback switching
- `adapters/failover.go` or equivalent
  - implement `model.LLM` (unified `GenerateContent` interface)
  - iterate through candidates
  - own per-candidate retry and breaker handling when failover is enabled
  - emit failover execution events
- `appkit/product/governance.go`
  - expose failover config defaults
  - discover/report active router + failover settings
  - support doctor/governance rendering
- `apps/mosscode/main.go`
  - assemble router and optional failover wrapper
  - disable loop-level LLM retry/breaker when the wrapper is active
- `kernel/loop`
  - remain unchanged in its public shape
  - accept small internal changes for selected-model reporting and streaming safety
  - continue to call a single `port.LLM`

This keeps the loop stable and makes failover an opt-in assembly choice rather than a new execution substrate.

## Configuration Model

v1 should reuse the existing `models.yaml` file and extend governance config rather than introducing a separate failover document.

Recommended governance fields:

- `LLMFailoverEnabled`
- `LLMFailoverMaxCandidates`
- `LLMFailoverPerCandidateRetries`
- `LLMFailoverOnBreakerOpen`

Recommended defaults:

- failover disabled by default
- `LLMFailoverMaxCandidates = 2`
- `LLMFailoverPerCandidateRetries = 1`
- `LLMFailoverOnBreakerOpen = true`

Recommended CLI flags:

- `--llm-failover`
- `--llm-failover-max-candidates`
- `--llm-failover-retries`
- `--llm-failover-on-breaker-open`

Environment variables should follow the existing governance naming pattern for `MOSSCODE_*` and `MOSS_*` compatibility.

Important integration rule:

- when failover is disabled, keep the current loop-level retry and breaker wiring
- when failover is enabled and a router is available, move retry and breaker ownership into `FailoverLLM`
- when failover is enabled and a router is available, `deepagent.BuildKernel` must be called with loop-level LLM retry disabled and loop-level LLM breaker omitted
- when failover is enabled but no router is configured, do not silently invent a candidate chain; keep current single-model behavior

This avoids double retries and ensures breaker state is tracked per candidate rather than per overall request.

## Candidate Selection

`ModelRouter` should be upgraded from "select one" to "rank many."

The current ordering semantics can stay largely intact:

- filter by required capabilities
- apply `MaxCostTier` if present
- sort by cheapest first when `PreferCheap` is true
- otherwise sort by strongest profile first

The main change is that the router should expose the ordered candidate chain instead of discarding every model except the first.

When failover is enabled, `FailoverLLM` should request the ordered chain and truncate it by `LLMFailoverMaxCandidates`.

When failover is disabled, the first candidate remains the effective model exactly as it does today.

For the common nil/empty-requirements case, v1 should define deterministic fallback ordering explicitly:

- candidate 0 is the configured default model
- remaining configured models follow in file order
- the default model is not duplicated if it also appears in the remainder set

This preserves current primary-model behavior while making failover useful for ordinary runs.

## Runtime Failover Semantics

The failover loop for synchronous calls should be:

1. ask router for ordered candidates
2. for each candidate:
   - consult that candidate's breaker when breaker handling is enabled
   - skip to the next candidate immediately if the breaker is open and `LLMFailoverOnBreakerOpen` is true
   - attempt the call with bounded retries for that candidate
   - on success, return immediately
   - on non-recoverable failure, continue only if the failure class is allowed for failover
3. if all candidates fail, return an aggregated error that names each candidate and failure reason

v1 failover triggers should be defined in terms of implementable existing contracts, not provider-specific error guesswork:

- breaker-open rejection for the current candidate when `LLMFailoverOnBreakerOpen` is true
- errors that the effective retry classifier would treat as retryable for that candidate

v1 should not fail over for:

- context cancellation
- subjective answer quality
- policy/content refusals unless they are already classified as retryable by the active retry policy
- downstream tool execution failures after the LLM step succeeded
- cases where visible output or executable tool calls have already been emitted

## Streaming Semantics

Streaming failover needs stricter boundaries than synchronous failover.

v1 rules:

- if stream startup fails before any visible output or tool-use payload is emitted, failover may try the next candidate
- if the stream errors after visible content has been emitted, do not silently switch candidates
- if the stream errors after tool-use payload has started, do not silently switch candidates
- if the first candidate does not support streaming, that is treated as candidate failure and the wrapper may continue to the next candidate before anything has been emitted

This avoids mixing outputs from multiple models into one user-visible response and avoids duplicate side effects from tool-calling streams.

To make this implementable against the current loop, v1 must also change the internal streaming error contract:

- `streamLLM` and failover streaming paths must distinguish safe pre-emission startup failure from unsafe post-emission failure
- `callLLMOnce` may only perform sync fallback after a streaming error when the error is explicitly marked safe for fallback
- post-emission streaming failures must return directly without sync fallback

## Retry and Breaker Ownership

The existing loop-level retry and breaker implementation assumes a single LLM instance. That assumption breaks down once one request can move across multiple candidates.

For that reason, v1 should make ownership explicit:

- no change to current behavior when failover is off
- when failover is on, loop-level LLM retry and breaker logic should be disabled for the wrapped path
- `FailoverLLM` should maintain retry and breaker state per candidate model

Per-candidate breaker state may be held in-memory inside the wrapper or a companion structure. It does not need to be durable across process restarts in v1.

This is the simplest way to keep breaker decisions scoped to an individual provider/model rather than accidentally opening the circuit for the entire candidate chain.

Candidate breaker identity should be keyed by stable router profile identity, preferably `ModelProfile.Name`.

## Observability and Doctor Surfaces

v1 should reuse existing observer interfaces instead of introducing a new observer contract, but it cannot do that purely inside the wrapper with today's response shape.

The spec therefore requires a small response/error propagation mechanism so loop-owned observer emission can stay session-aware:

- successful responses must carry the actual selected model
- successful responses should also be able to carry failover-attempt summary data for execution-event emission
- failover exhaustion errors should carry ordered attempt detail

`kernel/loop` should then:

- stop assuming `sess.Config.ModelConfig.Model` is the actual served model when routing/failover is active
- use propagated actual-model metadata for `LLMCallEvent` and `ExecutionLLMCompleted`
- allow `ExecutionLLMStarted` to omit `Model` or mark it as requested/unknown when actual candidate selection is deferred to runtime

`port.LLMCallEvent` should continue to describe the final executed successful or failed call. Intermediate failover activity should be surfaced through `OnExecutionEvent` and trace timeline entries emitted by the loop after it receives the attempt metadata.

Recommended execution event data:

- `candidate_model`
- `attempt_index`
- `candidate_retry`
- `failure_reason`
- `breaker_state`
- `failover_to`
- `final_model`
- `outcome`

Recommended event types:

- `llm_failover_attempt`
- `llm_failover_switch`
- `llm_failover_exhausted`

`product.BuildGovernanceReport` should be extended to show:

- whether failover is enabled
- configured max candidate count
- per-candidate retry count
- breaker-open failover policy
- whether router config is present and therefore failover is practically available

Doctor output should explain clearly when failover is configured but ineffective because no router is loaded.

## Error Handling

Failover must not hide the shape of the underlying failure.

Recommended behavior:

- if one candidate succeeds, return the normal response and record the final chosen model
- if all candidates fail, return an aggregated error that preserves ordered attempt detail
- if a stream becomes non-failover-safe after visible output begins, return the current candidate error directly
- do not silently fall back to a best-effort success-shaped response

The aggregated error should be easy to render in text and JSON later, but v1 does not need a rich public error API beyond structured enough detail for tests and doctor/debug output.

## Testing and Acceptance Criteria

### Router tests

Extend router tests to validate:

- ordered candidate selection
- capability filtering
- `PreferCheap`
- `MaxCostTier`
- max-candidate truncation behavior where applicable

### Failover wrapper tests

Add focused tests for:

- first candidate success
- first candidate failure then second candidate success
- per-candidate retry before switching
- breaker-open skip to next candidate
- all candidates exhausted returns aggregated error
- streaming startup failure allows switching
- streaming failure after visible output does not switch
- streaming failure after tool-use emission does not switch

### Product governance tests

Extend governance tests to validate:

- failover config defaults and flag/env merging
- governance/doctor report includes new failover fields
- report explains when failover is enabled but router config is absent

### Product wiring tests

Extend `apps/mosscode` tests to validate:

- build path keeps existing single-model behavior when failover is disabled
- failover-enabled build path wraps the router and disables loop-level retry/breaker ownership
- failover-enabled build path does not leave loop sync fallback active for unsafe post-emission streaming errors
- doctor JSON/text includes the new failover settings

### Acceptance criteria

The milestone is complete when all of the following are true:

- a router-backed run can recover from primary model failure by switching to a compatible secondary candidate
- breaker-open on the first candidate does not terminate the whole request if another candidate exists
- partial streamed output never silently switches to a second candidate
- successful routed/failover calls report the actual serving model rather than only the requested model config
- doctor and trace surfaces explain why failover happened and which model finally served the request
- failover disabled preserves current behavior
- repo validation still passes with `go test ./...` and `go build ./...`

## Rollout Notes

This milestone should be implemented as a focused production-hardening batch:

1. extend router candidate selection
2. add selected-model / attempt-metadata propagation through the LLM response and error path
3. tighten loop streaming fallback semantics so only safe pre-emission failures can fall back
4. add the failover wrapper and tests
5. extend governance config/reporting
6. wire `mosscode` assembly and doctor output
7. run targeted and full validation

No TUI-specific interaction work is required beyond surfacing the resulting doctor/governance information through existing product outputs.
