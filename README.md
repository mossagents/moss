# MOSS Agent Harness

**Agent harness for Go: compose fast, run safely.**

Moss is a library-first agent runtime for Go. The repository is organized around a small reusable kernel, an opinionated runtime assembly layer, two core applications under `apps\`, and reference examples under `examples\`.

For Chinese documentation, see [`README_ZH.md`](README_ZH.md).

## What Moss is today

- A three-layer runtime for building Go-based AI agents:
  - **Kernel** — core runtime primitives (Agent interface, request-shaped `RunAgent`, Session, Event, Tool, Plugin).
  - **Harness** — composable orchestration layer (Feature/Backend/Harness) that wires capabilities onto a Kernel.
  - **Applications** — end-user products (`apps\mosscode`, `apps\mosswork`) and reference examples.
- An `appkit` assembly layer for building complete kernels from `AppFlags`.
- An `appkit.BuildDeepAgent(...)` preset path for coding/research/writer-style products.
- Core applications in `apps\`, with `apps\mosscode` as the primary interactive app surface and the packaged `moss` CLI entrypoint targeting `mosscode`.
- Reference examples in `examples\` for smaller integrations and product patterns.

## Quickstart

### 1. Run the primary app

The most complete interactive product surface in the current tree is `apps\mosscode`.

```powershell
Set-Location apps\mosscode
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
	"github.com/mossagents/moss/kernel"
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
	userMsg := mdl.Message{
		Role: mdl.RoleUser,
		ContentParts: []mdl.ContentPart{
			mdl.TextPart("Read README.md and summarize it"),
		},
	}
	sess.AppendMessage(userMsg)

	result, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("root"),
		UserContent: &userMsg,
	})
	if err != nil {
		panic(err)
	}
	println(result.Output)
}
```

For feature-first assembly, use `appkit.BuildKernelWithFeatures(...)`. For a fuller product preset, use `appkit.BuildDeepAgent(...)`.

### 3. Use the Harness layer

The `harness` package provides composable Features that install tools, hooks, and system-prompt extensions onto a Kernel:

```go
package main

import (
	"context"
	"time"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/retry"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/sandbox"
)

func main() {
	ctx := context.Background()

	sb, _ := sandbox.NewLocal(".")
	k := kernel.New(
		kernel.WithLLM(myLLM),
		kernel.WithSandbox(sb),
		kernel.WithUserIO(myIO),
	)

	backend := &harness.LocalBackend{
		Workspace: k.Workspace(),
		Executor:  k.Executor(),
	}
	h := harness.New(k, backend)
	_ = h.Install(ctx,
		harness.BootstrapContext(".", "myapp", "trusted"),
		harness.LLMResilience(&retry.Config{
			MaxRetries:   3,
			InitialDelay: 500 * time.Millisecond,
		}, nil),
		harness.PatchToolCalls(),
	)

	_ = k.Boot(ctx)
	defer k.Shutdown(ctx)

	sess, _ := k.NewSession(ctx, session.SessionConfig{
		Goal: "help me", MaxSteps: 50,
	})
	userMsg := mdl.Message{
		Role: mdl.RoleUser,
		ContentParts: []mdl.ContentPart{mdl.TextPart("Hello")},
	}
	sess.AppendMessage(userMsg)
	result, _ := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("root"),
		UserContent: &userMsg,
	})
	println(result.Output)
}
```

For managed deployment wiring, prefer `harness.NewWithBackendFactory(ctx, k, harness.NewLocalBackendFactory(workspace))`; that is the path used by `appkit.BuildKernel(...)` and `appkit.BuildDeepAgent(...)`.

## Repository layout

| Path | Purpose |
|---|---|
| `kernel\` | Core runtime primitives (Agent, `RunAgent`, Session, Event, Tool, Plugin) |
| `harness\` | Composable orchestration layer (Feature, Backend, Harness) |
| `appkit\` | Recommended builders, extension composition, and deep-agent preset assembly |
| `appkit\runtime\` | Default capability loading (builtin tools, MCP, skills, subagents, memory, context, scheduling) |
| `skill\` / `mcp\` / `agent\` | Capability providers, MCP bridge, delegated agents |
| `bootstrap\`, `config\`, `providers\`, `logging\` | Support packages |
| `knowledge\`, `scheduler\`, `gateway\`, `distributed\`, `sandbox\` | Higher-level runtime building blocks |
| `apps\` | Core application surfaces (`mosscode`, `mosswork`) |
| `examples\` | Runnable reference and integration examples |

## Configuration

The default application name in the core config package is `moss`, so library users that keep the default naming model can use:

```text
~\.moss\config.yaml
```

Applications and examples override the app name and therefore use their own directories, such as:

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

## Applications and examples

Core apps in `apps\`:

- `mosscode` - coding agent product surface and target of the packaged `moss` CLI entrypoint
- `mosswork` - desktop work/collaboration assistant

Reference apps in `examples\`:

- `mossresearch` - deep research orchestrator
- `mosswriter` - content workflow agent
- `mossclaw` - assistant / gateway / scheduling / knowledge example
- `mossquant` - stateful analysis loop
- `mossroom` - realtime multi-user room
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
