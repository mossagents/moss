---
goal: Implement the approved mosscode TUI redesign with transcript-first layout and unified overlays
version: 1
date_created: 2026-04-08
last_updated: 2026-04-08
owner: tui-runtime
status: Planned
tags: [feature, tui, ux, mosscode, go]
---

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

Implement the approved `mosscode` TUI redesign described in `docs/superpowers/specs/2026-04-08-mosscode-tui-redesign-design.md`. The implementation must replace the current default visual direction with a quieter transcript-first shell, runtime-first status/composer surfaces, unified event blocks for tool/progress/reasoning output, and a coherent overlay and keyboard interaction model across shared `contrib/tui` components.

## 1. Requirements & Constraints

- **REQ-001**: The default shared TUI theme must shift to the approved quieter shell aesthetic; do not add a parallel opt-in "new UI" mode.
- **REQ-002**: The main `chatModel` view must render as thin header + single-column transcript + bottom runtime bar/composer.
- **REQ-003**: Tool calls, progress, and reasoning output must render as one summary-first event family with collapsible detail.
- **REQ-004**: Inline approval/review prompts must escalate to overlays only by the approved trigger rules in the design spec.
- **REQ-005**: Overlay workflows must share one shell language and one keyboard contract for navigate / confirm / close.
- **REQ-006**: Full-help discoverability must move into a dedicated help surface; the default footer/status area may show only send, newline, cancel-when-active, and help/slash discovery hints.
- **REQ-007**: Existing operator capabilities must remain reachable after the redesign, including help, review, checkpoint/resume/fork, model/theme, MCP, schedule, and mention flows.
- **REQ-008**: Launch-time `mosscode` wiring in `examples/mosscode/commands_exec.go` may only pass shared TUI defaults/config; do not fork product-only rendering behavior.
- **SEC-001**: Do not weaken approval UX; approval-required, blocked, error, cancel, and terminal states must remain explicit even when other details are collapsed.
- **CON-001**: Reuse existing Bubble Tea / Lip Gloss composition in `contrib/tui`; do not introduce a second rendering stack.
- **CON-002**: Keep all changes within shared TUI files and the approved `examples/mosscode/commands_exec.go` integration point unless implementation proves a supporting picker/update file is required for the new shared overlay contract.
- **CON-003**: Maintain narrow-terminal usability; header metadata and secondary hints must collapse before transcript readability does.
- **GUD-001**: Follow current file boundaries: theme/tokens, shell/layout, transcript rendering, status/composer, overlay interaction, and state orchestration.
- **PAT-001**: Prefer extending existing renderer and key-handler seams (`View`, `generateLayout`, `renderStatusLine`, `renderMessage`, `renderProgressBlock`, overlay `HandleKey` methods, `chatModel.Update`) instead of creating parallel render paths.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Rebuild the default shell layout, theme tokens, and bottom control surfaces to match the approved transcript-first structure.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Update shared visual tokens in `contrib/tui/theme.go` (`applyDefaultTheme`, `applyDarkTheme`, `setCommonThemeStyles`) and any related style constants in `contrib/tui/styles.go` so the default UI uses darker low-noise chrome, muted borders, restrained accents, and lighter framing. | | |
| TASK-002 | Refactor `contrib/tui/layout.go` (`generateLayout`, `editorPaneHeight`) and `contrib/tui/chat_components.go` (`renderMainPane`, `renderEditorPane`, `renderStatusPane`, `renderBody`) so the default chat screen is a single-column transcript with a bottom runtime bar + composer and no persistent side pane. | | |
| TASK-003 | Refactor `contrib/tui/chat_view.go` (`renderHeaderMetaLine`, `renderSlashHintLine`, `renderFooterHelpLine`, `View`) and `contrib/tui/shell.go` helpers so the header becomes a thin session/product strip and the default footer stops rendering dense always-on shortcut copy. | | |
| TASK-004 | Refactor `contrib/tui/statusline.go` (`renderStatusLine`) and `contrib/tui/chat.go` (`renderStatusSummary`, `compactPostureSummary`, `inputBoxHeight`, `adjustInputHeight`, `visibleInputHeight`, `visibleProgressHeight`) so the status bar becomes runtime-first and the composer becomes prompt-first with explicit idle / slash-active / busy / approval-pending states. | | |
| TASK-005 | Update `examples/mosscode/commands_exec.go` `launchTUI` wiring only if the shared TUI requires new default labels/config fields to keep `mosscode` startup aligned with the redesigned shell. | | |

### Implementation Phase 2

- **GOAL-002**: Replace the current mixed message/progress presentation with unified summary-first event blocks and deterministic folding/coalescing behavior.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-006 | Refactor `contrib/tui/message.go` renderers (`renderMessage`, `renderProgressMessage`, `renderReasoningMessage`, `renderToolCallMessages`, `renderToolCall`, `renderToolHeaderLine`, `renderToolBodyBlock`, `renderToolBody`) so tool, progress, and reasoning rows share one compact event-block hierarchy with summary-first layout and subdued chrome. | | |
| TASK-007 | Extend `contrib/tui/chat.go` state/orchestration (`newChatModel`, `handleBridge`, `refreshViewport`, `markToolStartCompleted`) with any explicit event-block collapse/coalescing state required by the design, keeping failure and approval-blocked items visible by default. | | |
| TASK-008 | Refactor `contrib/tui/progress.go` (`recordProgressSnapshot`, `applyProgressSnapshot`, `renderProgressBlock`, `foldExecutionProgressEvent`, `renderTimelineEntry`, `rebuildExecutionProgress`) so repeated progress updates coalesce only for the same in-flight activity key, while tool boundaries, approvals, failures, cancellations, and terminal states remain distinct. | | |
| TASK-009 | Ensure fold-state behavior remains view-local and session-local during a live TUI session only; do not persist event expansion state across full restart or later resume. Implement this in the `chatModel` state and the message/progress render paths instead of a separate persistence layer. | | |

### Implementation Phase 3

- **GOAL-003**: Unify overlay chrome, overlay lifecycle, and keyboard interaction across operator workflows without losing reachability.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-010 | Refactor `contrib/tui/overlay.go` (`overlayStack`, overlay `View` methods, overlay `HandleKey` methods, `open*Overlay`, `close*Overlay`, `activeOverlay`) so overlays share one narrower low-noise frame, one-primary-overlay-at-a-time lifecycle, and one consistent close/return-focus contract. | | |
| TASK-011 | Refactor shared overlay body widgets in `contrib/tui/selection_list.go` (`renderSelectionListDialog`) and `contrib/tui/ask_form.go` (`renderAskForm`, `renderApprovalAskForm`, `handleAskKey`, `handleApprovalAskKey`, `submitAskForm`, `submitApprovalAskForm`) so pickers/forms/approvals use the same visual shell and keyboard rhythm. | | |
| TASK-012 | Refactor slash/help discovery surfaces in `contrib/tui/slash_popup.go` (`buildSlashPopup`, `renderSlashPopup`) plus `contrib/tui/chat.go` (`refreshSlashHints`, `currentSlashHints`, `applySlashCompletion`) so slash popup terminology and help/discovery grouping match the new default footer/help model. | | |
| TASK-013 | Update keyboard routing in `contrib/tui/chat_update.go` (`Update`) and `contrib/tui/chat.go` (`handleEsc`, `handleHistoryNavigation`, `recordInputHistory`) so chat flow, timeline flow, and overlay flow are distinct mental zones with predictable send / newline / cancel / history / overlay navigation behavior. | | |
| TASK-014 | Normalize overlay-specific handlers in supporting shared files only where required by the new overlay contract: `contrib/tui/ops_pickers.go`, `contrib/tui/model_picker.go`, `contrib/tui/theme_picker.go`, `contrib/tui/schedule_browser.go`, `contrib/tui/thread_pickers.go`, `contrib/tui/mention_picker.go`, and `contrib/tui/transcript_overlay.go`. Update only the handlers/renderers needed to inherit the shared shell and keyboard rules. | | |

### Implementation Phase 4

- **GOAL-004**: Add regression coverage and execute the required repository validations for both the shared TUI module and the nested `examples/mosscode` module.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-015 | Extend `contrib/tui/chat_test.go`, `contrib/tui/message_test.go`, `contrib/tui/progress_test.go`, and `contrib/tui/app_test.go` with focused assertions for transcript-first layout, status/composer hints, unified event-block rendering, progress coalescing, overlay lifecycle, and keyboard routing regressions. | | |
| TASK-016 | Run `gofmt -w contrib/tui/*.go examples/mosscode/commands_exec.go` after code changes and keep imports/order deterministic. | | |
| TASK-017 | From `D:\Codes\qiulin\moss`, run `go test ./...` and `go build ./...`; fix only regressions introduced by the TUI redesign in the root module. | | |
| TASK-018 | From `D:\Codes\qiulin\moss\examples\mosscode`, run `go test .` and `go build .`; fix only regressions introduced by the redesign in the nested `mosscode` module. | | |

## 3. Alternatives

- **ALT-001**: Add the redesign as an optional theme toggle. Rejected because the approved design explicitly replaces the default direction instead of introducing a second long-lived UX.
- **ALT-002**: Restyle only colors and borders without changing layout or interaction. Rejected because the user approved high-impact interaction changes, and the core problem includes layout, discoverability, and overlay coherence.
- **ALT-003**: Implement product-specific `mosscode` shell behavior under `examples/mosscode` instead of updating the shared TUI. Rejected because the approved design is for the shared TUI consumed by `mosscode`, not a one-off fork.

## 4. Dependencies

- **DEP-001**: Existing Bubble Tea view/update flow in `contrib/tui/chat_view.go`, `contrib/tui/chat_update.go`, and `contrib/tui/app_update_routing.go`.
- **DEP-002**: Existing Lip Gloss theme/style substrate in `contrib/tui/styles.go`, `contrib/tui/theme.go`, and `contrib/tui/shell.go`.
- **DEP-003**: Existing execution event and replay substrate in `contrib/tui/progress.go` and the runtime state catalog it already consumes.
- **DEP-004**: Existing overlay/picker handler files (`overlay.go`, `ops_pickers.go`, `model_picker.go`, `theme_picker.go`, `thread_pickers.go`, `schedule_browser.go`, `mention_picker.go`, `transcript_overlay.go`).
- **DEP-005**: Approved design source of truth: `docs/superpowers/specs/2026-04-08-mosscode-tui-redesign-design.md`.

## 5. Files

- **FILE-001**: `docs/superpowers/specs/2026-04-08-mosscode-tui-redesign-design.md` — approved design source of truth.
- **FILE-002**: `plan/feature-mosscode-tui-redesign-1.md` — implementation plan source of truth.
- **FILE-003**: `contrib/tui/styles.go` — shared style tokens and reusable style primitives.
- **FILE-004**: `contrib/tui/theme.go` — default/dark/plain theme application logic.
- **FILE-005**: `contrib/tui/layout.go` — layout generation and editor sizing helpers.
- **FILE-006**: `contrib/tui/chat_view.go` — root shell view, header/footer composition.
- **FILE-007**: `contrib/tui/chat_components.go` — main/body/editor/status pane assembly.
- **FILE-008**: `contrib/tui/shell.go` — shared shell frame helpers.
- **FILE-009**: `contrib/tui/statusline.go` — runtime status-line rendering.
- **FILE-010**: `contrib/tui/chat.go` — chat state orchestration, composer behavior, status summary, slash/history helpers.
- **FILE-011**: `contrib/tui/chat_update.go` — top-level keyboard routing for chat mode.
- **FILE-012**: `contrib/tui/message.go` — transcript rendering and tool/event blocks.
- **FILE-013**: `contrib/tui/progress.go` — progress state folding, replay, and progress block rendering.
- **FILE-014**: `contrib/tui/overlay.go` — overlay stack, overlay lifecycle, shared overlay dispatch.
- **FILE-015**: `contrib/tui/selection_list.go` — shared list dialog renderer.
- **FILE-016**: `contrib/tui/ask_form.go` — ask/approval form UX.
- **FILE-017**: `contrib/tui/slash_popup.go` — slash popup state and rendering.
- **FILE-018**: `contrib/tui/ops_pickers.go` — help/status/MCP/review picker behavior if required by shared overlay changes.
- **FILE-019**: `contrib/tui/model_picker.go` — model picker behavior if required by shared overlay changes.
- **FILE-020**: `contrib/tui/theme_picker.go` — theme picker behavior if required by shared overlay changes.
- **FILE-021**: `contrib/tui/schedule_browser.go` — schedule overlay behavior if required by shared overlay changes.
- **FILE-022**: `contrib/tui/thread_pickers.go` — resume/fork/agent picker behavior if required by shared overlay changes.
- **FILE-023**: `contrib/tui/mention_picker.go` — mention overlay behavior if required by shared overlay changes.
- **FILE-024**: `contrib/tui/transcript_overlay.go` — transcript overlay behavior if required by shared overlay changes.
- **FILE-025**: `contrib/tui/chat_test.go` — chat interaction and routing regression coverage.
- **FILE-026**: `contrib/tui/message_test.go` — transcript rendering regression coverage.
- **FILE-027**: `contrib/tui/progress_test.go` — progress/coalescing regression coverage.
- **FILE-028**: `contrib/tui/app_test.go` — top-level TUI render/startup coverage.
- **FILE-029**: `examples/mosscode/commands_exec.go` — `mosscode` TUI launch-time integration only.

## 6. Testing

- **TEST-001**: Verify the default `chatModel.View()` renders a thin header, single-column transcript body, and bottom runtime/composer stack.
- **TEST-002**: Verify `renderStatusLine` shows runtime-first state and only the approved high-value hints in idle vs active states.
- **TEST-003**: Verify `renderMessage` / `renderToolCall` / `renderProgressBlock` render tool, progress, and reasoning items as one summary-first family.
- **TEST-004**: Verify repeated progress updates coalesce only for the same in-flight activity key and never hide approvals, failures, cancellations, or terminal tool/result boundaries.
- **TEST-005**: Verify inline-to-overlay escalation rules for approvals/review prompts follow the approved triggers.
- **TEST-006**: Verify overlay close/confirm/navigation behavior is consistent across ask forms, selection lists, help, review, resume/fork, and model/theme flows.
- **TEST-007**: Verify history recall, slash completion, overlay navigation, and escape/cancel routing continue to work with the new keyboard zones.
- **TEST-008**: Run `go test ./...` and `go build ./...` from the repository root.
- **TEST-009**: Run `go test .` and `go build .` from `examples/mosscode`.

## 7. Risks & Assumptions

- **RISK-001**: Shared-file overlap in `chat.go`, `chat_view.go`, and `chat_components.go` can cause layout and interaction regressions if rendering and state changes are not kept aligned.
- **RISK-002**: Overlay unification can silently break niche operator flows if supporting picker handlers are not updated to the new contract.
- **RISK-003**: Progress coalescing can hide meaningful state if activity identity is implemented too loosely.
- **RISK-004**: Narrow-terminal rendering may regress if the redesign keeps too much footer/header metadata visible.
- **ASSUMPTION-001**: The approved design spec is the behavioral source of truth for escalation rules, fold-state policy, and discoverability decisions.
- **ASSUMPTION-002**: `examples/mosscode` remains a thin shared-TUI consumer; product integration should stay limited to `launchTUI` configuration unless the shared contract requires a new exposed option.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-08-mosscode-tui-redesign-design.md`
- `contrib/tui/chat.go`
- `contrib/tui/chat_update.go`
- `contrib/tui/message.go`
- `contrib/tui/progress.go`
- `contrib/tui/overlay.go`
- `examples/mosscode/commands_exec.go`
