# GoGents

**GoGents** is an automation daemon written in Go that bridges external task sources (GitHub Issues, or a local ticket inbox from [agent-manager](#agent-manager-integration)) with LLM-powered coding agents (Gemini CLI, Claude Code). It continuously syncs assigned tasks, dispatches them to an AI agent for execution, and reports results back to the originating source.

## Features

- **Pluggable Task Sources** — `github_issue` polls GitHub Issues (via `gh` CLI) by label; `local` reads an am ticket inbox (`~/.am/tickets/`) filtered by worker label. Both share one task store, state machine, and execution pipeline.
- **Multi-Agent Support** — Pluggable agent architecture supporting both [Gemini CLI](https://github.com/google-gemini/gemini-cli) and [Claude Code](https://docs.anthropic.com/en/docs/claude-code). New agents can be added by implementing a simple interface.
- **Automated Execution Pipeline** — For each task: fetches task content → prepares `input.md` → creates a Git branch → invokes the agent (with the **workspace root** as its working directory, so workspace-level `.claude/` and `.mcp.json` apply) → judges the result → reports back to the source.
- **Ticket-Declared Verification** — A `local` ticket may declare a `verify` command; the daemon runs it in the workspace and uses its exit code to judge pass/fail, falling back to the `result.json` coding verdict when unspecified.
- **Graceful Lifecycle Management** — Signal-aware daemon with startup validation (preflight checks), crash recovery (requeues interrupted tasks), and graceful shutdown.
- **Structured Logging** — JSONL-formatted logs with Asia/Taipei timezone, split into `daemon.jsonl` (all events) and `error.jsonl` (errors only).
- **Runtime Registration & Observability** — `start` registers the runtime into `~/.gogents/config.yaml` (keyed by workspace path); `gogents runtime list/status` and `gogents logs` expose live `runtime_state`, PID, and logs for auditing.
- **Source Feedback** — `github_issue` updates issue comments, PR links, and `gogents:*` labels; `local` writes the terminal `status` and `result` back to the ticket file.

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
| D05    | `internal/source`     | Task sources (`github_issue`, `local`), feedback reporters, content fetchers, dispatcher |
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

Create a runtime config at `<workspace>/.gogents/runtime.yaml`.

**GitHub Issue source:**

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

**Local ticket source (agent-manager):**

```yaml
runtime_name: "my-worker"
project:
  repo:
    url: "https://github.com/<owner>/<repo>"
source:
  type: "local"
  path: "~/.am/tickets"    # am ticket inbox (default)
  filter:
    label: "<this-worker-label>"
  sync_interval: 30s
agent:
  type: "claude"
  model: "claude-sonnet-5"
```

### 4. Run

```bash
bin/gogents start --workspace /path/to/workspace
```

The daemon will:
1. Validate config & run preflight checks (git, source toolchain, agent CLI)
2. Initialize workspace directory structure and register the runtime into `~/.gogents/config.yaml`
3. Enter the sync loop — poll → execute → verify → report → repeat
4. Shut down gracefully on `SIGTERM` / `SIGINT`

### Observability

```bash
# List all registered runtimes (workspace / name / registered_at)
bin/gogents runtime list

# Live status of one runtime (runtime_state, PID, current task, last sync);
# reports "down" if the daemon PID is no longer alive
bin/gogents runtime status <workspace|name>

# Print a runtime's logs (daemon.jsonl / error.jsonl)
bin/gogents logs <workspace|name>
```

## agent-manager Integration

GoGents is the **execution plane** for [agent-manager](https://github.com/dccoding1118/agent-manager) (am), the local agent control plane (Tower). am produces tickets into `~/.am/tickets/`; a GoGents worker daemon configured with `source.type: local` claims tickets matching its worker label, executes them, runs the ticket's `verify` command, and writes the terminal `status` and `result` back to the ticket file. Communication is one-way (`am → gogents`): am queries live execution state via `gogents runtime status` / `gogents logs` rather than caching it. See `docs/requirements/reqs/R11-local-source.md` and `R12-runtime-observability.md`.

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
  source/          # Task source adapters (github_issue, local) + dispatcher
  agent/           # LLM agent adapters
  gitops/          # Git operations
docs/
  designs/         # Architecture design documents (D01–D08)
  requirements/    # Requirement specifications (R01–R12)
  changes/         # Change proposals (CH-00x)
  tests/           # Unit test planning
```

## License

This project is currently unlicensed. All rights reserved.
