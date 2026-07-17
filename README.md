# GoGents

**GoGents** is an automation daemon written in Go that bridges external task sources (GitHub Issues, or a local ticket inbox from [agent-manager](#agent-manager-integration)) with LLM-powered coding agents (Claude Code). It continuously syncs assigned tasks, dispatches them to an AI agent for execution, and reports results back to the originating source.

## Architecture

```
┌──────────────────┐
│  GitHub Issues   │──┐
└──────────────────┘  │
                      │   ┌──────────┐     ┌──────────────┐     ┌───────────┐
                      ├──▶│  Syncer  │────▶│  Task Store  │────▶│  Runner   │
                      │   │  (D02)   │     │  (D01)       │     │  (D03)    │
┌──────────────────┐  │   └──────────┘     └──────────────┘     └─────┬─────┘
│  Local Tickets   │──┘                                               │
│ (ticket inbox)   │                                           ┌──────▼──────┐
└──────────────────┘                                           │  Agent CLI  │
         ▲                                                     │   claude    │
         │                                                     └──────┬──────┘
         └───────────────── feedback (results) ────────────────────┘
```

Two task sources feed one shared pipeline: `github_issue` (GitHub Issues via `gh`) and `local` (a ticket inbox on disk, `<GOGENTS_HOME>/tickets/` by default). Results are reported back to the originating source.

| Module | Package               | Description                                        |
| ------ | --------------------- | -------------------------------------------------- |
| D01    | `internal/foundation` | Config, workspace paths, task store, state machine |
| D02    | `internal/daemon`     | Daemon lifecycle, sync loop, state persistence     |
| D03    | `internal/execution`  | Task runner, result judge, input builder           |
| D04    | `internal/logger`     | Structured JSONL logger                            |
| D05    | `internal/source`     | Task sources (`github_issue`, `local`), feedback reporters, content fetchers, dispatcher |
| D06    | `internal/agent`      | Claude probe & executor adapter                    |
| D09    | `internal/provision`  | Workspace provisioning from a built-in or user-supplied template (`runtime create`) |

## Features

- **One-Command Workspace Provisioning** — `gogents runtime create <name>` materializes a ready-to-start workspace (runtime.yaml + agent runtime elements) from a template embedded in the binary, so a downloaded binary needs no external files. Point `--template` at your own tree to provision a specialty repeatedly. It never deletes anything under your directory, and asks before writing into a non-empty one.
- **Pluggable Task Sources** — `github_issue` polls GitHub Issues (via `gh` CLI) by label; `local` reads a ticket inbox on disk (`<GOGENTS_HOME>/tickets/` by default), claiming tickets that match the worker label **and** carry `status: queued`. Any tool that writes the ticket format can feed it. Both share one task store, state machine, and execution pipeline.
- **Pluggable Agent Architecture** — Agents sit behind a simple probe/executor interface. [Claude Code](https://docs.anthropic.com/en/docs/claude-code) is the officially supported agent; new agents can be added by implementing the interface.
- **Automated Execution Pipeline** — For each task: fetches task content → prepares `input.md` → creates a Git branch → invokes the agent (with the **workspace root** as its working directory, so workspace-level `.claude/` and `.mcp.json` apply) → judges the result → reports back to the source.
- **Ticket-Declared Verification** — A `local` ticket may declare a `verify` command; the daemon runs it in the workspace and uses its exit code to judge pass/fail, falling back to the `result.json` coding verdict when unspecified.
- **Graceful Lifecycle Management** — Signal-aware daemon with startup validation (preflight checks), crash recovery (requeues interrupted tasks), and graceful shutdown.
- **Structured Logging** — JSONL-formatted logs with Asia/Taipei timezone, split into `daemon.jsonl` (all events) and `error.jsonl` (errors only).
- **Runtime Registration & Observability** — `start` registers the runtime into `~/.gogents/config.yaml` (keyed by workspace path); `gogents runtime list/status` and `gogents logs` expose live `runtime_state`, PID, and logs for auditing.
- **Source Feedback** — `github_issue` updates issue comments, PR links, and `gogents:*` labels; `local` writes the terminal `status` and `result` back to the ticket file.

## Prerequisites

To run gogents:

- **[git](https://git-scm.com/)**
- **[gh](https://cli.github.com/)** — GitHub CLI (authenticated via `gh auth login`)
- **[Claude Code](https://docs.anthropic.com/en/docs/claude-code)** (`claude`) — the coding agent CLI

To build it from source, additionally:

- **Go** 1.23+
- **[mise](https://mise.jdx.dev/)** — Task runner & toolchain manager

## Install

### From a release

Download the archive for your platform from [Releases](https://github.com/dccoding1118/gogents/releases) and put the binary on your `PATH`:

```bash
VERSION=v0.1.0
OS=$(uname -s | tr '[:upper:]' '[:lower:]')   # linux | darwin
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

curl -fsSL -o gogents.tar.gz \
    "https://github.com/dccoding1118/gogents/releases/download/${VERSION}/gogents_${VERSION}_${OS}_${ARCH}.tar.gz"
tar -xzf gogents.tar.gz
sudo install gogents_${VERSION}_${OS}_${ARCH}/gogents /usr/local/bin/
```

Archives are published for **linux** and **macOS** on `amd64` and `arm64`; verify a download against the release's `checksums.txt` if you like. Windows is not supported — the daemon's process-liveness probe relies on POSIX signals.

The binary is self-contained: the workspace template is compiled into it, so `runtime create` works with nothing else installed. Each archive also carries a plain copy of `templates/` for reading and copying (see [Custom templates](#custom-templates)) and the `LICENSE`.

### From source

See [Quick Start](#quick-start) below — clone, `mise install`, `mise run build`.

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

### 3. Create a runtime

`runtime create` provisions a ready-to-start workspace from the built-in template — `.gogents/runtime.yaml` plus the agent runtime elements (`.claude/CLAUDE.md`, `.claude/skills/`, `.mcp.json`):

```bash
# Local ticket source
bin/gogents runtime create my-worker \
    --repo https://github.com/<owner>/<repo> \
    --agent claude --model claude-sonnet-5 \
    --source local

# GitHub Issue source
bin/gogents runtime create my-workspace \
    --repo https://github.com/<owner>/<repo> \
    --agent claude --model claude-sonnet-5 \
    --source github_issue --instance <owner>/<repo> \
    --label gogents:queued
```

| Flag | Required | Default | Notes |
| --- | --- | --- | --- |
| `<name>` | yes | — | Runtime name (positional) |
| `--template` | no | — | Template tree: a path or a name (see [Custom templates](#custom-templates)); omit to use the built-in one |
| `--workspace` | no | `./<name>` | Workspace root |
| `--repo` | yes | — | Repo URL |
| `--branch` | no | `main` | Default branch |
| `--agent` | yes | — | `claude` |
| `--model` | yes | — | Agent model |
| `--source` | yes | — | `local` or `github_issue` |
| `--label` | no | `<name>` | Task filter label |
| `--instance` | for `github_issue` | — | `<owner>/<repo>` |
| `--tickets-dir` | no | — | `local` ticket inbox; omit to use the default |
| `--force` | no | false | Skip the confirmation prompt on a non-empty workspace |

`create` only builds the workspace — it does not register the runtime (that happens on `start`) and does not clone the repo (the daemon does that at startup). Pointing it at a non-empty directory prompts for confirmation, listing exactly what will be written or overwritten; nothing else in the directory is touched, and **no file is ever deleted**, including on failure. Non-interactive shells refuse instead of prompting — pass `--force` to proceed.

### Custom templates

The built-in template is a generic starting point. To turn a specialty — working conventions, skills, a set of MCP servers — into something you can provision repeatedly, copy `templates/base/` (shipped in the release archive and in this repo), edit it, and point `--template` at it:

```bash
cp -r templates/base ./my-tpl
$EDITOR ./my-tpl/.claude/CLAUDE.md

bin/gogents runtime create my-worker --template ./my-tpl \
    --repo https://github.com/<owner>/<repo> \
    --agent claude --model claude-sonnet-5 --source local
```

`--template` takes either form; the value **contains a path separator** ⇒ it's a path, otherwise it's a name:

| `--template` | Template tree used |
| --- | --- |
| `./my-tpl`, `/opt/tpl`, `a/b` (path) | That directory. `GOGENTS_TEMPLATES` is ignored. |
| `rust-svc` (name), `GOGENTS_TEMPLATES` set | `$GOGENTS_TEMPLATES/rust-svc/` |
| `rust-svc` (name), `GOGENTS_TEMPLATES` unset | `./rust-svc/` |
| omitted | The template built into the binary |

**`GOGENTS_TEMPLATES`** points at a directory holding your templates, one subdirectory per name — mirroring this repo's `templates/` layout, so `cp -r templates/base $GOGENTS_TEMPLATES/rust-svc` just works:

```bash
export GOGENTS_TEMPLATES=~/gogents-templates   # holds rust-svc/, docs-writer/, …
bin/gogents runtime create my-worker --template rust-svc --repo … --source local
```

It is a **base for name lookups, not a search path**: a single directory (not `PATH`-style), and it only applies to the name form. Setting it without passing `--template` still uses the built-in template.

A few properties worth knowing:

- **One tree wins — no layering.** The resolved tree is the whole content; it is never merged with the built-in one. Dropping `.mcp.json` from your copy means the provisioned workspace has no `.mcp.json`.
- **No fallback on failure.** If `--template` resolves to a directory that doesn't exist, `create` fails and tells you which path it looked at. It won't silently fall back to the built-in template and hand you a workspace that isn't what you asked for.
- **The directory you point at is the tree root.** Its relative paths are the workspace's relative paths — no extra `templates/` layer inside.
- **`*.tmpl` files are rendered** through Go's `text/template` (the suffix is dropped); everything else is copied verbatim. Available variables: `.Name`, `.RepoURL`, `.Branch`, `.Agent`, `.Model`, `.Source`, `.Label`, `.Instance`, `.TicketsPath`. Referencing an unknown one is an error, not an empty string.
- **Your tree's contents aren't validated.** If it renders a `runtime.yaml` that doesn't match the schema — or omits it entirely — you'll find out at `gogents start`, not at `create`.

### Configuring manually

A workspace is just a directory with `.gogents/runtime.yaml`, so you can also write one by hand.

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
  type: "claude"
  model: "claude-sonnet-5"
```

**Local ticket source:**

```yaml
runtime_name: "my-worker"
project:
  repo:
    url: "https://github.com/<owner>/<repo>"
source:
  type: "local"
  path: "~/.gogents/tickets"    # ticket inbox; defaults to <GOGENTS_HOME>/tickets
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
2. Initialize workspace directory structure and register the runtime into the global registry (`<GOGENTS_HOME>/config.yaml`)
3. Enter the sync loop — poll → execute → verify → report → repeat
4. Shut down gracefully on `SIGTERM` / `SIGINT`

**Global home (`GOGENTS_HOME`)** — the global registry (`config.yaml`) and daemon logs (`logs/<workspace-hash>/`) live under a single global-home root, resolved as: `$GOGENTS_HOME` if set, else `$HOME/.gogents` (default). Set `GOGENTS_HOME` to keep independent registries/logs on one machine (e.g. dev vs prod), mirroring agent-manager's `AM_HOME`. The per-workspace `.gogents/` (runtime.yaml / state.yaml / tasks/) is unaffected.

`GOGENTS_HOME` and [`GOGENTS_TEMPLATES`](#custom-templates) are independent: the global home is a state directory for the registry and logs, and is never used as a template location.

### Observability

```bash
# List all registered runtimes (workspace / name / registered_at)
bin/gogents runtime list

# Live status of one runtime (runtime_state, PID, current task, last sync);
# reports "down" if the daemon PID is no longer alive
bin/gogents runtime status <workspace|name>

# Print a runtime's logs (daemon.jsonl / error.jsonl)
bin/gogents logs <workspace|name>

# Unregister a retired/test runtime from the global registry (config.yaml only;
# leaves the runtime's logs and workspace .gogents/ intact, does not stop the daemon)
bin/gogents runtime remove <workspace|name>
```

`runtime list` and `runtime status` accept an opt-in `--json` flag for a stable machine-readable contract (used by the am adapter): a runtime array or a single status object on stdout, and a structured `{"error":{"code","message"}}` object on stderr with a non-zero exit on failure. Timestamps are RFC3339; an empty `last_sync_at` is `""`. Without `--json` the text output is unchanged.

### Version

```bash
gogents version     # `gogents --version` is equivalent
```

```
gogents v0.1.0
commit:   1a2b3c4 (2026-07-17T08:30:00Z)
go:       go1.23.0
platform: darwin/arm64
```

Release binaries carry their tag, commit and build time, injected at build time — quote the output when reporting an issue. A binary built from source instead reports `dev` with the commit it was built from, suffixed `-dirty` if the work tree had uncommitted changes.

## agent-manager Integration

GoGents is the **execution plane** for [agent-manager](https://github.com/dccoding1118/agent-manager) (am), the local agent control plane (Tower). am produces tickets into its own inbox and points the worker's `source.path` at it; a GoGents worker daemon configured with `source.type: local` claims tickets matching its worker label whose `status` is `queued` (am uses `status` as its dispatch gate — `blocked` and other states are left untouched), executes them, runs the ticket's `verify` command, and writes the terminal `status` and `result` back to the ticket file. Communication is one-way (`am → gogents`): am queries live execution state via `gogents runtime status` / `gogents logs` rather than caching it. See `docs/requirements/reqs/R11-local-source.md` and `R12-runtime-observability.md`.

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
  provision/       # Workspace provisioning (template materialize)
templates/
  base/            # Built-in workspace template (embedded into the binary)
docs/
  designs/         # Architecture design documents (D01–D09)
  requirements/    # Requirement specifications (R01–R13)
  changes/         # Change proposals (CH-00x)
  tests/           # Unit test planning
```

## License

[MIT](LICENSE)
