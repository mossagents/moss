# mosswork

`mosswork` is a lightweight but production-oriented desktop cowork assistant built on `moss` + Wails.

## What it includes

- Deep harness runtime (`presets/deepagent.BuildKernel`)
- Persistent sessions (`~/.mosswork/sessions`)
- Persistent memories (`~/.mosswork/memories`)
- Context offload (`offload_context`)
- Async task lifecycle (`task`, `list_tasks`, `cancel_task`)
- Scheduler with persistence (`~/.mosswork/schedules/jobs.json`)
- Desktop dashboard events (`desktop:dashboard`, `desktop:sessions`, `desktop:schedules`)

## Runtime slash commands

Type these directly in chat input:

- `/session` - show current session summary
- `/sessions` - list persisted sessions
- `/resume <session_id>` - resume a session
- `/compact [keep_recent] [note]` - compact context
- `/tasks [status] [limit]` - list async tasks
- `/task <task_id>` - query task result
- `/task cancel <task_id> [reason]` - cancel task
- `/schedules` - list schedules
- `/schedule <id> <@every|@after|@once> <goal>` - create schedule
- `/schedule-cancel <id>` - remove schedule
- `/dashboard` - print dashboard snapshot

## Configure

Environment variable precedence follows appkit defaults. Desktop-specific prefix is:

- `MOSSWORK_DESKTOP_*` (falls back to `MOSS_*`)

Common values:

- `MOSSWORK_DESKTOP_PROVIDER`
- `MOSSWORK_DESKTOP_MODEL`
- `MOSSWORK_DESKTOP_API_KEY`
- `MOSSWORK_DESKTOP_BASE_URL`
- `MOSSWORK_DESKTOP_WORKSPACE`

## Development

```bash
cd apps/mosswork/frontend
npm install
npm run build

cd ..
# Requires Wails v3 toolchain
wails3 dev
```

## Notes

- In this environment, Go toolchain is `1.24.2`, while example module targets `go 1.25`, so `go test ./...` may fail locally until matching toolchain is installed.
- Validation recommendation:
  - Use `go test ./...` for module tests.
  - Use `go build .` for module build verification.
  - Avoid treating `go build ./...` as a strict gate because helper packaging directories such as `build/ios` are not standalone runnable `main` packages.
