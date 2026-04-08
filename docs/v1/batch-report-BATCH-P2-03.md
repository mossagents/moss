# Batch Report: BATCH-P2-03

- Start: 2026-04-08T00:00:00Z
- End: 2026-04-08T00:00:00Z
- Executor: AI

## Tasks
- P2-SERVE-001: done

## Changes

### kernel/run_supervisor.go
- Add `activeCount() int` helper method returning the length of the active `runs` map (thread-safe, under mutex).

### kernel/kernel.go
- Add `ActiveRunCount() int` — public API exposing current in-flight run count.
- Add `IsShuttingDown() bool` — public API reporting kernel shutdown state.

### appkit/serve.go
- Add `HealthStatus` typed constant: `"ok"` / `"shutting_down"`.
- Add `HealthOutput` struct — unified health snapshot:
  - `status`, `active_runs`, `llm_latency_avg_ms`, `tool_latency_avg_ms`, `success_rate`, `tool_error_rate`, `total_runs`
- Add `Health(k, ...NormalizedMetricsSnapshot) HealthOutput` — point-in-time snapshot function. Accepts optional P2-OBS-001 metrics.
- Add `HealthJSON(k, ...) string` — JSON serialization of the snapshot.
- Add `HealthText(k, ...) string` — single-line human-readable summary.

### appkit/serve_test.go (new)
- 11 unit tests covering all three functions and the two new kernel methods.

## Validation

- `go test ./appkit/...` : **PASS** (appkit, appkit/product, appkit/runtime, appkit/runtime/events)
- `go test ./kernel/...` : **PASS** (all 16 kernel packages)

## Acceptance Criteria

✅ `status` — "ok" / "shutting_down" depending on kernel state
✅ `active_runs` — live count from runSupervisor
✅ `latency` — llm_latency_avg_ms + tool_latency_avg_ms from P2-OBS-001 NormalizedMetricsSnapshot
✅ rollback: remove Health* calls; revert ActiveRunCount/IsShuttingDown (pure additions, zero breaking changes)

## Risks

- `active_runs` reflects in-process count only. Distributed deployments need aggregation at a higher layer.
- Latency fields are populated only when the caller supplies a `NormalizedMetricsSnapshot`; absent metrics default to zero (not an error).

## Rollback Actions

- Remove `Health`, `HealthJSON`, `HealthText` from `appkit/serve.go` — no other code depends on them.
- Remove `ActiveRunCount` / `IsShuttingDown` from `kernel/kernel.go` — purely additive.
- Remove `activeCount` from `run_supervisor.go`.

## Next

All 11 tasks in the v1 roadmap (P0 → P1 → P2) are **DONE**. 🎉

