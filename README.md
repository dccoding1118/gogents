# GoGents

[![CI](https://github.com/dccoding1118/gogents/actions/workflows/ci.yml/badge.svg)](https://github.com/dccoding1118/gogents/actions/workflows/ci.yml)
[![CodeQL](https://github.com/dccoding1118/gogents/actions/workflows/codeql.yml/badge.svg)](https://github.com/dccoding1118/gogents/actions/workflows/codeql.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/dccoding1118/gogents)](https://goreportcard.com/report/github.com/dccoding1118/gogents)
[![Go Reference](https://pkg.go.dev/badge/github.com/dccoding1118/gogents.svg)](https://pkg.go.dev/github.com/dccoding1118/gogents)
[![Latest release](https://img.shields.io/github/v/release/dccoding1118/gogents)](https://github.com/dccoding1118/gogents/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**GoGents** is an automation daemon, written in Go, that works a queue of coding tasks unattended. It continuously pulls assigned tasks from a configured source, runs an LLM coding agent ([Claude Code](https://docs.anthropic.com/en/docs/claude-code)) against them inside a prepared workspace, verifies the outcome, and reports back to wherever the task came from.

It runs standalone on your own machine: point it at a Git repository and a task source, start the daemon, and it syncs → executes → verifies → reports on a loop until you stop it.

## Why GoGents

- **Unattended throughput** — a long-running daemon drains a task queue on its own: it branches, runs the agent, judges the result, and reports back without a human in the loop.
- **Bring your own workflow** — a workspace carries its own conventions, skills, and MCP servers (`.claude/`, `.mcp.json`). Capture a specialty once as a template and provision it repeatedly.
- **Two sources, one pipeline** — feed it from GitHub Issues or from a local ticket inbox that any tool can write to; both share the same task store, state machine, and execution flow.
- **Auditable by design** — a global registry tracks every runtime, and structured JSONL logs record every event, so you can inspect live state and reconstruct history after the fact.

## How It Works

For each task, the daemon runs a fixed cycle:

1. **Sync** — polls the configured source and claims tasks that match its filter into a shared task store.
2. **Prepare** — fetches the task's content, writes it to `input.md`, and cuts a fresh Git branch.
3. **Execute** — invokes the agent with the **workspace root** as its working directory, so workspace-level `.claude/` and `.mcp.json` apply.
4. **Verify** — runs the task's declared `verify` command (or falls back to the agent's `result.json` verdict) to judge pass or fail.
5. **Report** — writes the outcome back to the originating source — a GitHub comment, PR link, and `gogents:*` label, or the ticket file's terminal `status` and `result`.
6. **Repeat** on the sync interval, and shut down gracefully on `SIGTERM` / `SIGINT`.

```
 source ──▶ sync ──▶ task store ──▶ run agent ──▶ verify ──▶ report ──┐
    ▲                                                                 │
    └─────────────────────────── feedback ────────────────────────────┘
```

## Task Sources

Two source types feed the one pipeline; a runtime picks one via `source.type`.

- **`github_issue`** — polls GitHub Issues (via the `gh` CLI) by label. Feedback goes back as issue comments, PR links, and `gogents:*` labels.
- **`local`** — reads a ticket inbox on disk (`<GOGENTS_HOME>/tickets/` by default), claiming tickets that match the worker label **and** carry `status: queued`. On completion it writes the terminal `status` and `result` back to the ticket file.

Any tool that writes the ticket format is a **producer** — GoGents does not care which one. [agent-manager](https://github.com/dccoding1118/agent-manager) is one such producer: it queues tickets into an inbox, points a worker's `source.path` at it, and later reads live execution state via `gogents runtime status` / `gogents logs` (see [Managing Runtimes](#managing-runtimes)).

## Features

- **One-Command Workspace Provisioning** — `gogents runtime create <name>` materializes a ready-to-start workspace (runtime.yaml + agent runtime elements) from a template embedded in the binary, so a downloaded binary needs no external files. Point `--template` at your own tree to provision a specialty repeatedly. It never deletes anything under your directory, and asks before writing into a non-empty one.
- **Pluggable Task Sources** — `github_issue` and `local` share one task store, state machine, and execution pipeline; a runtime selects one via `source.type` (see [Task Sources](#task-sources)).
- **Pluggable Agent Architecture** — agents sit behind a simple probe/executor interface. [Claude Code](https://docs.anthropic.com/en/docs/claude-code) is the officially supported agent; new agents can be added by implementing the interface.
- **Ticket-Declared Verification** — a `local` ticket may declare a `verify` command; the daemon runs it in the workspace and uses its exit code to judge pass/fail, falling back to the `result.json` coding verdict when unspecified.
- **Graceful Lifecycle Management** — a signal-aware daemon with startup validation (preflight checks), crash recovery (requeues interrupted tasks), and graceful shutdown.
- **Structured Logging** — JSONL-formatted logs with Asia/Taipei timezone, split into `daemon.jsonl` (all events) and `error.jsonl` (errors only).
- **Runtime Registration & Observability** — `start` registers the runtime into `<GOGENTS_HOME>/config.yaml` (keyed by workspace path); `gogents runtime list/status` and `gogents logs` expose live `runtime_state`, PID, and logs for auditing.

## Prerequisites

gogents runs on **linux** and **macOS** (`amd64` / `arm64`). Windows is not supported — the daemon's process-liveness probe relies on POSIX signals.

To run gogents:

- **[git](https://git-scm.com/)**
- **[gh](https://cli.github.com/)** — GitHub CLI (authenticated via `gh auth login`)
- **[Claude Code](https://docs.anthropic.com/en/docs/claude-code)** (`claude`) — the coding agent CLI

To build it from source, additionally:

- **Go** 1.23+
- **[mise](https://mise.jdx.dev/)** — task runner & toolchain manager

## Install

### From a release

Install the latest release onto your `PATH` with one command:

```bash
curl -fsSL https://raw.githubusercontent.com/dccoding1118/gogents/main/install.sh | sh
```

The script detects your OS and architecture, downloads the matching archive from [Releases](https://github.com/dccoding1118/gogents/releases), verifies its checksum, and installs the binary to `/usr/local/bin`. Override the destination with `INSTALL_DIR` and pin a version with `VERSION`:

```bash
curl -fsSL https://raw.githubusercontent.com/dccoding1118/gogents/main/install.sh \
    | VERSION=v0.1.0 INSTALL_DIR="$HOME/.local/bin" sh
```

<details>
<summary>Or download and install manually</summary>

```bash
VERSION=v0.1.0
OS=$(uname -s | tr '[:upper:]' '[:lower:]')   # linux | darwin
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

curl -fsSL -o gogents.tar.gz \
    "https://github.com/dccoding1118/gogents/releases/download/${VERSION}/gogents_${VERSION}_${OS}_${ARCH}.tar.gz"
tar -xzf gogents.tar.gz
sudo install gogents_${VERSION}_${OS}_${ARCH}/gogents /usr/local/bin/
```

</details>

Archives are published for **linux** and **macOS** on `amd64` and `arm64`; the installer verifies the download against the release's `checksums.txt`.

The binary is self-contained: the workspace template is compiled into it, so `runtime create` works with nothing else installed. Each archive also carries a plain copy of `templates/` for reading and copying (see [Custom templates](#custom-templates)) and the `LICENSE`.

### From source

```bash
git clone https://github.com/dccoding1118/gogents.git
cd gogents
mise install       # installs Go, golangci-lint, gofumpt
mise run build     # output: bin/gogents
```

## Getting Started

### 1. Create a runtime

`runtime create` provisions a ready-to-start workspace from the built-in template — `.gogents/runtime.yaml` plus the agent runtime elements (`.claude/CLAUDE.md`, `.claude/skills/`, `.mcp.json`):

```bash
# Local ticket source
gogents runtime create my-worker \
    --repo https://github.com/<owner>/<repo> \
    --agent claude --model claude-sonnet-5 \
    --source local

# GitHub Issue source
gogents runtime create my-workspace \
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

### 2. Run

```bash
gogents start --workspace /path/to/workspace
```

The daemon will:
1. Validate config & run preflight checks (git, source toolchain, agent CLI)
2. Initialize the workspace directory structure and register the runtime into the global registry (`<GOGENTS_HOME>/config.yaml`)
3. Enter the sync loop — poll → execute → verify → report → repeat
4. Shut down gracefully on `SIGTERM` / `SIGINT`

**Global home (`GOGENTS_HOME`)** — the global registry (`config.yaml`) and daemon logs (`logs/<workspace-hash>/`) live under a single global-home root, resolved as `$GOGENTS_HOME` if set, else `$HOME/.gogents` (default). Set `GOGENTS_HOME` to keep independent registries/logs on one machine (e.g. dev vs prod). The per-workspace `.gogents/` (runtime.yaml / state.yaml / tasks/) is unaffected.

`GOGENTS_HOME` and [`GOGENTS_TEMPLATES`](#custom-templates) are independent: the global home is a state directory for the registry and logs, and is never used as a template location.

### Custom templates

The built-in template is a generic starting point. To turn a specialty — working conventions, skills, a set of MCP servers — into something you can provision repeatedly, copy `templates/base/` (shipped in the release archive and in this repo), edit it, and point `--template` at it:

```bash
cp -r templates/base ./my-tpl
$EDITOR ./my-tpl/.claude/CLAUDE.md

gogents runtime create my-worker --template ./my-tpl \
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
gogents runtime create my-worker --template rust-svc --repo … --source local
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

## Managing Runtimes

```bash
# List all registered runtimes (workspace / name / registered_at)
gogents runtime list

# Live status of one runtime (runtime_state, PID, current task, last sync);
# reports "down" if the daemon PID is no longer alive
gogents runtime status <workspace|name>

# Print a runtime's logs (daemon.jsonl / error.jsonl)
gogents logs <workspace|name>

# Unregister a retired/test runtime from the global registry (config.yaml only;
# leaves the runtime's logs and workspace .gogents/ intact, does not stop the daemon)
gogents runtime remove <workspace|name>
```

`runtime list` and `runtime status` accept an opt-in `--json` flag for a stable machine-readable contract, consumed by external tooling ([agent-manager](https://github.com/dccoding1118/agent-manager) among them): a runtime array or a single status object on stdout, and a structured `{"error":{"code","message"}}` object on stderr with a non-zero exit on failure. Timestamps are RFC3339; an empty `last_sync_at` is `""`. Without `--json` the text output is unchanged.

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

## Contributing

GoGents is a personal tool, developed in the open as a reference. It is not accepting external pull requests, but you are welcome to fork it and take it in your own direction. Development conventions, the module map, and architecture live in [AGENTS.md](AGENTS.md).

## License

[MIT](LICENSE)
