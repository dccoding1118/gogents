// Package cli is the command layer for the gogents binary. It owns argument
// parsing, wiring (composition root for `start`), and terminal output for every
// subcommand. cmd/gogents/main.go is a thin entry point that calls Dispatch.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dccoding1118/gogents/internal/agent"
	"github.com/dccoding1118/gogents/internal/daemon"
	"github.com/dccoding1118/gogents/internal/execution"
	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/internal/gitops"
	"github.com/dccoding1118/gogents/internal/logger"
	"github.com/dccoding1118/gogents/internal/source"
)

// Dispatch routes args (os.Args[1:]) to a subcommand handler and returns the
// process exit code.
func Dispatch(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gogents <command> [options]")
		fmt.Fprintln(os.Stderr, "commands: start, runtime, logs")
		return 1
	}

	switch args[0] {
	case "start":
		return runStart(args[1:])
	case "runtime":
		return runRuntime(args[1:])
	case "logs":
		return runLogs(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		return 1
	}
}

// runRuntime handles `gogents runtime create <name>`, `gogents runtime list`,
// `gogents runtime status <target>`, and `gogents runtime remove <target>`.
// list/status accept an opt-in `--json` flag (position-tolerant, D08 §3.4);
// create and remove do not.
func runRuntime(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gogents runtime <create|list|status|remove> [target] [--json]")
		return 1
	}
	sub := args[0]

	// create owns its own flag set and takes no --json; route before the flag is
	// stripped so `create --json` reports an unknown flag rather than ignoring it.
	if sub == "create" {
		return runCreate(args[1:])
	}

	rest, jsonOut := extractBoolFlag(args[1:], "json")
	gcfg := foundation.NewGlobalConfigStore()

	switch sub {
	case "list":
		views, err := List(gcfg)
		if err != nil {
			return emitRuntimeError(jsonOut, err)
		}
		if jsonOut {
			return printJSON(views)
		}
		if len(views) == 0 {
			fmt.Println("(no registered runtimes)")
			return 0
		}
		for _, v := range views {
			fmt.Printf("%s\t%s\t%s\n", v.Name, v.Workspace, v.RegisteredAt)
		}
		return 0

	case "status":
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "usage: gogents runtime status <workspace|name> [--json]")
			return 1
		}
		view, err := Status(gcfg, rest[0])
		if err != nil {
			return emitRuntimeError(jsonOut, err)
		}
		if jsonOut {
			return printJSON(view)
		}
		fmt.Printf("name:          %s\n", view.Name)
		fmt.Printf("workspace:     %s\n", view.Workspace)
		fmt.Printf("runtime_state: %s\n", view.RuntimeState)
		fmt.Printf("pid:           %d (alive=%t)\n", view.Pid, view.Alive)
		fmt.Printf("current_task:  %s\n", view.CurrentTask)
		fmt.Printf("last_sync:     %s (%s)\n", view.LastSyncAt.Format("2006-01-02 15:04:05 -07:00"), view.LastSyncStatus)
		return 0

	case "remove":
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "usage: gogents runtime remove <workspace|name>")
			return 1
		}
		if err := Remove(gcfg, rest[0]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Printf("removed runtime %q from registry\n", rest[0])
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown runtime subcommand: %s\n", sub)
		return 1
	}
}

// Remove unregisters the target runtime from config.yaml (R12 / D08 B7). It only
// touches the registry — not the runtime's logs, its workspace .gogents/, or its
// daemon. Returns foundation.ErrRuntimeNotRegistered when the target is unknown.
func Remove(gcfg foundation.GlobalConfigStore, target string) error {
	return gcfg.Remove(target)
}

// extractBoolFlag removes every occurrence of --name/-name from args and reports
// whether it was present. Unlike the flag package it tolerates the flag in any
// position (before or after positional args), so `runtime status <t> --json` and
// `runtime status --json <t>` both work (D08 §3.4).
func extractBoolFlag(args []string, name string) (remaining []string, present bool) {
	long, short := "--"+name, "-"+name
	remaining = make([]string, 0, len(args))
	for _, a := range args {
		if a == long || a == short {
			present = true
			continue
		}
		remaining = append(remaining, a)
	}
	return remaining, present
}

// printJSON writes v as compact JSON to stdout and returns exit 0.
func printJSON(v any) int {
	enc, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Println(string(enc))
	return 0
}

// emitRuntimeError renders a query failure. In --json mode it writes a single
// {"error":{"code","message"}} object to stderr with a classified code (D08
// §3.4); otherwise the existing `error: …` text. Exit is non-zero either way.
func emitRuntimeError(jsonOut bool, err error) int {
	if !jsonOut {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	enc, mErr := json.Marshal(newRuntimeErrorEnvelope(err))
	if mErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, string(enc))
	return 1
}

// runtimeErrorEnvelope is the --json failure contract (D08 §3.4).
type runtimeErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// newRuntimeErrorEnvelope classifies err into a stable code: runtime_not_found
// (target not registered), state_not_found (registered but no state.yaml), or
// internal (everything else).
func newRuntimeErrorEnvelope(err error) runtimeErrorEnvelope {
	code := "internal"
	switch {
	case errors.Is(err, foundation.ErrRuntimeNotRegistered):
		code = "runtime_not_found"
	case errors.Is(err, ErrStateNotFound):
		code = "state_not_found"
	}
	var env runtimeErrorEnvelope
	env.Error.Code = code
	env.Error.Message = err.Error()
	return env
}

// runLogs handles `gogents logs <target> [--error]`.
func runLogs(args []string) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	errOnly := fs.Bool("error", false, "print error.jsonl instead of daemon.jsonl")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: gogents logs <workspace|name> [--error]")
		return 1
	}

	gcfg := foundation.NewGlobalConfigStore()
	logPath, err := Logs(gcfg, fs.Arg(0), *errOnly)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	f, err := os.Open(logPath) //nolint:gosec // path resolved from the registry
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(os.Stdout, f); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// runStart handles `gogents start --workspace <path>`: it is the composition
// root that wires every module together and runs the daemon lifecycle.
func runStart(args []string) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	workspace := fs.String("workspace", "", "workspace root directory (required)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if *workspace == "" {
		fmt.Fprintln(os.Stderr, "error: --workspace is required")
		return 1
	}

	// Build paths early (for logger).
	paths := foundation.NewWorkspacePaths(*workspace)
	if iErr := paths.Init(); iErr != nil {
		fmt.Fprintf(os.Stderr, "error: failed to init workspace paths: %v\n", iErr)
		return 2
	}

	// Open logger.
	lg, err := logger.Open(paths.DaemonLog(), paths.ErrorLog())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to open logger: %v\n", err)
		return 2
	}
	defer func() {
		if cErr := lg.Close(); cErr != nil {
			fmt.Fprintf(os.Stderr, "error: failed to close logger: %v\n", cErr)
		}
	}()

	// Load config to determine source type for dispatcher.
	cfgLoader := foundation.NewConfigLoader(paths)
	cfg, err := cfgLoader.Load(*workspace)
	if err != nil {
		lg.Error("config.parse_error", "", "failed to load config for dispatcher", map[string]any{
			"reason": err.Error(),
		})
		return 3
	}

	// Dispatch source.
	src, err := source.NewTaskSourceFromConfig(cfg, lg)
	if err != nil {
		lg.Error("config.source_dispatch_failed", "", err.Error(), nil)
		return 3
	}

	srcProbe, err := source.NewSourceProbeFromConfig(cfg, lg)
	if err != nil {
		lg.Error("config.source_probe_dispatch_failed", "", err.Error(), nil)
		return 3
	}

	agentProbe, err := agent.NewAgentProbeFromConfig(cfg, lg)
	if err != nil {
		lg.Error("config.agent_probe_dispatch_failed", "", err.Error(), nil)
		return 3
	}

	feedback, err := source.NewFeedbackReporterFromConfig(cfg, lg)
	if err != nil {
		lg.Error("config.feedback_dispatch_failed", "", err.Error(), nil)
		return 3
	}

	contentFetcher, err := source.NewTaskContentFetcherFromConfig(cfg, lg)
	if err != nil {
		lg.Error("config.content_fetcher_dispatch_failed", "", err.Error(), nil)
		return 3
	}

	verifier, err := source.NewTaskVerifierFromConfig(cfg, lg)
	if err != nil {
		lg.Error("config.verifier_dispatch_failed", "", err.Error(), nil)
		return 3
	}

	agentExecutor, err := agent.NewAgentExecutorFromConfig(cfg, lg)
	if err != nil {
		lg.Error("config.agent_executor_dispatch_failed", "", err.Error(), nil)
		return 3
	}

	// Shared state store: the executor and the daemon both write runtime_state
	// through this single instance (D03 fine-grained phases, D02 coarse ones).
	stateStore := daemon.NewStateStore(paths.StateYaml())

	// Compose execution.TaskExecutor (D03 Stage 3).
	executor := execution.New(execution.Deps{
		Store:           foundation.NewTaskStore(paths),
		Paths:           paths,
		Config:          cfg,
		Logger:          lg,
		ContentFetcher:  contentFetcher,
		Verifier:        verifier,
		Git:             gitops.NewTaskBranchRunner(),
		Agent:           agentExecutor,
		Feedback:        feedback,
		SetRuntimeState: func(rs foundation.RuntimeState) { _ = stateStore.SetRuntimeState(rs) },
	})

	// Assemble Deps.
	deps := daemon.Deps{
		ConfigLoader: cfgLoader,
		NewPaths:     foundation.NewWorkspacePaths,
		NewStore:     foundation.NewTaskStore,
		Logger:       lg,
		Source:       src,
		SourceProbe:  srcProbe,
		Feedback:     feedback,
		Git:          gitops.NewRunner(),
		Executor:     executor,
		AgentProbe:   agentProbe,
		StateStore:   stateStore,
		GlobalConfig: foundation.NewGlobalConfigStore(),
	}

	d := daemon.New(deps)
	ctx := context.Background()

	if dErr := d.Run(ctx, *workspace); dErr != nil {
		var exitErr *daemon.ExitError
		if errors.As(dErr, &exitErr) {
			return exitErr.Code
		}
		return 1
	}

	return 0
}
