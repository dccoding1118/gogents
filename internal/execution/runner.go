package execution

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dccoding1118/gogents/internal/agent"
	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/internal/gitops"
	"github.com/dccoding1118/gogents/internal/logger"
	"github.com/dccoding1118/gogents/internal/source"
)

// Deps wires the dependencies for the real TaskExecutor (D03 §4.2).
// All fields must be non-nil; nil is treated as a programmer error and is
// the responsibility of the CLI startup code, not validated here.
type Deps struct {
	Store           foundation.TaskStore
	Paths           foundation.WorkspacePaths
	Config          foundation.Config
	Logger          logger.Logger
	ContentFetcher  source.TaskContentFetcher
	Verifier        source.TaskVerifier // ticket-declared verify (github: ok=false)
	Git             gitops.TaskBranchRunner
	Agent           agent.AgentExecutor
	Feedback        source.FeedbackReporter
	SetRuntimeState func(foundation.RuntimeState) // injected by D02; nil in tests/noop
}

// New returns the production TaskExecutor. The Stage 2 noop variant remains
// available via NewNoopExecutor for tests / scenarios where execution must
// be inert.
func New(deps Deps) TaskExecutor {
	repoDir := deriveRepoDir(deps.Paths.Root(), deps.Config.Project.Repo.URL)
	return &runner{
		deps:     deps,
		repoDir:  repoDir,
		reponame: filepath.Base(repoDir),
	}
}

type runner struct {
	deps     Deps
	repoDir  string
	reponame string
}

// setRuntimeState updates the daemon's runtime_state via the injected setter,
// which writes state.yaml (owned by D02). No-op when unset (tests / noop).
func (r *runner) setRuntimeState(ws foundation.RuntimeState) {
	if r.deps.SetRuntimeState != nil {
		r.deps.SetRuntimeState(ws)
	}
}

func (r *runner) Run(ctx context.Context, task foundation.Task) error {
	lg := r.deps.Logger
	store := r.deps.Store
	cfg := r.deps.Config

	// Step 0: invariant check (B11).
	if task.Status != foundation.StatusQueued {
		lg.Error("task.input_invariant_violated", task.ID,
			"task entered executor with unexpected status",
			map[string]any{
				"task_id":    task.ID,
				"got_status": string(task.Status),
			})
		return fmt.Errorf("task %s entered executor with status=%s", task.ID, task.Status)
	}

	// Step 1: queued → processing.
	branchName := "task/" + task.ID
	now := foundation.Now()
	if _, err := store.Transition(task.ID, foundation.StatusProcessing, foundation.TaskPatch{
		StartedAt: &now,
		Branch:    &branchName,
	}); err != nil {
		// IO or illegal-transition: surface to D02 (B1 ii / iii).
		return err
	}
	lg.Info("task.started", task.ID, "task transitioned to processing", map[string]any{
		"task_id": task.ID,
		"branch":  branchName,
	})

	// Notify GitHub that this task is now being worked on (label → gogents:processing,
	// comment updated). Failures are logged but do not block execution: the local
	// task already moved to processing, and the agent run is the substantive work.
	r.notifyProcessingStarted(ctx, task)

	base := "origin/" + cfg.Project.Repo.DefaultBranch
	taskDir := r.deps.Paths.TaskDir(task.ID)

	// Step 2: fetch content (issue body + comments).
	content, err := r.deps.ContentFetcher.Fetch(ctx, task.ID)
	switch {
	case errors.Is(err, source.ErrSourceNotFound):
		if _, tErr := store.Transition(task.ID, foundation.StatusDeleted, foundation.TaskPatch{}); tErr != nil {
			return tErr
		}
		lg.Info("task.deleted_during_exec", task.ID, "issue deleted while task in flight", map[string]any{
			"task_id": task.ID,
		})
		return nil
	case err != nil:
		if isCtxErr(ctx, err) {
			lg.Warning("task.canceled", task.ID, "task canceled during content fetch", map[string]any{
				"task_id": task.ID,
				"step":    "content",
			})
			return ctx.Err()
		}
		reason := classifySourceErr(err)
		lg.Warning("task.content_fetch_failed", task.ID, "content fetch failed", map[string]any{
			"task_id": task.ID,
			"reason":  reason,
		})
		r.failAndReport(ctx, task, "content_fetch: "+reason, -1)
		return nil
	}
	lg.Info("task.content_fetched", task.ID, "content fetched", map[string]any{
		"task_id":       task.ID,
		"comment_count": len(content.Comments),
	})

	// Step 3: write input.md.
	inputBytes := renderInputMd(content, taskDir, r.reponame)
	inputPath := r.deps.Paths.InputMd(task.ID)
	if wErr := os.WriteFile(inputPath, inputBytes, 0o644); wErr != nil { //nolint:gosec // workspace path
		lg.Warning("task.input_write_failed", task.ID, "failed to write input.md", map[string]any{
			"task_id": task.ID,
			"reason":  wErr.Error(),
		})
		r.failAndReport(ctx, task, "input_write: "+wErr.Error(), -1)
		return nil
	}
	lg.Info("task.input_prepared", task.ID, "input.md ready", map[string]any{
		"task_id": task.ID,
		"bytes":   len(inputBytes),
	})

	// Step 4: git fetch + checkout -B task/<id> origin/<base>.
	if fErr := r.deps.Git.Fetch(ctx, r.repoDir); fErr != nil {
		if isCtxErr(ctx, fErr) {
			lg.Warning("task.canceled", task.ID, "task canceled during git fetch", map[string]any{
				"task_id": task.ID, "step": "git",
			})
			return ctx.Err()
		}
		lg.Warning("task.git_failed", task.ID, "git fetch failed", map[string]any{
			"task_id": task.ID,
			"command": "fetch",
			"stderr":  truncate(fErr.Error(), 1024),
		})
		r.failAndReport(ctx, task, "git_fetch: "+truncate(fErr.Error(), 512), -1)
		return nil
	}
	if cErr := r.deps.Git.CheckoutBranch(ctx, r.repoDir, branchName, base); cErr != nil {
		if isCtxErr(ctx, cErr) {
			lg.Warning("task.canceled", task.ID, "task canceled during git checkout", map[string]any{
				"task_id": task.ID, "step": "git",
			})
			return ctx.Err()
		}
		lg.Warning("task.git_failed", task.ID, "git checkout failed", map[string]any{
			"task_id": task.ID,
			"command": "checkout",
			"stderr":  truncate(cErr.Error(), 1024),
		})
		r.failAndReport(ctx, task, "git_checkout: "+truncate(cErr.Error(), 512), -1)
		return nil
	}
	lg.Info("task.git_prepared", task.ID, "branch prepared", map[string]any{
		"task_id": task.ID,
		"branch":  branchName,
		"base":    base,
	})

	// Step 5: invoke agent. WorkDir is the workspace root (not the repo dir) so
	// workspace-level .claude/ and .mcp.json apply; the repo is ./<reponame>.
	res, agErr := r.deps.Agent.Run(ctx, agent.RunRequest{
		TaskID:     task.ID,
		WorkDir:    r.deps.Paths.Root(),
		InputPath:  inputPath,
		OutputPath: r.deps.Paths.OutputJsonl(task.ID),
		Model:      cfg.Agent.Model,
	})
	if agErr != nil {
		if errors.Is(agErr, context.Canceled) || errors.Is(agErr, context.DeadlineExceeded) {
			lg.Warning("task.canceled", task.ID, "task canceled during agent run", map[string]any{
				"task_id": task.ID, "step": "agent",
			})
			return agErr
		}
		// ErrAgentSpawn / ErrAgentIO — D06 already emitted agent.spawn_failed.
		r.failAndReport(ctx, task, "agent_spawn: "+agErr.Error(), -1)
		return nil
	}

	// Step 5.5: verify (when the ticket declares verify.cmd; otherwise fall back
	// to the result.json verdict in Step 6).
	if v, ok := r.verifyVerdict(ctx, task, res.ExitCode); ok {
		if v.Case == verdictCanceled {
			return ctx.Err()
		}
		r.finalize(ctx, task, v.verdict, res.ExitCode)
		return nil
	}

	// Step 6: judge result.json + finalize.
	verdict := judge(r.deps.Paths.ResultJson(task.ID))
	r.finalize(ctx, task, verdict, res.ExitCode)
	return nil
}

// verifyStep carries the outcome of Step 5.5. Case==verdictCanceled signals the
// caller to return ctx.Err(); otherwise verdict is ready for finalize.
type verifyStep struct {
	verdict Verdict
	Case    int
}

const verdictCanceled = 1

// verifyVerdict runs the ticket-declared verify command (if any) in the
// workspace root and derives a coding verdict from its exit code. Returns
// ok=false when no verify is declared (Step 6 fallback path).
func (r *runner) verifyVerdict(ctx context.Context, task foundation.Task, _ int) (verifyStep, bool) {
	lg := r.deps.Logger
	if r.deps.Verifier == nil {
		return verifyStep{}, false
	}
	spec, has, err := r.deps.Verifier.VerifySpec(ctx, task.ID)
	if err != nil || !has {
		// No verify declared (or unreadable) → fall back to result.json.
		return verifyStep{}, false
	}

	r.setRuntimeState(foundation.RuntimeVerifying)
	lg.Info("task.verifying", task.ID, "running ticket verify", map[string]any{
		"task_id": task.ID,
		"cmd":     spec.Cmd,
	})

	code := runVerify(ctx, r.deps.Paths.Root(), spec.Cmd)
	if ctx.Err() != nil {
		lg.Warning("task.canceled", task.ID, "task canceled during verify", map[string]any{
			"task_id": task.ID, "step": "verify",
		})
		return verifyStep{Case: verdictCanceled}, true
	}

	pass := code == 0
	lg.Info("task.verified", task.ID, "verify finished", map[string]any{
		"task_id":   task.ID,
		"exit_code": code,
		"pass":      pass,
	})

	// pr_url / message come from result.json (best effort) for the report payload.
	rj := judge(r.deps.Paths.ResultJson(task.ID))
	if pass {
		return verifyStep{verdict: Verdict{Case: VerdictCodingSuccess, PrURL: rj.PrURL, Message: rj.Message}}, true
	}
	msg := fmt.Sprintf("verify failed: exit %d", code)
	if rj.Message != "" {
		msg = msg + "; " + rj.Message
	}
	return verifyStep{verdict: Verdict{Case: VerdictCodingFailed, Message: msg}}, true
}

// runVerify executes the verify command via `sh -c` in workDir and returns its
// exit code. A command that cannot be started is treated as a failure (-1).
func runVerify(ctx context.Context, workDir, cmd string) int {
	//nolint:gosec // cmd is the ticket-declared verify command; running it is the point.
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = workDir
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return -1
	}
	return 0
}

// finalize converges task state per Verdict (D03 §5.3).
func (r *runner) finalize(ctx context.Context, task foundation.Task, v Verdict, agentExit int) {
	lg := r.deps.Logger
	store := r.deps.Store

	switch v.Case {
	case VerdictCodingSuccess:
		// First: processing → reporting with finished_at + pr_url.
		now := foundation.Now()
		prURL := v.PrURL
		updated, err := store.Transition(task.ID, foundation.StatusReporting, foundation.TaskPatch{
			FinishedAt: &now,
			PrURL:      &prURL,
		})
		if err != nil {
			// Daemon-level IO error after agent already produced PR — rare but documented (§6 boundary).
			lg.Error("task.reporting_failed", task.ID, "failed to transition to reporting", map[string]any{
				"task_id": task.ID,
				"reason":  err.Error(),
			})
			return
		}

		// Then: call FeedbackReporter.
		r.setRuntimeState(foundation.RuntimeReporting)
		issueN, _ := parseIssueNumber(task.ID)
		out := r.deps.Feedback.Report(ctx, source.FeedbackInput{
			TaskID:      task.ID,
			IssueNumber: issueN,
			Trigger:     source.TriggerCodingSuccess,
			PrURL:       prURL,
			Message:     v.Message,
			CommentID:   updated.Feedback.CommentID,
			Assignee:    r.deps.Config.Assignee,
		})

		if out.Result == source.FeedbackSuccess {
			cid := out.CommentID
			label := out.LastLabel
			donePatch := foundation.TaskPatch{
				CommentID: &cid,
				LastLabel: &label,
			}
			if label != "" {
				newLabels := computeUpdatedLabels(updated.Source.Labels, label)
				donePatch.SourceLabels = &newLabels
			}
			if _, dErr := store.Transition(task.ID, foundation.StatusDone, donePatch); dErr != nil {
				lg.Error("task.reporting_failed", task.ID, "failed to transition to done", map[string]any{
					"task_id": task.ID,
					"reason":  dErr.Error(),
				})
				return
			}
			duration := int64(0)
			if updated.Execution.StartedAt != nil {
				duration = time.Since(*updated.Execution.StartedAt).Milliseconds()
			}
			lg.Info("task.completed", task.ID, "task completed", map[string]any{
				"task_id":     task.ID,
				"pr_url":      prURL,
				"duration_ms": duration,
			})
			return
		}

		// Partial success: status stays reporting; persist via UpdateFields.
		cid := out.CommentID
		label := out.LastLabel
		errStr := out.Error
		partialPatch := foundation.TaskPatch{
			CommentID:      &cid,
			LastLabel:      &label,
			ReportingError: &errStr,
		}
		if label != "" {
			newLabels := computeUpdatedLabels(updated.Source.Labels, label)
			partialPatch.SourceLabels = &newLabels
		}
		if _, uErr := store.UpdateFields(task.ID, partialPatch); uErr != nil {
			lg.Error("task.reporting_failed", task.ID, "failed to persist reporting fields", map[string]any{
				"task_id": task.ID,
				"reason":  uErr.Error(),
			})
			return
		}
		lg.Warning("task.reporting_failed", task.ID, "feedback report failed", map[string]any{
			"task_id":         task.ID,
			"pr_url":          prURL,
			"reporting_error": out.Error,
		})

	case VerdictCodingFailed, VerdictMissing, VerdictParseFailed, VerdictInvalid:
		reason := reasonString(v)
		nextCount := task.Execution.FailureCount + 1
		now := foundation.Now()
		if _, err := store.Transition(task.ID, foundation.StatusFailed, foundation.TaskPatch{
			FinishedAt:   &now,
			FailureCount: &nextCount,
		}); err != nil {
			lg.Error("task.failed", task.ID, "failed to transition to failed", map[string]any{
				"task_id": task.ID,
				"reason":  err.Error(),
			})
			return
		}

		switch v.Case {
		case VerdictMissing:
			lg.Warning("task.result_missing", task.ID, "result.json missing", map[string]any{
				"task_id": task.ID,
			})
		case VerdictParseFailed, VerdictInvalid:
			lg.Warning("task.result_parse_failed", task.ID, "result.json parse/validate failed", map[string]any{
				"task_id": task.ID,
				"reason":  v.Reason,
			})
		case VerdictCodingFailed, VerdictCodingSuccess, VerdictUnknown:
			// no per-case event for plain CodingFailed; covered by task.failed below.
		}
		lg.Warning("task.failed", task.ID, "task failed", map[string]any{
			"task_id":         task.ID,
			"reason":          reason,
			"failure_count":   nextCount,
			"agent_exit_code": agentExit,
		})

		// Notify R08; status stays failed regardless of feedback outcome (B8),
		// but persist label / comment movement so task.yaml stays consistent
		// with the GitHub side and the next sync round can detect re-trigger.
		r.setRuntimeState(foundation.RuntimeReporting)
		issueN, _ := parseIssueNumber(task.ID)
		fbOut := r.deps.Feedback.Report(ctx, source.FeedbackInput{
			TaskID:       task.ID,
			IssueNumber:  issueN,
			Trigger:      source.TriggerCodingFailed,
			Message:      reason,
			FailureCount: nextCount,
			MaxFailures:  r.deps.Config.TaskPolicy.MaxFailures,
			CommentID:    task.Feedback.CommentID,
			Assignee:     r.deps.Config.Assignee,
		})
		if fbOut.CommentID != 0 || fbOut.LastLabel != "" {
			fcid := fbOut.CommentID
			flabel := fbOut.LastLabel
			failPatch := foundation.TaskPatch{
				CommentID: &fcid,
				LastLabel: &flabel,
			}
			if fbOut.Result == source.FeedbackSuccess && flabel != "" {
				newLabels := computeUpdatedLabels(task.Source.Labels, flabel)
				failPatch.SourceLabels = &newLabels
			}
			if _, uErr := store.UpdateFields(task.ID, failPatch); uErr != nil {
				lg.Error("task.feedback_persist_failed", task.ID,
					"failed to persist coding_failed feedback", map[string]any{
						"task_id": task.ID,
						"reason":  uErr.Error(),
					})
			}
		}
	}
}

// notifyProcessingStarted switches the GitHub label to gogents:processing and
// updates the existing feedback comment. The result is persisted via
// UpdateFields so we keep status=processing (no state-machine transition).
// Failures are logged at warning level and do not abort the task — the agent
// still gets to run, and finalize() will overwrite the label later anyway.
func (r *runner) notifyProcessingStarted(ctx context.Context, task foundation.Task) {
	if r.deps.Feedback == nil {
		return
	}
	issueN, _ := parseIssueNumber(task.ID)
	out := r.deps.Feedback.Report(ctx, source.FeedbackInput{
		TaskID:      task.ID,
		IssueNumber: issueN,
		Trigger:     source.TriggerProcessingStarted,
		CommentID:   task.Feedback.CommentID,
		Assignee:    r.deps.Config.Assignee,
	})
	cid := out.CommentID
	label := out.LastLabel
	if cid != 0 || label != "" {
		patch := foundation.TaskPatch{
			CommentID: &cid,
			LastLabel: &label,
		}
		if out.Result == source.FeedbackSuccess && label != "" {
			newLabels := computeUpdatedLabels(task.Source.Labels, label)
			patch.SourceLabels = &newLabels
		}
		if _, uErr := r.deps.Store.UpdateFields(task.ID, patch); uErr != nil {
			r.deps.Logger.Error("task.feedback_persist_failed", task.ID,
				"failed to persist processing_started feedback", map[string]any{
					"task_id": task.ID,
					"reason":  uErr.Error(),
				})
		}
	}
	if out.Result == source.FeedbackFailure {
		r.deps.Logger.Warning("task.processing_feedback_failed", task.ID,
			"processing_started feedback failed", map[string]any{
				"task_id": task.ID,
				"reason":  out.Error,
			})
	}
}

// failAndReport is the shared path for pre-Step-6 task-level failures
// (content fetch non-NotFound, input write, git, agent spawn). It funnels
// through finalize with a synthetic CodingFailed verdict so failure_count,
// task.failed event, and feedback report all happen exactly once.
func (r *runner) failAndReport(ctx context.Context, task foundation.Task, reason string, agentExit int) {
	r.finalize(ctx, task, Verdict{Case: VerdictCodingFailed, Message: reason}, agentExit)
}

// reasonString picks a stable reason label per D03 §5.4.
func reasonString(v Verdict) string {
	switch v.Case {
	case VerdictMissing:
		return "result.json missing"
	case VerdictParseFailed:
		return "result.json parse failed: " + v.Reason
	case VerdictInvalid:
		return v.Reason
	case VerdictCodingFailed:
		if v.Message != "" {
			return v.Message
		}
		return "coding failed"
	case VerdictCodingSuccess, VerdictUnknown:
		return "coding failed"
	}
	return "coding failed"
}

func classifySourceErr(err error) string {
	switch {
	case errors.Is(err, source.ErrSourceAuth):
		return "auth"
	case errors.Is(err, source.ErrSourceTransient):
		return "transient"
	default:
		return "unknown"
	}
}

func isCtxErr(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return ctx.Err() != nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// computeUpdatedLabels derives the new task.yaml source.labels list after a
// FeedbackReporter call. It mirrors the GitHub-side mutation: drop every
// existing label under `gogents:`, then append `newLabel` (skipped when
// empty). Non-gogents labels (e.g. user-applied tags) are preserved.
func computeUpdatedLabels(existing []string, newLabel string) []string {
	out := make([]string, 0, len(existing)+1)
	for _, l := range existing {
		if !strings.HasPrefix(l, source.LabelPrefix) {
			out = append(out, l)
		}
	}
	if newLabel != "" {
		out = append(out, newLabel)
	}
	return out
}

// parseIssueNumber strips the leading source prefix and parses the trailing
// integer (Phase 1: only "github-<n>"). Returns 0 + false if the format does
// not match — callers downgrade to a no-op feedback call rather than panic.
func parseIssueNumber(taskID string) (int, bool) {
	idx := strings.LastIndex(taskID, "-")
	if idx < 0 || idx == len(taskID)-1 {
		return 0, false
	}
	n, err := strconv.Atoi(taskID[idx+1:])
	if err != nil {
		return 0, false
	}
	return n, true
}

// deriveRepoDir computes <workspace>/<reponame>/ from the configured repo URL.
// Mirrors the same derivation D02 uses (resolveRepoDir in daemon.go).
func deriveRepoDir(workspaceDir, repoURL string) string {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return filepath.Join(workspaceDir, "repo")
	}
	repoName := filepath.Base(strings.TrimSuffix(parsed.Path, ".git"))
	if repoName == "" || repoName == "." || repoName == "/" {
		return filepath.Join(workspaceDir, "repo")
	}
	return filepath.Join(workspaceDir, repoName)
}
