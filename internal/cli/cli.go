// Package cli is the command layer for the gogents binary. It owns argument
// parsing, wiring (composition root for `start`), and terminal output for every
// subcommand. cmd/gogents/main.go is a thin entry point that calls Dispatch.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dccoding/gogents/internal/agent"
	"github.com/dccoding/gogents/internal/daemon"
	"github.com/dccoding/gogents/internal/execution"
	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/gitops"
	"github.com/dccoding/gogents/internal/logger"
	"github.com/dccoding/gogents/internal/source"
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

// runRuntime handles `gogents runtime list` and `gogents runtime status <target>`.
func runRuntime(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gogents runtime <list|status> [target]")
		return 1
	}
	gcfg := foundation.NewGlobalConfigStore()

	switch args[0] {
	case "list":
		views, err := List(gcfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
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
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: gogents runtime status <workspace|name>")
			return 1
		}
		view, err := Status(gcfg, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Printf("name:          %s\n", view.Name)
		fmt.Printf("workspace:     %s\n", view.Workspace)
		fmt.Printf("runtime_state: %s\n", view.RuntimeState)
		fmt.Printf("pid:           %d (alive=%t)\n", view.Pid, view.Alive)
		fmt.Printf("current_task:  %s\n", view.CurrentTask)
		fmt.Printf("last_sync:     %s (%s)\n", view.LastSyncAt.Format("2006-01-02 15:04:05 -07:00"), view.LastSyncStatus)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown runtime subcommand: %s\n", args[0])
		return 1
	}
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
