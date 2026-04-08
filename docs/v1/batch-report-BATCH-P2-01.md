# Batch Report: BATCH-P2-01

- Start: 2026-04-08T00:00:00Z
- End: 2026-04-08T00:00:00Z
- Executor: AI

## Tasks
- P2-OBS-001: done

## Changes
- `kernel/observe/normalized_metrics.go`: add unified metrics accumulator and normalized metrics map for `success/latency/cost/tool-error`.
- `kernel/observe/normalized_metrics_test.go`: add accumulator behavior tests.
- `contrib/telemetry/otel/observer.go`: wire normalized metrics accumulation via `OnEvent`, expose `NormalizedMetricsMap()`.
- `contrib/telemetry/otel/observer_test.go`: add normalized map test.
- `contrib/telemetry/prometheus/observer.go`: wire normalized metrics accumulation via `OnEvent`, expose `NormalizedMetricsMap()`.
- `contrib/telemetry/prometheus/observer_test.go`: add normalized map test.
- `docs/v1/status.md`: sync task and batch status.

## Validation
- `go test ./kernel/observe/...` : pass
- `go test ./...` (under `contrib/telemetry`) : pass

## Risks
- Normalized metrics map currently aggregates in-process observer memory; external persistence/export pipeline can be added later for long windows.

## Rollback Actions
- fallback to `kobs.NoOpObserver{}` or stop consuming `NormalizedMetricsMap()` output.
- keep existing per-backend legacy metrics as source of truth during rollback window.

## Next
- execute `P2-REL-001` (release gates and arch guard integration).

