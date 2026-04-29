# GoGents

**GoGents** is an automation daemon written in Go that bridges external task sources (e.g. GitHub Issues) with LLM-powered coding agents (Gemini CLI, Claude Code). It continuously syncs assigned tasks, dispatches them to an AI agent for execution, and reports results back to the originating platform.

## Features

- **Task Synchronization** — Periodically polls GitHub Issues (via `gh` CLI) for tasks tagged with a configurable label, and maintains a local task store with YAML-based persistence.
- **Multi-Agent Support** — Pluggable agent architecture supporting both [Gemini CLI](https://github.com/google-gemini/gemini-cli) and [Claude Code](https://docs.anthropic.com/en/docs/claude-code). New agents can be added by implementing a simple interface.
- **Automated Execution Pipeline** — For each task: fetches issue content → prepares `input.md` → creates a Git branch → invokes the agent → judges the result → reports back to GitHub with comments and label transitions.
- **Graceful Lifecycle Management** — Signal-aware daemon with proper startup validation (preflight checks), crash recovery (requeues interrupted tasks), and graceful shutdown.
- **Structured Logging** — JSONL-formatted logs with Asia/Taipei timezone, split into `daemon.jsonl` (all events) and `error.jsonl` (errors only).
- **GitHub Feedback** — Automated issue commenting with status updates, PR links, and `gogents:*` label rotation.

## Architecture

```
┌────────────┐     ┌──────────┐     ┌───────────────┐     ┌─────────────┐
│  GitHub    │────▶│  Syncer  │────▶│  Task Store   │────▶│   Runner    │
│  Issues    │◀────│  (D02)   │     │  (D01)        │     │   (D03)     │
└────────────┘     └──────────┘     └───────────────┘     └──────┬──────┘
                                                                  │
                                                          ┌───────▼───────┐
                                                          │  Agent CLI    │
                                                          │ gemini/claude │
                                                          └───────────────┘
```

| Module | Package               | Description                                        |
| ------ | --------------------- | -------------------------------------------------- |
| D01    | `internal/foundation` | Config, workspace paths, task store, state machine |
| D02    | `internal/daemon`     | Daemon lifecycle, sync loop, state persistence     |
| D03    | `internal/execution`  | Task runner, result judge, input builder           |
| D04    | `internal/logger`     | Structured JSONL logger                            |
| D05    | `internal/source`     | GitHub client, feedback reporter, content fetcher  |
| D06    | `internal/agent`      | Gemini/Claude probe & executor adapters            |

## Prerequisites

- **Go** 1.23+
- **[mise](https://mise.jdx.dev/)** — Task runner & toolchain manager
- **[gh](https://cli.github.com/)** — GitHub CLI (authenticated via `gh auth login`)
- **Agent CLI** — At least one of:
  - [Gemini CLI](https://github.com/google-gemini/gemini-cli) (`gemini`)
  - [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (`claude`)

## Quick Start

### 1. Clone & Install Tools

```bash
git clone https://github.com/dccoding1118/gogents.git
cd gogents
mise install   # installs Go, golangci-lint, gofumpt
```

### 2. Build

```bash
mise run build
# Output: bin/gogents
```

### 3. Configure

Create a runtime config at `<workspace>/.gogents/runtime.yaml`:

```yaml
runtime_name: "my-workspace"
project:
  repo:
    url: "https://github.com/<owner>/<repo>"
source:
  type: "github_issue"
  instance: "<owner>/<repo>"
  filter:
    label: "gogents:queued"
  sync_interval: 30s
agent:
  type: "gemini"         # or "claude"
  model: "gemini-2.5-pro"
```

### 4. Run

```bash
bin/gogents start --workspace /path/to/workspace
```

The daemon will:
1. Validate config & run preflight checks (git, gh auth, agent CLI)
2. Initialize workspace directory structure
3. Enter the sync loop — poll → execute → report → repeat
4. Shut down gracefully on `SIGTERM` / `SIGINT`

## Development

```bash
# Format code
mise run fmt

# Lint
mise run lint

# Run all unit tests with coverage
mise run test
```

### Project Structure

```
cmd/gogents/       # CLI entrypoint
internal/
  foundation/      # Config, workspace, task store
  daemon/          # Daemon lifecycle, syncer
  execution/       # Task runner, judge
  logger/          # Structured JSONL logger
  source/          # GitHub client adapters
  agent/           # LLM agent adapters
  gitops/          # Git operations
docs/
  designs/         # Architecture design documents (D01–D06)
  requirements/    # Requirement specifications
  tests/           # Unit test planning
```

## License

This project is currently unlicensed. All rights reserved.
