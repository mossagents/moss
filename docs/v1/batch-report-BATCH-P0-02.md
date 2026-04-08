# Batch Report: BATCH-P0-02

- Start: 2026-04-08T00:00:00Z
- End: 2026-04-08T00:00:00Z
- Executor: AI

## Tasks
- P0-EVAL-002: done
- P0-DOC-001: ready

## Changes
- `testing/eval/types.go`: add baseline snapshot and gate decision types.
- `testing/eval/runner.go`: add baseline load/save and regression gate compare logic with score-drop threshold and report-only mode.
- `testing/eval/eval_test.go`: add baseline round-trip and gate behavior tests.
- `docs/v1/status.md`: sync task and batch status.

## Validation
- `go test ./testing/eval/...` : pass

## Risks
- Baseline file is not initialized yet in repository, so gate currently falls back to report-only when baseline is missing.

## Rollback Actions
- Set `GateReportOnly=true` and keep score regression as non-blocking report.

## Next
- execute `P0-DOC-001` (regression rollback handbook + drill record).

