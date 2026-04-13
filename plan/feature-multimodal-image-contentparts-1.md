---
goal: Implement multimodal image recognition and generation using content-parts protocol across moss runtime
version: 1.0
date_created: 2026-04-03
last_updated: 2026-04-03
owner: Copilot
status: 'In progress'
tags: [feature, multimodal, image, protocol, adapters, tui, migration]
---

> **Archived historical implementation plan.** This file is retained for reference only and is not the canonical source for the current architecture or execution state.

# Introduction

![Status: In progress](https://img.shields.io/badge/status-In%20progress-yellow)

This implementation plan executes the approved design in `docs/superpowers/specs/2026-04-03-multimodal-image-recognition-generation-design.md` by replacing string-only message payloads with structured `content_parts`, enabling first-class image input/output in kernel, OpenAI/Claude adapters, and TUI, while preserving deterministic migration behavior for stored sessions.

## 1. Requirements & Constraints

- **REQ-001**: Replace `kernel/port.Message.Content` with `kernel/port.Message.ContentParts` and make `content_parts` the canonical runtime payload.
- **REQ-002**: Replace `kernel/port.ToolResult.Content` with `kernel/port.ToolResult.ContentParts` and make tool-result multimodal payload first-class.
- **REQ-003**: Implement `kernel/port.ValidateContentParts(parts []ContentPart) error` and enforce it at required call sites.
- **REQ-004**: Support image understanding input (`input_image`) and image generation output (`output_image`) for both `adapters/openai` and `adapters/claude`.
- **REQ-005**: Upgrade `userio/tui` attachment flow from image-path text note to structured image content part submission and structured image rendering.
- **REQ-006**: Implement read-old/write-new session migration in `kernel/session/store_file.go` for legacy `messages[].content` and `tool_results[].content`.
- **REQ-007**: Keep audio/video part types reserved in schema and reject them at runtime in this release.
- **SEC-001**: Reject invalid or ambiguous part payloads (both url+inline, neither source, non-image MIME for image parts) with explicit errors.
- **SEC-002**: Do not silently drop unsupported content part types in adapters or runtime.
- **SEC-003**: Ensure provider request mappers never send `source_path` to remote model APIs.
- **CON-001**: This phase is a protocol cutover; runtime code must compile against `content_parts` and no longer depend on `Message.Content`.
- **CON-002**: Existing loop/tool/session behavior (tool call execution order, observer emission, stream contract) must remain behaviorally stable except for payload shape.
- **GUD-001**: Follow existing adapter mapping structure in `adapters/openai/openai.go` and `adapters/claude/claude.go` by adding typed mapper helpers instead of inlining conditionals in large switch blocks.
- **PAT-001**: Keep kernel canonical and adapter-specific mapping isolated; no provider-specific payload semantics in `kernel/port`.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Define canonical multimodal content-part protocol in kernel/port and convert core runtime types to it.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Modify `kernel/port/types.go` to add `ContentPartType`, `ContentPart`, and constants: `text`, `input_image`, `output_image`, `input_audio`, `output_audio`, `input_video`, `output_video`; replace `Message.Content` with `Message.ContentParts`; replace `ToolResult.Content` with `ToolResult.ContentParts`. | ✅ | 2026-04-03 |
| TASK-002 | Add validation API in `kernel/port` (new file `kernel/port/content_parts.go` and tests `kernel/port/content_parts_test.go`) implementing exact validation matrix from spec and explicit errors for unsupported reserved types. | ✅ | 2026-04-03 |
| TASK-003 | Update `kernel/port/llm.go` comments and any request/response structs referencing message content semantics to use `ContentParts` terminology and ensure stream types remain unchanged. |  |  |
| TASK-004 | Update compile references in core runtime (`kernel/loop/loop.go`, `kernel/session/session.go`, `kernel/session/context.go`, `kernel/session/hooks.go`, related tests) to stop reading/writing `Message.Content` and instead use helper extraction for text-only fallback where required. | ✅ | 2026-04-03 |
| TASK-005 | Add utility helpers in `kernel/port` (for example `TextPart`, `ImageInlinePart`, `ImageURLPart`, `ContentPartsToPlainText`) and migrate direct string construction sites in core runtime to helpers for deterministic behavior. | ✅ | 2026-04-03 |

### Implementation Phase 2

- **GOAL-002**: Implement OpenAI and Claude multimodal request/response mapping with content-parts contract.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-006 | Refactor `adapters/openai/openai.go`: replace `toOpenAIMessages` string-only mapping with `ContentParts -> ChatCompletion content parts` mapping for user/assistant/tool paths; add `contentPartToOpenAI*` helper functions; preserve tool-call mapping behavior. | ✅ | 2026-04-03 |
| TASK-007 | Refactor `adapters/openai/openai.go`: map response message payloads (text and image outputs where exposed) back to `[]port.ContentPart` in `fromOpenAIResponse` and stream finalization path where applicable. | ✅ | 2026-04-03 |
| TASK-008 | Refactor `adapters/claude/claude.go`: replace text-only block construction with `ContentParts -> anthropic content blocks` mapping; include tool result content-part mapping and preserve tool-use semantics. | ✅ | 2026-04-03 |
| TASK-009 | Refactor `adapters/claude/claude.go`: map response blocks (`TextBlock`, image blocks when present) back to `[]port.ContentPart` in `fromAnthropicResponse` and maintain existing streaming tool-call aggregation behavior. | ✅ | 2026-04-03 |
| TASK-010 | Add or update adapter tests in `adapters/openai/openai_test.go` and `adapters/claude/claude_test.go` to cover: text+input_image request mapping, output_image response mapping, tool-result content parts, and unsupported content-part errors. | ✅ | 2026-04-03 |

### Implementation Phase 3

- **GOAL-003**: Implement TUI multimodal attachment/rendering flow and runtime validation call-site enforcement.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-011 | Replace image mention fallback in `userio/tui/external_context.go`: keep text-file expansion behavior, but produce structured image attachment metadata for submission path instead of appending “Image reference attached by path” text blocks. | ✅ | 2026-04-03 |
| TASK-012 | Modify TUI send path (`userio/tui/chat_update.go`, `userio/tui/chat.go`, and associated message bridge files) to construct `port.Message{Role:user, ContentParts:[...]}` with text and validated `input_image` inline parts (mime + base64 + source_path metadata). | ✅ | 2026-04-03 |
| TASK-013 | Modify TUI rendering (`userio/tui/message.go`, `userio/tui/chat_view.go`) to display assistant `output_image` parts as explicit image result entries and keep text rendering for `text` parts; do not lose existing markdown formatting for text parts. | ✅ | 2026-04-03 |
| TASK-014 | Enforce `ValidateContentParts` call sites at TUI submit path, session append/load path (`kernel/session`), adapter request emission (`adapters/openai`, `adapters/claude`), and tool-result append path (`kernel/loop` + runtime tool plumbing). |  |  |
| TASK-015 | Update TUI and runtime tests (`userio/tui/external_context_test.go`, `userio/tui/message_test.go`, `userio/tui/chat_test.go`, loop/session tests) to cover valid/invalid image attachments and rendering behavior. | ✅ | 2026-04-03 |

### Implementation Phase 4

- **GOAL-004**: Implement persistence migration, cross-package compile stabilization, and full validation.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-016 | Implement read-old/write-new migration in `kernel/session/store_file.go`: custom load-path conversion from legacy `messages[].content` and `tool_results[].content` to `content_parts`; prefer new field when both exist; return explicit errors on corrupt legacy payloads; add warning logs for dual-field inputs. | ✅ | 2026-04-03 |
| TASK-017 | Update store/session tests in `kernel/session/store_file_test.go` and related session tests to verify migration behavior, dual-field precedence, and save output shape (`content_parts` only). | ✅ | 2026-04-03 |
| TASK-018 | Sweep and update remaining repository compile references using `rg "Message\\{[^}]*Content:|\\.Content\\b|ToolResult\\{[^}]*Content:"` across `appkit/`, `agent/`, `userio/`, `examples/`, and touched tests; replace with content-part helpers or explicit `ContentParts` literals. | ✅ | 2026-04-03 |
| TASK-019 | Run formatting and verification commands: `gofmt -w` on changed Go files, `go test ./...`, and `go build ./...`; fix regressions until green; update this plan status and completion marks deterministically. | ✅ | 2026-04-03 |

## 3. Alternatives

- **ALT-001**: Keep `Message.Content` and add optional side-channel attachments was rejected because it creates dual truth and delays inevitable protocol migration.
- **ALT-002**: Implement only TUI/adapters patch without kernel protocol change was rejected because it hardcodes multimodal semantics in edges and blocks clean audio/video extension.
- **ALT-003**: Full Codex-style attachment/history parity in phase one was rejected as unnecessary scope expansion relative to requested image recognition/generation objective.

## 4. Dependencies

- **DEP-001**: `docs/superpowers/specs/2026-04-03-multimodal-image-recognition-generation-design.md` is the governing architecture and validation source.
- **DEP-002**: `github.com/openai/openai-go` SDK must support multimodal message content-part request fields used by adapter mapping.
- **DEP-003**: `github.com/anthropics/anthropic-sdk-go` SDK must support image content block request/response mapping for Claude adapter.
- **DEP-004**: `kernel/session/store_file.go` remains the source of truth for persisted session JSON migration behavior.

## 5. Files

- **FILE-001**: `kernel/port/types.go` — message and tool-result content schema cutover.
- **FILE-002**: `kernel/port/content_parts.go` (new) — validators and helper constructors/converters.
- **FILE-003**: `kernel/port/content_parts_test.go` (new) — validation and conversion tests.
- **FILE-004**: `kernel/port/llm.go` — LLM request/response semantic update for content parts.
- **FILE-005**: `kernel/loop/loop.go` — runtime part validation and message append path updates.
- **FILE-006**: `kernel/session/store_file.go` — read-old/write-new migration.
- **FILE-007**: `kernel/session/store_file_test.go` — migration regression tests.
- **FILE-008**: `adapters/openai/openai.go` — OpenAI content-part request/response mapping.
- **FILE-009**: `adapters/openai/openai_test.go` — OpenAI multimodal mapping tests.
- **FILE-010**: `adapters/claude/claude.go` — Claude content-part request/response mapping.
- **FILE-011**: `adapters/claude/claude_test.go` — Claude multimodal mapping tests.
- **FILE-012**: `userio/tui/external_context.go` — image mention parsing behavior update.
- **FILE-013**: `userio/tui/chat_update.go` and `userio/tui/chat.go` — user submission content-part assembly.
- **FILE-014**: `userio/tui/message.go` and `userio/tui/chat_view.go` — image output rendering.
- **FILE-015**: `userio/tui/external_context_test.go`, `userio/tui/message_test.go`, `userio/tui/chat_test.go` — TUI multimodal tests.
- **FILE-016**: `examples/*` and any touched `appkit/*` compile-call sites that currently set `Message.Content`.

## 6. Testing

- **TEST-001**: `go test ./kernel/port` validates content-part schema and validation rules.
- **TEST-002**: `go test ./kernel/session` validates session migration and persistence behavior.
- **TEST-003**: `go test ./kernel/loop` validates loop/runtime message flow with content parts.
- **TEST-004**: `go test ./adapters/openai ./adapters/claude ./adapters` validates provider mappings and integration surface.
- **TEST-005**: `go test ./userio/tui` validates attachment parse/render behavior.
- **TEST-006**: `go test ./...` validates full repository behavior under protocol cutover.
- **TEST-007**: `go build ./...` validates full repository compilation under protocol cutover.

## 7. Risks & Assumptions

- **RISK-001**: Protocol cutover impacts many compile sites simultaneously; missed conversions can cause broad build breaks.
- **RISK-002**: Provider SDK type constraints for image blocks may require adapter-specific normalization edge handling.
- **RISK-003**: Session migration bugs can break restore of old sessions if conversion logic is incomplete.
- **RISK-004**: TUI rendering of binary-backed outputs can degrade UX if large payload handling is not bounded.
- **ASSUMPTION-001**: Current product usage prioritizes local session migration over external long-term archival format compatibility.
- **ASSUMPTION-002**: Initial image output rendering can be metadata/path-first in terminal UX without implementing full inline image display protocol.
- **ASSUMPTION-003**: Audio/video reserved types will remain reject-only in this release and not required by acceptance criteria.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-03-multimodal-image-recognition-generation-design.md`
- `docs/architecture.md`
- `kernel/port/capability.go`
