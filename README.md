# Moss

**Agent harness for Go: compose fast, run safely.**

Moss provides a ready-to-run agent stack (CLI + runtime + extension surface) while keeping the core composable and library-first.

For Chinese documentation, see [`README_ZH.md`](README_ZH.md).

## Why Moss

- Start fast: run a coding agent in minutes with `moss`.
- Build your own: embed Moss as a Go library and control runtime behavior.
- Production-minded defaults: policy, sandbox, session, and tool boundaries.

## What is included

- Planning and task tracking tools (including deepagent-style flows).
- Filesystem and command-execution tools with trust-level controls.
- Sub-agent delegation for multi-agent workflows.
- Interactive TUI and headless execution.
- Extension-friendly architecture with middleware and appkit assembly APIs.

## Quickstart

### 1) Install CLI

```bash
go install github.com/mossagents/moss/cmd/moss@latest
```

### 2) Run in terminal

```bash
# Interactive TUI
moss

# Non-interactive run
moss run --goal "Fix the bug in main.go" --workspace .

# Version
moss version
```

### 3) Embed as a Go library

```go
package main

import (
	"context"
	"os"

	"github.com/mossagents/moss/appkit"
	intr "github.com/mossagents/moss/kernel/io"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

func main() {
	ctx := context.Background()

	k, err := appkit.BuildKernel(ctx, &appkit.AppFlags{
		Provider:  "openai",
		Model:     "gpt-4o",
		Workspace: ".",
		APIKey:    os.Getenv("OPENAI_API_KEY"),
	}, intr.NewConsoleIO())
	if err != nil {
		panic(err)
	}

	if err := k.Boot(ctx); err != nil {
		panic(err)
	}
	defer k.Shutdown(ctx)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:     "Fix the bug in main.go",
		Mode:     "oneshot",
		MaxSteps: 50,
	})
	if err != nil {
		panic(err)
	}
	sess.AppendMessage(mdl.Message{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("Fix the bug in main.go")}})

	result, err := k.Run(ctx, sess)
	if err != nil {
		panic(err)
	}
	println(result.Output)
}
```

For extension-first assembly, use `appkit.BuildKernelWithExtensions(...)`.

## CLI at a glance

- `moss`: launch interactive TUI.
- `moss run --goal "..."`: run one goal with flags such as `--workspace`, `--provider`, `--model`, and `--trust`.
- `moss version`: print CLI version.

## Configuration

Global config path: `~/.moss/config.yaml`

```yaml
provider: openai
model: gpt-4o
base_url: ""
api_key: ""
skills:
  - name: my-mcp-server
    transport: stdio
    command: npx
    args: ["-y", "@example/mcp-server"]
```

Priority: CLI flags > config file > environment variables

Common environment variables:

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`
- `ANTHROPIC_API_KEY`
- `GEMINI_API_KEY` (or `GOOGLE_API_KEY`)
- `MOSS_DEBUG=1` (write debug logs to `~/.moss/debug.log`)

## Architecture

Moss is organized into a minimal runtime core plus top-level feature packages:

- `kernel/`: runtime primitives (loop, tool, session, middleware, port).
- `appkit/`: high-level assembly helpers.
- `agent/`, `skill/`, `bootstrap/`, `knowledge/`, `scheduler/`, `gateway/`: feature and support packages.
- `cmd/moss/`: terminal CLI and TUI entrypoints.

## Presets and customization

- Use `presets/deepagent` for deepagent-style defaults (planning, context compaction, task lifecycle).
- Add middleware for policy, audit, events, and guardrails.
- Add custom tools, skills, and MCP servers through runtime setup and config.

## Examples

Reference applications live in `examples/`:

- `examples/mosscode/` - coding assistant
- `examples/mossresearch/` - deep research orchestrator with delegated web research
- `examples/mosswriter/` - content builder agent with filesystem-based writing workflows
- `examples/mosswork-desktop/` - desktop cowork assistant with delegated agents and persistent runtime state
- `examples/mossclaw/` - web automation and scraping workflows
- `examples/mossquant/` - stateful autonomous loop patterns
- `examples/mossroom/` - realtime multi-user agent game

Run an example:

```bash
cd examples/mosscode
go run .
```

## Documentation

- [Getting Started](docs/getting-started.md)
- [Architecture](docs/architecture.md)
- [Skills](docs/skills.md)
- [Kernel Design](docs/kernel-design.md)
- [Production Readiness](docs/production-readiness.md)
- [Changelog](docs/changelog.md)
- [Roadmap](docs/roadmap.md)

## Security model

Moss follows a tool-boundary security model: the agent can only do what exposed tools allow.

Use sandbox, policy, and tool-level controls rather than relying on prompt-only restrictions.

## Development checks

```bash
go vet ./...
go test ./...
pwsh ./testing/validate_examples.ps1
go build ./...
```

For per-example verification:

```bash
cd examples/<name>
go test ./...
go build .
```

Note: `go build ./...` is not a strict pass/fail gate for every example module because some packaging helper directories (for example `examples/mosswork-desktop/build/ios`) are not standalone runnable `main` packages.

## Compatibility

- Module path: `github.com/mossagents/moss`
- `go.mod` target: Go `1.25.0`

## License

MIT
