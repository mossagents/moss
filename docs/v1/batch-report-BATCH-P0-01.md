# Batch Report: BATCH-P0-01

- Start: 2026-04-08T00:00:00Z
- End: 2026-04-08T00:00:00Z
- Executor: AI

## Tasks
- P0-EVAL-001: done
- P0-EVAL-003: done

## Changes
- `testing/eval/loader.go`: add `validateCase` with path-aware validation errors for `id`, `input.messages`, `expect`, `scoring.weights`.
- `testing/eval/eval_test.go`: add validation test and catalog coverage test (`>=20` cases + required tag coverage + suite list checks).
- `testing/eval/cases/smoke/`: add 8 smoke regression cases.
- `testing/eval/cases/full/`: add 11 full regression cases.
- `testing/eval/cases/smoke.txt` and `testing/eval/cases/full.txt`: add suite case ID lists.
- `docs/v1/status.md`: sync task and batch status.

## Validation
- `go test ./testing/eval/...` : pass

## Risks
- P0-EVAL-002 baseline file is still missing; gate should start in report-only mode to reduce false positive risk.

## Rollback Actions
- none

## Next
- run BATCH-P0-02 and execute `P0-EVAL-002` (baseline compare + regression gate).
