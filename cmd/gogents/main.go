package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/dccoding/gogents/internal/agent"
	"github.com/dccoding/gogents/internal/daemon"
	"github.com/dccoding/gogents/internal/execution"
	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/gitops"
	"github.com/dccoding/gogents/internal/logger"
	"github.com/dccoding/gogents/internal/source"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Sub-command dispatch.
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gogents <command> [options]")
		fmt.Fprintln(os.Stderr, "commands: start")
		return 1
	}

	switch os.Args[1] {
	case "start":
		return runStart(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		return 1
	}
}

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

	agentExecutor, err := agent.NewAgentExecutorFromConfig(cfg, lg)
	if err != nil {
		lg.Error("config.agent_executor_dispatch_failed", "", err.Error(), nil)
		return 3
	}

	// Compose execution.TaskExecutor (D03 Stage 3).
	executor := execution.New(execution.Deps{
		Store:          foundation.NewTaskStore(paths),
		Paths:          paths,
		Config:         cfg,
		Logger:         lg,
		ContentFetcher: contentFetcher,
		Git:            gitops.NewTaskBranchRunner(),
		Agent:          agentExecutor,
		Feedback:       feedback,
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
