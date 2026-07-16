package daemon

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dccoding1118/gogents/internal/execution"
	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/internal/gitops"
	"github.com/dccoding1118/gogents/internal/logger"
	"github.com/dccoding1118/gogents/internal/source"
)

// Daemon is the top-level entry point for the daemon process (D02 §4.1).
// CLI parses flags and delegates to Run; Run blocks until exit.
type Daemon interface {
	Run(ctx context.Context, workspaceDir string) error
}

// Deps collects all dependencies injected into the Daemon.
type Deps struct {
	ConfigLoader foundation.ConfigLoader
	NewPaths     func(ws string) foundation.WorkspacePaths
	NewStore     func(paths foundation.WorkspacePaths) foundation.TaskStore
	Logger       logger.Logger
	Source       source.TaskSource
	SourceProbe  source.SourceProbe
	Feedback     source.FeedbackReporter // Stage 3: TaskCreated trigger from Syncer.
	Git          gitops.Runner
	Executor     execution.TaskExecutor
	AgentProbe   foundation.AgentProbe
	// StateStore persists state.yaml. Optional: when nil, Run creates its own
	// from paths.StateYaml(). Injecting the same instance the executor uses lets
	// runtime_state updates from D03 and D02 share one store.
	StateStore StateStore
	// GlobalConfig registers this runtime into ~/.gogents/config.yaml at startup
	// (R12). Optional: nil skips registration (tests never touch the real home).
	GlobalConfig foundation.GlobalConfigStore
}

type daemon struct {
	deps     Deps
	draining atomic.Bool
}

// New creates a Daemon with the given dependencies.
func New(deps Deps) Daemon {
	return &daemon{deps: deps}
}

// Run executes the daemon lifecycle (D02 §5.1): startup → mainLoop → shutdown.
// It blocks until the daemon exits. A non-nil return indicates a non-zero exit.
func (d *daemon) Run(ctx context.Context, workspaceDir string) error {
	lg := d.deps.Logger

	// B1.1 — Validate workspace path.
	info, err := os.Stat(workspaceDir)
	if err != nil || !info.IsDir() {
		msg := fmt.Sprintf("workspace path does not exist or is not a directory: %s", workspaceDir)
		fmt.Fprintln(os.Stderr, msg)
		return &ExitError{Code: 2, Msg: msg}
	}

	lg.Info("daemon.starting", "", "daemon starting", map[string]any{
		"workspace": workspaceDir,
	})

	// B1.2 — Build paths.
	paths := d.deps.NewPaths(workspaceDir)

	// B1.3 — Load config.
	cfg, err := d.deps.ConfigLoader.Load(workspaceDir)
	if err != nil {
		lg.Error("config.parse_error", "", "failed to load config", map[string]any{
			"reason": err.Error(),
		})
		return &ExitError{Code: 3, Msg: err.Error()}
	}
	lg.Info("config.loaded", "", "configuration loaded", map[string]any{
		"runtime_name": cfg.RuntimeName,
		"source_type":  cfg.Source.Type,
	})

	// Apply configured timezone process-wide so persisted timestamps and log
	// lines agree with the operator's chosen zone. Validation already happened
	// during ConfigLoader.Load; LoadTimezone error here would be a programmer
	// bug rather than user input.
	if loc, tzErr := foundation.LoadTimezone(cfg.Timezone); tzErr == nil {
		foundation.SetGlobalTimezone(loc)
		logger.SetTimezone(loc)
		lg.Info("config.timezone_applied", "", "timezone applied", map[string]any{
			"timezone": cfg.Timezone,
		})
	} else {
		lg.Error("config.timezone_invalid", "", "timezone parse failed after schema check", map[string]any{
			"timezone": cfg.Timezone,
			"reason":   tzErr.Error(),
		})
	}

	// B1.4 — Preflight (assemble composite probe).
	probe := &compositeProbe{
		sourceProbe: d.deps.SourceProbe,
		agentProbe:  d.deps.AgentProbe,
		gitProbe:    foundation.NewGitProbe(workspaceDir),
	}
	if pfErr := d.deps.ConfigLoader.Preflight(cfg, probe); pfErr != nil {
		lg.Error("config.preflight_failed", "", "preflight check failed", map[string]any{
			"reason": pfErr.Error(),
		})
		return &ExitError{Code: 4, Msg: pfErr.Error()}
	}
	lg.Info("config.preflight_ok", "", "preflight passed", nil)

	// B1.5 — Init workspace directories.
	if iErr := paths.Init(); iErr != nil {
		lg.Error("workspace.init_failed", "", "failed to initialize workspace", map[string]any{
			"reason": iErr.Error(),
		})
		return &ExitError{Code: 2, Msg: iErr.Error()}
	}

	// B1.6 — Prepare repo directory.
	repoDir, err := resolveRepoDir(workspaceDir, cfg.Project.Repo.URL)
	if err != nil {
		lg.Error("daemon.repo_invalid", "", "failed to resolve repo directory", map[string]any{
			"reason": err.Error(),
		})
		return &ExitError{Code: 5, Msg: err.Error()}
	}

	if rpErr := d.prepareRepo(ctx, repoDir, cfg.Project.Repo.URL, lg); rpErr != nil {
		return rpErr
	}

	lg.Info("workspace.initialized", "", "workspace ready", map[string]any{
		"repo_dir": repoDir,
	})

	// B1.6b — Register runtime into ~/.gogents/config.yaml (upsert by workspace
	// path). Non-fatal: config.yaml is an observability aid, not required to run.
	if d.deps.GlobalConfig != nil {
		reg := foundation.RuntimeRegistration{
			Name:         cfg.RuntimeName,
			Workspace:    workspaceDir,
			RegisteredAt: foundation.Now().Format(time.RFC3339),
		}
		if rErr := d.deps.GlobalConfig.Upsert(reg); rErr != nil {
			lg.Warning("config.register_failed", "", "failed to register runtime", map[string]any{
				"workspace": workspaceDir,
				"reason":    rErr.Error(),
			})
		} else {
			lg.Info("daemon.registered", "", "runtime registered", map[string]any{
				"workspace": workspaceDir,
				"name":      cfg.RuntimeName,
			})
		}
	}

	// B1.7 — Load / init state.yaml.
	stateStore := d.deps.StateStore
	if stateStore == nil {
		stateStore = NewStateStore(paths.StateYaml())
	}
	state, err := stateStore.Load()
	if err != nil {
		lg.Error("daemon.state_corrupt", "", "state.yaml parse error", map[string]any{
			"reason": err.Error(),
		})
		return &ExitError{Code: 7, Msg: err.Error()}
	}

	state.Daemon = DaemonState{
		PID:       os.Getpid(),
		StartedAt: foundation.Now(),
		Workspace: workspaceDir,
	}
	state.RuntimeState = foundation.RuntimeIdle
	if sErr := stateStore.Save(state); sErr != nil {
		lg.Error("daemon.state_save_failed", "", "failed to save initial state", map[string]any{
			"reason": sErr.Error(),
		})
		// Not fatal — continue.
	}

	// B1.8 — Restart recovery: reset all processing tasks to queued.
	store := d.deps.NewStore(paths)
	tasks, err := store.List()
	if err != nil {
		lg.Error("daemon.task_list_failed", "", "failed to list tasks for restart recovery", map[string]any{
			"reason": err.Error(),
		})
		// Not fatal — try to continue.
	} else {
		for _, t := range tasks {
			if t.Status == foundation.StatusProcessing {
				patch := foundation.TaskPatch{
					StartedAt: nil, // Clear started_at.
				}
				if _, tErr := store.Transition(t.ID, foundation.StatusQueued, patch); tErr != nil {
					lg.Error("daemon.restart_reset_failed", t.ID, "failed to reset processing task", map[string]any{
						"reason": tErr.Error(),
					})
					// Individual failure doesn't block others.
					continue
				}
				lg.Warning("daemon.restart_reset", t.ID, "reset processing task to queued on restart", map[string]any{
					"task_id": t.ID,
				})
			}
		}
	}

	// B1.9 — Emit daemon.started.
	lg.Info("daemon.started", "", "daemon started", map[string]any{
		"pid":          os.Getpid(),
		"workspace":    workspaceDir,
		"runtime_name": cfg.RuntimeName,
	})

	// B1.10 — Start signal watcher.
	sigCtx, sigCancel := context.WithCancel(ctx)
	defer sigCancel()
	go d.signalWatcher(sigCtx, sigCancel, lg)

	// B1.11 — Enter main loop.
	syncer := NewSyncer(store, d.deps.Source, d.deps.Feedback, lg, cfg)
	d.mainLoop(sigCtx, syncer, store, stateStore, lg, cfg)

	// Shutdown.
	lg.Info("daemon.stopped", "", "daemon stopped normally", nil)
	return nil
}

// mainLoop implements B2 (D02 §5.2).
//
// Round ordering: a sync round (and any task picked from it) runs immediately
// on entry — operators expect the daemon to start working as soon as preflight
// passes, not after sync_interval. The interval wait is performed AFTER each
// round completes, so the cadence between rounds equals sync_interval rather
// than (sync_interval - round duration).
func (d *daemon) mainLoop(ctx context.Context, syncer Syncer, store foundation.TaskStore, stateStore StateStore, lg logger.Logger, cfg foundation.Config) {
	interval := cfg.Source.SyncInterval
	if interval <= 0 {
		interval = 30 * time.Second // default
	}

	for {
		if d.draining.Load() {
			return
		}
		if ctx.Err() != nil {
			return
		}

		d.runRound(ctx, syncer, store, stateStore, lg)

		if d.draining.Load() {
			return
		}

		// Wait for sync interval or context cancellation before the next round.
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// runRound runs a single sync round and, if anything is queued, executes the
// next picked task. Returns to mainLoop after the round body so the loop's
// select handles the inter-round wait.
func (d *daemon) runRound(ctx context.Context, syncer Syncer, store foundation.TaskStore, stateStore StateStore, lg logger.Logger) {
	// Sync round.
	lg.Info("sync.started", "", "sync round started", nil)
	outcome, err := syncer.Tick(ctx)

	now := foundation.Now()
	if err != nil {
		lg.Error("sync.failed", "", "sync round failed", map[string]any{
			"reason": err.Error(),
		})
		if sErr := stateStore.UpdateSync(func(s *SyncState) {
			s.LastSyncAt = &now
			s.LastSyncStatus = "failed"
			s.LastSyncError = err.Error()
		}); sErr != nil {
			lg.Error("daemon.state_save_failed", "", "failed to save sync state", map[string]any{
				"reason": sErr.Error(),
			})
		}
		return
	}

	lg.Info("sync.completed", "", "sync round completed", map[string]any{
		"queried_total": outcome.QueriedTotal,
		"created":       len(outcome.Created),
		"updated":       len(outcome.Updated),
		"deleted":       len(outcome.Deleted),
		"unchanged":     outcome.Unchanged,
		"duration_ms":   outcome.DurationMs,
		"errors":        len(outcome.PerSourceErrors),
	})

	if sErr := stateStore.UpdateSync(func(s *SyncState) {
		s.LastSyncAt = &now
		s.LastSyncStatus = "success"
		s.LastSyncError = ""
	}); sErr != nil {
		lg.Error("daemon.state_save_failed", "", "failed to save sync state", map[string]any{
			"reason": sErr.Error(),
		})
	}

	if d.draining.Load() {
		return
	}

	// Pick the next queued task (lexicographically smallest ID).
	allTasks, lErr := store.List()
	if lErr != nil {
		lg.Error("daemon.task_list_failed", "", "failed to list tasks for execution", map[string]any{
			"reason": lErr.Error(),
		})
		return
	}

	pick := pickNextTask(allTasks)
	if pick == nil {
		setRuntimeState(stateStore, lg, foundation.RuntimeIdle)
		return
	}

	// Update state: current task + runtime_state=executing. Fine-grained
	// verifying/reporting are set by D03 through the injected setter.
	taskStartedAt := foundation.Now()
	if sErr := stateStore.UpdateCurrentTask(func(ct *CurrentTask) {
		ct.TaskID = pick.ID
		ct.StartedAt = &taskStartedAt
	}); sErr != nil {
		lg.Error("daemon.state_save_failed", "", "failed to save current task state", map[string]any{
			"reason": sErr.Error(),
		})
	}
	setRuntimeState(stateStore, lg, foundation.RuntimeExecuting)

	// Execute task.
	d.executeTask(ctx, pick, lg)

	// Clear current task in state.
	if sErr := stateStore.UpdateCurrentTask(func(ct *CurrentTask) {
		ct.TaskID = ""
		ct.StartedAt = nil
	}); sErr != nil {
		lg.Error("daemon.state_save_failed", "", "failed to clear current task state", map[string]any{
			"reason": sErr.Error(),
		})
	}

	// Converge runtime_state: failed if the task ended failed, else idle.
	ws := foundation.RuntimeIdle
	if finalTask, gErr := store.Get(pick.ID); gErr == nil && finalTask.Status == foundation.StatusFailed {
		ws = foundation.RuntimeFailed
	}
	setRuntimeState(stateStore, lg, ws)
}

// setRuntimeState writes runtime_state, logging (not failing) on error.
func setRuntimeState(stateStore StateStore, lg logger.Logger, ws foundation.RuntimeState) {
	if sErr := stateStore.SetRuntimeState(ws); sErr != nil {
		lg.Error("daemon.state_save_failed", "", "failed to save runtime_state", map[string]any{
			"runtime_state": string(ws),
			"reason":        sErr.Error(),
		})
	}
}

// executeTask runs the executor with panic recovery (D02 §6.2).
func (d *daemon) executeTask(ctx context.Context, task *foundation.Task, lg logger.Logger) {
	defer func() {
		if r := recover(); r != nil {
			lg.Error("task.executor_panic", task.ID, "executor panicked", map[string]any{
				"panic": fmt.Sprintf("%v", r),
			})
		}
	}()

	if err := d.deps.Executor.Run(ctx, *task); err != nil {
		lg.Error("task.executor_error", task.ID, "executor returned error", map[string]any{
			"reason": err.Error(),
		})
	}
}

// signalWatcher implements B4 (D02 §5.4).
func (d *daemon) signalWatcher(ctx context.Context, cancel context.CancelFunc, lg logger.Logger) {
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	select {
	case sig := <-sigCh:
		// First signal: set draining, cancel context.
		d.draining.Store(true)
		lg.Info("daemon.stopping", "", "received signal, draining", map[string]any{
			"signal": sig.String(),
		})
		cancel()
	case <-ctx.Done():
		return
	}

	// Wait for second signal or context done.
	select {
	case <-sigCh:
		// Second signal: force exit.
		lg.Error("daemon.force_stopped", "", "received second signal, force exiting", nil)
		os.Exit(1)
	case <-ctx.Done():
		return
	}
}

// prepareRepo handles B1.6: clone if missing, fetch if exists, reject if not git.
func (d *daemon) prepareRepo(ctx context.Context, repoDir, repoURL string, lg logger.Logger) error {
	info, err := os.Stat(repoDir)
	if os.IsNotExist(err) {
		// Clone.
		lg.Info("daemon.git_clone", "", "cloning repository", map[string]any{
			"url": repoURL,
			"dir": repoDir,
		})
		if cErr := d.deps.Git.Clone(ctx, repoURL, repoDir); cErr != nil {
			lg.Error("daemon.git_init_failed", "", "git clone failed", map[string]any{
				"command": "git clone",
				"reason":  cErr.Error(),
			})
			return &ExitError{Code: 6, Msg: cErr.Error()}
		}
		return nil
	}
	if err != nil {
		return &ExitError{Code: 5, Msg: fmt.Sprintf("failed to stat repo dir: %v", err)}
	}
	if !info.IsDir() {
		return &ExitError{Code: 5, Msg: fmt.Sprintf("repo path is not a directory: %s", repoDir)}
	}

	// Check if it's a git repo.
	if !gitops.IsGitRepo(repoDir) {
		lg.Error("daemon.repo_invalid", "", "repo directory is not a git repository", map[string]any{
			"dir": repoDir,
		})
		return &ExitError{Code: 5, Msg: fmt.Sprintf("not a git repository: %s", repoDir)}
	}

	// Fetch.
	lg.Info("daemon.git_fetch", "", "fetching from origin", map[string]any{
		"dir": repoDir,
	})
	if fErr := d.deps.Git.Fetch(ctx, repoDir); fErr != nil {
		lg.Error("daemon.git_init_failed", "", "git fetch failed", map[string]any{
			"command": "git fetch origin",
			"reason":  fErr.Error(),
		})
		return &ExitError{Code: 6, Msg: fErr.Error()}
	}
	return nil
}

// resolveRepoDir derives the local repo directory path from workspace and repo URL.
func resolveRepoDir(workspaceDir, repoURL string) (string, error) {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse repo URL %q: %w", repoURL, err)
	}
	repoName := filepath.Base(strings.TrimSuffix(parsed.Path, ".git"))
	if repoName == "" || repoName == "." || repoName == "/" {
		return "", fmt.Errorf("cannot derive repo name from URL %q", repoURL)
	}
	return filepath.Join(workspaceDir, repoName), nil
}

// compositeProbe adapts source/agent probes to the D01 ToolchainProbe interface.
type compositeProbe struct {
	sourceProbe source.SourceProbe
	agentProbe  foundation.AgentProbe
	gitProbe    foundation.ToolchainProbe
}

func (p *compositeProbe) CheckSource(_ foundation.SourceSpec) error {
	return p.sourceProbe.CheckSource()
}

func (p *compositeProbe) CheckAgent(spec foundation.AgentSpec) error {
	return p.agentProbe.CheckAgent(spec)
}

func (p *compositeProbe) CheckGit() error {
	return p.gitProbe.CheckGit()
}

// ExitError carries a specific exit code for the daemon process.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit %d: %s", e.Code, e.Msg)
}
