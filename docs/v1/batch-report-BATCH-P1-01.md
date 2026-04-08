# Batch Report: BATCH-P1-01

- Start: 2026-04-08T00:00:00Z
- End: 2026-04-08T00:00:00Z
- Executor: AI

## Tasks
- P1-PROMPT-001: done
- P1-BUDGET-001: done

## Changes
- `appkit/flags.go`: add budget governance flags (`--budget-governance`, `--global-max-tokens`, `--global-max-steps`, `--global-budget-warn-at`) with env support.
- `appkit/runtime_builder.go`: extend `RuntimeFeatureFlags` with budget governance and global budget limits.
- `appkit/runtime_builder_test.go`: add tests for budget governance defaults and override propagation.
- `appkit/appkit_test.go`: add defaults/env tests for budget governance options.
- `kernel/session/session.go`: add `SessionConfig.BudgetPolicy`.
- `kernel/session/budget_governance.go`: add policy normalization, block decision, and aggregated budget report model.
- `kernel/session/session_test.go`: add budget governance normalization and aggregate report behavior tests.
- `kernel/session/store_file_test.go`: add `budget_policy` persistence round-trip test.
- `docs/v1/status.md`: sync board status.

## Validation
- `go test ./appkit/... ./kernel/session/... ./kernel/budget/...` : pass
- `go test ./testing/eval/...` : pass

## Risks
- Governance policy/report is now available and persisted, but enforcement remains opt-in (`enforce`) and still requires entrypoint wiring to actively block in production flows.

## Rollback Actions
- Set `--budget-governance=observe-only` (or `off`) to disable blocking immediately.
- Keep global limits configured while collecting report evidence only.

## Next
- execute `P1-EVAL-001` (grouped eval reporting by `prompt_version` and `budget_policy`).
