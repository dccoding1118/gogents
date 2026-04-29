package daemon

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/logger"
	"github.com/dccoding/gogents/internal/source"
)

// Syncer performs one round of synchronization per Tick call (D02 §4.2).
type Syncer interface {
	Tick(ctx context.Context) (SyncOutcome, error)
}

// SyncOutcome captures the result of a single sync round.
type SyncOutcome struct {
	QueriedTotal    int
	Created         []string
	Updated         []string
	Deleted         []string
	Unchanged       int
	DurationMs      int64
	PerSourceErrors []PerSourceError
}

// PerSourceError records a per-task query failure that does not abort the entire sync round.
type PerSourceError struct {
	TaskID string
	Reason string
}

type defaultSyncer struct {
	store    foundation.TaskStore
	source   source.TaskSource
	feedback source.FeedbackReporter
	lg       logger.Logger
	cfg      foundation.Config
}

// NewSyncer creates a Syncer that synchronizes tasks from the given TaskSource into the TaskStore.
// feedback may be nil when the daemon runs in a no-feedback mode (Stage 2 only); in Stage 3 it is required.
func NewSyncer(store foundation.TaskStore, src source.TaskSource, feedback source.FeedbackReporter, lg logger.Logger, cfg foundation.Config) Syncer {
	return &defaultSyncer{
		store:    store,
		source:   src,
		feedback: feedback,
		lg:       lg,
		cfg:      cfg,
	}
}

func (s *defaultSyncer) Tick(ctx context.Context) (SyncOutcome, error) {
	t0 := time.Now()
	var outcome SyncOutcome

	// Build filter from config.
	filter := source.SourceFilter{
		Labels: []string{s.cfg.Source.Filter.Label},
		State:  source.SourceStateOpen,
	}

	// List remote tasks.
	remoteTasks, err := s.source.List(ctx, filter)
	if err != nil {
		return outcome, err // Entire sync fails; caller decides sync.failed.
	}

	// Build remote lookup map.
	remoteMap := make(map[string]source.SourceTask, len(remoteTasks))
	for _, rt := range remoteTasks {
		remoteMap[rt.TaskID] = rt
	}

	// Load local tasks (exclude deleted).
	localTasks, err := s.store.List()
	if err != nil {
		return outcome, err
	}

	localMap := make(map[string]foundation.Task, len(localTasks))
	for _, lt := range localTasks {
		if lt.Status != foundation.StatusDeleted {
			localMap[lt.ID] = lt
		}
	}

	// Detect new tasks.
	for _, rt := range remoteTasks {
		if _, exists := localMap[rt.TaskID]; exists {
			continue
		}
		issueN := parseIssueNumberFromTaskID(rt.TaskID)
		newTask := foundation.Task{
			ID:     rt.TaskID,
			Status: foundation.StatusQueued,
			Source: foundation.SourceFields{
				IssueNumber: issueN,
				Title:       rt.Title,
				Labels:      rt.Labels,
				Assignees:   rt.Assignees,
				State:       string(rt.State),
				UpdatedAt:   rt.UpdatedAt,
				URL:         rt.URL,
			},
		}
		if pErr := s.store.Put(newTask); pErr != nil {
			outcome.PerSourceErrors = append(outcome.PerSourceErrors, PerSourceError{
				TaskID: rt.TaskID,
				Reason: pErr.Error(),
			})
			s.lg.Error("sync.task_create_failed", rt.TaskID, "failed to create task", map[string]any{
				"reason": pErr.Error(),
			})
			// Local state did not change despite the attempt — keep the
			// per-bucket invariant (created+updated+deleted+unchanged ==
			// queried_total) by counting this distinct task as unchanged.
			outcome.Unchanged++
			continue
		}
		outcome.Created = append(outcome.Created, rt.TaskID)
		s.lg.Info("sync.task_created", rt.TaskID, "new task created", map[string]any{
			"kind": s.source.Kind(),
			"url":  rt.URL,
		})

		// D02 §2 B3: trigger TaskCreated feedback (comment + queued label).
		// Failures here do not abort sync; they are logged via FeedbackOutcome.Error
		// (and D05 already emits github.cli_failed). Persist whatever partial
		// state the reporter returns so it is not lost on restart.
		if s.feedback != nil {
			out := s.feedback.Report(ctx, source.FeedbackInput{
				TaskID:      rt.TaskID,
				IssueNumber: issueN,
				Trigger:     source.TriggerTaskCreated,
				Assignee:    s.cfg.Assignee,
			})
			cid := out.CommentID
			label := out.LastLabel
			if cid != 0 || label != "" {
				if _, uErr := s.store.UpdateFields(rt.TaskID, foundation.TaskPatch{
					CommentID: &cid,
					LastLabel: &label,
				}); uErr != nil {
					s.lg.Error("sync.feedback_persist_failed", rt.TaskID, "failed to persist task_created feedback", map[string]any{
						"reason": uErr.Error(),
					})
				}
			}
			if out.Result == source.FeedbackFailure {
				s.lg.Warning("sync.feedback_failed", rt.TaskID, "task_created feedback failed", map[string]any{
					"task_id": rt.TaskID,
					"reason":  out.Error,
				})
			}
		}
	}

	// Track existing tasks + detect deletions.
	//
	// Bucket invariant: every distinct task scanned this round (whether seen
	// only remotely, only locally, or both) is counted in exactly one of
	// created/updated/deleted/unchanged. PerSourceErrors is a diagnostic
	// side-channel and does NOT replace bucket assignment — errored tasks
	// fall back to unchanged because the local task.yaml did not change.
	for _, lt := range localTasks {
		if lt.Status == foundation.StatusDeleted {
			// Already deleted locally; the syncer skips Get for these but
			// the task is still part of this round's distinct scan set.
			outcome.Unchanged++
			continue
		}

		rt2, gErr := s.source.Get(ctx, lt.ID)
		switch {
		case errors.Is(gErr, source.ErrSourceNotFound):
			// Task deleted from source.
			if _, tErr := s.store.Transition(lt.ID, foundation.StatusDeleted, foundation.TaskPatch{}); tErr != nil {
				outcome.PerSourceErrors = append(outcome.PerSourceErrors, PerSourceError{
					TaskID: lt.ID,
					Reason: tErr.Error(),
				})
				s.lg.Error("sync.task_delete_failed", lt.ID, "failed to transition to deleted", map[string]any{
					"reason": tErr.Error(),
				})
				outcome.Unchanged++
				continue
			}
			outcome.Deleted = append(outcome.Deleted, lt.ID)
			s.lg.Info("sync.task_deleted", lt.ID, "task deleted from source", nil)
			continue

		case gErr != nil:
			// Auth / transient / other — don't change status, record error.
			reason := classifyError(gErr)
			outcome.PerSourceErrors = append(outcome.PerSourceErrors, PerSourceError{
				TaskID: lt.ID,
				Reason: reason,
			})
			s.lg.Error("sync.source_query_failed", lt.ID, "source query failed", map[string]any{
				"reason": reason,
			})
			outcome.Unchanged++
			continue
		}

		// Check for changes.
		if rt2.UpdatedAt.Equal(lt.Source.UpdatedAt) && string(rt2.State) == lt.Source.State {
			outcome.Unchanged++
			continue
		}

		// Always persist source field updates first, independent of any state
		// machine outcome. This way, IssueNumber / Labels / UpdatedAt always
		// reflect the latest GitHub state even when the next status transition
		// is illegal (which would otherwise reject the entire patch).
		sourcePatch := foundation.TaskPatch{
			Source: &foundation.SourceFields{
				IssueNumber: parseIssueNumberFromTaskID(rt2.TaskID),
				Title:       rt2.Title,
				Labels:      rt2.Labels,
				Assignees:   rt2.Assignees,
				State:       string(rt2.State),
				UpdatedAt:   rt2.UpdatedAt,
				URL:         rt2.URL,
			},
		}
		if _, uErr := s.store.UpdateFields(lt.ID, sourcePatch); uErr != nil {
			outcome.PerSourceErrors = append(outcome.PerSourceErrors, PerSourceError{
				TaskID: lt.ID,
				Reason: uErr.Error(),
			})
			s.lg.Error("sync.source_update_failed", lt.ID, "failed to update source fields", map[string]any{
				"reason": uErr.Error(),
			})
			// Source not actually written → no local change.
			outcome.Unchanged++
			continue
		}

		// Source fields are now persisted; from the bucket point of view this
		// task is "updated" regardless of whether the subsequent status
		// transition succeeds.
		outcome.Updated = append(outcome.Updated, lt.ID)

		nextStatus := applySourceTransition(lt.Status, rt2, lt.Execution.FailureCount, s.cfg.TaskPolicy.MaxFailures)

		if nextStatus == foundation.StatusFailed && lt.Status != foundation.StatusFailed {
			s.lg.Warning("sync.failure_lock_reached", lt.ID, "failure count reached max_failures", map[string]any{
				"failure_count": lt.Execution.FailureCount,
				"max_failures":  s.cfg.TaskPolicy.MaxFailures,
			})
		}

		// Skip the Transition call entirely when status does not change —
		// avoids spurious failed→failed / done→done illegal transitions and
		// the patch has already been written above.
		if nextStatus == lt.Status {
			continue
		}

		if _, tErr := s.store.Transition(lt.ID, nextStatus, foundation.TaskPatch{}); tErr != nil {
			// Check if it's an illegal transition — log as program error.
			if errors.Is(tErr, foundation.ErrIllegalTransition) {
				s.lg.Error("sync.illegal_transition", lt.ID, "unexpected illegal transition", map[string]any{
					"from":   string(lt.Status),
					"to":     string(nextStatus),
					"reason": tErr.Error(),
				})
			} else {
				outcome.PerSourceErrors = append(outcome.PerSourceErrors, PerSourceError{
					TaskID: lt.ID,
					Reason: tErr.Error(),
				})
			}
			continue
		}
		s.lg.Info("sync.task_updated", lt.ID, "task updated", map[string]any{
			"from": string(lt.Status),
			"to":   string(nextStatus),
		})
	}

	// QueriedTotal = |distinct(remote ∪ local)| — a task that lives on both
	// sides counts once. Issue numbers on GitHub are never reused, so a
	// locally-deleted task will not reappear in the remote List set; we
	// nonetheless rely on the union semantics rather than that assumption.
	distinct := make(map[string]struct{}, len(remoteTasks)+len(localTasks))
	for _, rt := range remoteTasks {
		distinct[rt.TaskID] = struct{}{}
	}
	for _, lt := range localTasks {
		distinct[lt.ID] = struct{}{}
	}
	outcome.QueriedTotal = len(distinct)
	outcome.DurationMs = time.Since(t0).Milliseconds()
	return outcome, nil
}

// applySourceTransition is a pure function encapsulating R03/R05 state transition rules.
//
// Re-queueing a terminal task (done / failed) is **opt-in by the user**: they
// flip the gogents-owned status label on the issue back to `gogents:queued`.
// We deliberately do not look at issue state or updated_at — adding a feedback
// comment after each transition bumps updated_at, so any time-based check would
// fire on its own. The only definitive signal is the user-visible label.
//
// processing / reporting react to the issue being closed (treated as a manual
// "this is done") because closing an issue is a deliberate user action and
// has no comment-bump confound.
func applySourceTransition(localStatus foundation.Status, remote source.SourceTask, failureCount, maxFailures int) foundation.Status {
	isClosed := remote.State == source.SourceStateClosed
	requeueRequested := containsLabel(remote.Labels, source.LabelQueued)

	switch localStatus {
	case foundation.StatusQueued:
		return foundation.StatusQueued

	case foundation.StatusProcessing:
		if isClosed {
			return foundation.StatusDone
		}
		return foundation.StatusProcessing

	case foundation.StatusDone:
		if requeueRequested {
			return foundation.StatusQueued
		}
		return foundation.StatusDone

	case foundation.StatusFailed:
		if requeueRequested && failureCount < maxFailures {
			return foundation.StatusQueued
		}
		return foundation.StatusFailed

	case foundation.StatusReporting:
		if isClosed {
			return foundation.StatusDone
		}
		return foundation.StatusReporting

	default:
		return localStatus
	}
}

func containsLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

// classifyError maps source errors to human-readable reason strings.
func classifyError(err error) string {
	switch {
	case errors.Is(err, source.ErrSourceAuth):
		return "auth"
	case errors.Is(err, source.ErrSourceTransient):
		return "transient"
	default:
		return "unknown"
	}
}

// parseIssueNumberFromTaskID extracts the trailing integer from a task ID like
// "github-42". Returns 0 when the format does not match (e.g. future source kinds);
// FeedbackReporter implementations must tolerate IssueNumber=0 as best-effort.
func parseIssueNumberFromTaskID(taskID string) int {
	idx := strings.LastIndex(taskID, "-")
	if idx < 0 || idx == len(taskID)-1 {
		return 0
	}
	n, err := strconv.Atoi(taskID[idx+1:])
	if err != nil {
		return 0
	}
	return n
}

// pickNextTask selects the queued task with the lexicographically smallest ID.
func pickNextTask(tasks []foundation.Task) *foundation.Task {
	var candidates []foundation.Task
	for _, t := range tasks {
		if t.Status == foundation.StatusQueued {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ID < candidates[j].ID
	})
	return &candidates[0]
}
