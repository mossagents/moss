# Multimodal Image Recognition + Generation Design

> **Archived historical design note.** This file records a past design iteration and is not the canonical source for the current architecture.

## Problem Statement

`moss` has model capability tags for image/audio/video in `kernel/port`, but runtime message transport is still text-centric (`Message.Content string`). In current TUI flow, image mentions are converted to path references and appended as plain text, so image understanding/generation cannot be represented as first-class model I/O.

This causes three issues:

1. Kernel cannot act as a single source of truth for multimodal turns.
2. Adapter layers must rely on lossy text conventions instead of structured media payloads.
3. Future audio/video expansion would require another protocol break instead of incremental extension.

## Goals

1. Make multimodal image input/output first-class in kernel message protocol.
2. Enable end-to-end image understanding and image generation for OpenAI + Claude adapters.
3. Keep architecture extensible for audio/video without another core schema redesign.
4. Ensure unsupported media cases fail explicitly and traceably (no silent degradation).

## Non-Goals

1. No first-phase audio/video implementation behavior.
2. No runtime compatibility mode that continues to use old `Message.Content` as first-class protocol after migration.
3. No UI-heavy attachment gallery/history overhaul equivalent to Codex in phase one.

## Proposed Architecture

### 1. Kernel Port Protocol: `ContentPart` as canonical payload

Refactor `kernel/port` message model from text-only to structured parts.

- Replace `Message.Content string` with `Message.ContentParts []ContentPart`.
- Keep tool calls/results model unchanged except where tool payload text conversion depends on content shape.
- Add discriminated union style part schema:
  - `text`
  - `input_image`
  - `output_image`
  - reserved now for future extension:
    - `input_audio`, `output_audio`
    - `input_video`, `output_video`

Suggested shape:

```go
type ContentPartType string

const (
    ContentPartText       ContentPartType = "text"
    ContentPartInputImage ContentPartType = "input_image"
    ContentPartOutputImage ContentPartType = "output_image"

    ContentPartInputAudio  ContentPartType = "input_audio"
    ContentPartOutputAudio ContentPartType = "output_audio"
    ContentPartInputVideo  ContentPartType = "input_video"
    ContentPartOutputVideo ContentPartType = "output_video"
)

type ContentPart struct {
    Type       ContentPartType `json:"type"`
    Text       string          `json:"text,omitempty"`
    MIMEType   string          `json:"mime_type,omitempty"`
    DataBase64 string          `json:"data_base64,omitempty"`
    URL        string          `json:"url,omitempty"`
    SourcePath string          `json:"source_path,omitempty"`
}
```

Design rule:

- `input_image` accepts exactly one source:
  - inline bytes: `{mime_type, data_base64}` with both non-empty, `url` empty.
  - remote source: `{url}` non-empty, `mime_type` and `data_base64` empty.
- `input_image` is invalid when:
  - both inline and URL are set,
  - neither source is set,
  - `mime_type` does not match `image/*` for inline form.
- `output_image` uses the same exclusivity/validation rule as `input_image`.
- `text` remains a normal part instead of a special message field.
- `source_path` is optional local provenance for UI/debug/persistence (never sent to model providers).

Validation matrix:

| Type | Required fields | Forbidden fields |
|---|---|---|
| `text` | `text` | `mime_type`, `data_base64`, `url` |
| `input_image` (inline) | `mime_type`, `data_base64` | `text`, `url` |
| `input_image` (url) | `url` | `text`, `mime_type`, `data_base64` |
| `output_image` (inline) | `mime_type`, `data_base64` | `text`, `url` |
| `output_image` (url) | `url` | `text`, `mime_type`, `data_base64` |
| `input_audio`/`output_audio`/`input_video`/`output_video` | reserved in phase one, reject at runtime | all media-specific fields until implemented |

Enforcement ownership:

1. Canonical validation lives in `kernel/port` (`ValidateContentParts(parts []ContentPart) error`).
2. Required call sites:
   - TUI before message submit,
   - session append/load path,
   - adapters before provider request emit,
   - tool-result ingestion before append.
3. Adapters may add provider-specific checks, but cannot weaken kernel validation rules.

### 2. Adapter boundaries remain pure mapping

`adapters/openai` and `adapters/claude` map `[]ContentPart` to provider-native multimodal request blocks and map responses back to `[]ContentPart`.

- No provider logic leaks into kernel.
- No TUI-origin placeholder text embedded into model payload.
- Capability-based routing (`TaskRequirement.Capabilities`) remains unchanged and becomes immediately useful for multimodal turns.

### 3. TUI boundaries: collect/render parts, not protocol semantics

`userio/tui` handles:

1. composer parsing (`@image-path` -> `input_image` part),
2. local validation + MIME detect + base64 encode,
3. rendering assistant `output_image` parts as image result entries with save/open actions.

TUI does not invent adapter-specific message shape.

## Data Flow

### A. Image understanding input flow

1. User submits text + local image attachment.
2. TUI builds user `Message{Role:user, ContentParts:[text, input_image]}`.
3. Session stores canonical parts.
4. Loop sends parts to selected adapter.
5. Adapter maps to provider request blocks.
6. Provider response maps back to assistant `ContentParts` (text and/or media).

### B. Image generation output flow

1. Model returns image output (inline bytes or URL).
2. Adapter normalizes it to `output_image` parts.
3. Loop/session preserve parts without text downgrade.
4. TUI renders output image entries and exposes local save/open UX.

### C. Tool result multimodal flow

If a tool emits multimodal output, tool result schema is upgraded to preserve structured parts:

```go
type ToolResult struct {
    CallID       string        `json:"call_id"`
    ContentParts []ContentPart `json:"content_parts,omitempty"`
    IsError      bool          `json:"is_error,omitempty"`
}
```

Contract:

1. `ToolResult.ContentParts` is authoritative payload for model roundtrip.
2. Any legacy plain-text conversion is derived/auxiliary only.
3. Adapters must map tool result parts to provider-native tool-result content blocks.
4. Session persistence stores only `content_parts` for tool results after migration save.
5. Runtime and adapter codepaths are required to reject `ToolResult` entries with empty `content_parts`.

## Error Handling

### Input-side media errors (TUI)

Fail fast with explicit errors for:

1. file missing/unreadable,
2. non-image MIME,
3. unsupported image format,
4. base64 encode failures.

Errors include path + cause and are surfaced to user as actionable messages.

### Adapter/provider capability errors

If selected model/provider does not support an incoming part type:

1. return structured adapter error (unsupported part type + model/provider),
2. include capability hint for routing (`image_understanding` / `image_generation`),
3. do not silently drop part.

### Response parse errors

When provider returns malformed/partial media payload:

1. return explicit parse error for invalid payload,
2. only recover when provider contract allows deterministic fallback,
3. preserve stream/tool integrity guarantees.

## Testing Strategy

### 1. Kernel/port tests

1. `Message` + `ContentPart` serialization/deserialization.
2. session append/retrieve behavior with mixed text/image parts.
3. loop pass-through invariants: parts unchanged across runtime boundaries.

### 2. Adapter tests

OpenAI and Claude:

1. request mapping tests (`text + input_image`),
2. response mapping tests (`text + output_image`),
3. unsupported part failures,
4. stream behavior with mixed text/tool/media responses when supported.

### 3. TUI tests

1. mention/attachment parse tests,
2. MIME validation and encoding tests,
3. rendering snapshot tests for input + output image rows,
4. explicit error surface tests for invalid image sources.

### 4. End-to-end smoke

1. text-only regression still works under new part schema,
2. image-understanding roundtrip,
3. image-generation roundtrip.

## Migration & Rollout

Phase 1 is an intentional protocol cutover (no backward compatibility mode):

1. update `kernel/port` types,
2. update all adapters and runtime call sites,
3. update TUI composer/rendering,
4. fix compile/test breakages caused by schema switch,
5. ship with updated docs and examples.

Persistence migration (read-old/write-new only):

1. `kernel/session/store_file.go` loader supports legacy sessions that contain `messages[].content` by converting them to `messages[].content_parts=[{type:text,text:<content>}]` in memory.
2. On next save, sessions persist only new `content_parts` schema.
3. Legacy `tool_results[].content` is mapped to `tool_results[].content_parts=[{type:text,text:<content>}]` during load when encountered.
4. If both legacy and new fields are present, prefer new fields and emit a migration warning log.
5. Corrupt legacy message/tool payload fails fast with explicit load error (no silent drop).

Release gate:

1. Before release, all runtime paths constructing `port.Message` and `port.ToolResult` must compile against `content_parts`-only contract.
2. Rollback strategy is binary-level rollback to prior release; no dual-write mode in this phase.

Risk control:

1. keep scope to image input/output only in behavior,
2. reserve audio/video part types now to avoid future schema churn.

## Open Questions Resolved in This Spec

1. **Compatibility strategy**: no legacy string schema in phase one.
2. **Scope**: image understanding + image generation both included.
3. **Extensibility**: audio/video enabled at schema level now, behavior later.

