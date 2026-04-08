# Moss

**Agent harness for Go: compose fast, run safely.**

Moss is a library-first agent runtime for Go. The repository is organized around a small reusable kernel, an opinionated runtime assembly layer, and product-style example apps such as `examples\mosscode`.

For Chinese documentation, see [`README_ZH.md`](README_ZH.md).

## What Moss is today

- A reusable `kernel` for running agent sessions with tools, middleware, policy, and observation.
- An `appkit` assembly layer for building complete kernels from `AppFlags`.
- A `presets\deepagent` preset for coding/research/writer-style products.
- Example apps in `examples\` that act as the real runnable entrypoints in this repository.

## Quickstart

### 1. Run the primary example app

The most complete interactive product surface in the current tree is `examples\mosscode`.

```powershell
Set-Location examples\mosscode
go run . --provider openai --model gpt-4o
```

Useful variants:

```powershell
# Interactive TUI
go run .

# One-shot execution
go run . --prompt "Summarize the repository structure"

# Diagnostics
go run . doctor
```

### 2. Embed Moss as a Go library

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
		Goal:     "Read README.md and summarize it",
		Mode:     "oneshot",
		MaxSteps: 20,
	})
	if err != nil {
		panic(err)
	}
	sess.AppendMessage(mdl.Message{
		Role: mdl.RoleUser,
		ContentParts: []mdl.ContentPart{
			mdl.TextPart("Read README.md and summarize it"),
		},
	})

	result, err := k.Run(ctx, sess)
	if err != nil {
		panic(err)
	}
	println(result.Output)
}
```

For extension-first assembly, use `appkit.BuildKernelWithExtensions(...)`. For a fuller product preset, use `presets\deepagent.BuildKernel(...)`.

## Repository layout

| Path | Purpose |
|---|---|
| `kernel\` | Core runtime primitives |
| `appkit\` | Recommended builders and extension composition |
| `appkit\runtime\` | Default capability loading (builtin tools, MCP, skills, subagents, memory, context, scheduling) |
| `presets\deepagent\` | Product preset for deep-agent style apps |
| `skill\` / `mcp\` / `agent\` | Capability providers, MCP bridge, delegated agents |
| `bootstrap\`, `config\`, `providers\`, `logging\` | Support packages |
| `knowledge\`, `scheduler\`, `gateway\`, `distributed\`, `sandbox\` | Higher-level runtime building blocks |
| `examples\` | Runnable product and integration examples |

## Configuration

The default application name in the core config package is `moss`, so library users that keep the default naming model can use:

```text
~\.moss\config.yaml
```

Example apps override the app name and therefore use their own directories, such as:

- `~\.mosscode\config.yaml`
- `~\.mossresearch\config.yaml`
- `~\.mosswriter\config.yaml`

Typical config:

```yaml
provider: openai
model: gpt-4o
base_url: ""
api_key: ""
default_profile: coding

skills:
  - name: github
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
```

Priority is:

**CLI flags > environment variables > config file**

## Examples

Reference apps in `examples\`:

- `mosscode` - coding agent product surface
- `mossresearch` - deep research orchestrator
- `mosswriter` - content workflow agent
- `mossclaw` - assistant / gateway / scheduling / knowledge example
- `mossquant` - stateful analysis loop
- `mossroom` - realtime multi-user room
- `mosswork-desktop` - desktop assistant
- `basic`, `custom-tool`, `websocket` - focused integration examples

## Documentation

- [Getting Started](docs/getting-started.md)
- [Architecture](docs/architecture.md)
- [Skills](docs/skills.md)
- [Kernel Design](docs/kernel-design.md)
- [Production Readiness](docs/production-readiness.md)
- [Changelog](docs/changelog.md)
- [Roadmap](docs/roadmap.md)

## Development checks

```powershell
go test ./...
go build ./...
```

## Compatibility

- Module path: `github.com/mossagents/moss`
- `go.mod` target: Go `1.25.0`

## License

MIT
