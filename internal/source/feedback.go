package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/internal/logger"
)

// FeedbackReporter interacts with GitHub issues to add comments and labels.
type FeedbackReporter interface {
	Report(ctx context.Context, in FeedbackInput) FeedbackOutcome
}

type FeedbackTrigger string

const (
	TriggerTaskCreated       FeedbackTrigger = "task_created"
	TriggerProcessingStarted FeedbackTrigger = "processing_started"
	TriggerCodingSuccess     FeedbackTrigger = "coding_success"
	TriggerCodingFailed      FeedbackTrigger = "coding_failed"
)

// FeedbackMarker is the hidden HTML comment placed at the top of every
// feedback comment body. ContentFetcher uses this to filter gogents-authored
// comments out of input.md so they do not leak back into the agent prompt.
const FeedbackMarker = "<!-- gogents-feedback -->"

// gogents-owned status label namespace. The user-facing trigger label
// (configured via source.filter.label) lives outside this namespace; gogents
// rotates exactly one of these labels per state transition.
const (
	LabelPrefix     = "gogents:"
	LabelQueued     = "gogents:queued"
	LabelProcessing = "gogents:processing"
	LabelDone       = "gogents:done"
	LabelFailed     = "gogents:failed"
)

type FeedbackInput struct {
	TaskID       string
	IssueNumber  int
	Trigger      FeedbackTrigger
	PrURL        string
	Message      string
	FailureCount int
	MaxFailures  int
	CommentID    int64
	Assignee     string
}

type FeedbackResult string

const (
	FeedbackSuccess FeedbackResult = "success"
	FeedbackFailure FeedbackResult = "failure"
)

type FeedbackOutcome struct {
	Result    FeedbackResult
	Error     string
	CommentID int64
	LastLabel string
}

type githubFeedbackReporter struct {
	cfg foundation.Config
	lg  logger.Logger
	ghRunner
}

func newGitHubFeedbackReporter(cfg foundation.Config, lg logger.Logger) FeedbackReporter {
	return &githubFeedbackReporter{cfg: cfg, lg: lg, ghRunner: ghRunner{lg: lg}}
}

func (r *githubFeedbackReporter) Report(ctx context.Context, in FeedbackInput) FeedbackOutcome {
	body := r.renderTemplate(in)

	switch in.Trigger {
	case TriggerTaskCreated:
		cid, err := r.createComment(ctx, in.IssueNumber, body)
		if err != nil {
			return FeedbackOutcome{Result: FeedbackFailure, Error: fmt.Sprintf("create_comment: %v", err)}
		}
		if err := r.switchLabel(ctx, in.IssueNumber, LabelPrefix+"*", LabelQueued); err != nil {
			return FeedbackOutcome{Result: FeedbackFailure, Error: fmt.Sprintf("label_change: %v", err), CommentID: cid}
		}
		return FeedbackOutcome{Result: FeedbackSuccess, CommentID: cid, LastLabel: LabelQueued}

	case TriggerProcessingStarted:
		cid, err := r.upsertComment(ctx, in.IssueNumber, in.CommentID, body)
		if err != nil {
			return FeedbackOutcome{Result: FeedbackFailure, Error: fmt.Sprintf("upsert_comment: %v", err), CommentID: cid}
		}
		if err := r.switchLabel(ctx, in.IssueNumber, LabelPrefix+"*", LabelProcessing); err != nil {
			return FeedbackOutcome{Result: FeedbackFailure, Error: fmt.Sprintf("label_change: %v", err), CommentID: cid}
		}
		return FeedbackOutcome{Result: FeedbackSuccess, CommentID: cid, LastLabel: LabelProcessing}

	case TriggerCodingSuccess:
		cid, err := r.upsertComment(ctx, in.IssueNumber, in.CommentID, body)
		if err != nil {
			return FeedbackOutcome{Result: FeedbackFailure, Error: fmt.Sprintf("upsert_comment: %v", err), CommentID: cid}
		}
		if err := r.switchLabel(ctx, in.IssueNumber, LabelPrefix+"*", LabelDone); err != nil {
			return FeedbackOutcome{Result: FeedbackFailure, Error: fmt.Sprintf("label_change: %v", err), CommentID: cid}
		}
		return FeedbackOutcome{Result: FeedbackSuccess, CommentID: cid, LastLabel: LabelDone}

	case TriggerCodingFailed:
		cid, err := r.upsertComment(ctx, in.IssueNumber, in.CommentID, body)
		if err != nil {
			return FeedbackOutcome{Result: FeedbackFailure, Error: fmt.Sprintf("upsert_comment: %v", err), CommentID: cid}
		}
		if err := r.switchLabel(ctx, in.IssueNumber, LabelPrefix+"*", LabelFailed); err != nil {
			return FeedbackOutcome{Result: FeedbackFailure, Error: fmt.Sprintf("label_change: %v", err), CommentID: cid}
		}
		return FeedbackOutcome{Result: FeedbackSuccess, CommentID: cid, LastLabel: LabelFailed}
	}

	r.lg.Log(logger.LevelError, "github.cli_failed", "", "bad_trigger", map[string]any{"classification": "programmer", "trigger": in.Trigger})
	return FeedbackOutcome{Result: FeedbackFailure, Error: "bad_trigger"}
}

func (r *githubFeedbackReporter) renderTemplate(in FeedbackInput) string {
	nowStr := foundation.Now().Format("2006-01-02 15:04:05 -07:00")
	var mention string
	if in.Assignee != "" {
		mention = "@" + in.Assignee + " "
	}

	switch in.Trigger {
	case TriggerTaskCreated:
		return fmt.Sprintf("%s\n🤖 Task Syncer:任務已建立\n- Task ID: %s\n- Runtime: %s\n- 狀態:queued\n- 建立時間:%s",
			FeedbackMarker, in.TaskID, r.cfg.RuntimeName, nowStr)

	case TriggerProcessingStarted:
		return fmt.Sprintf("%s\n🤖 Task Syncer:任務執行中 ⚙️\n- Task ID: %s\n- 狀態:processing\n- 開始時間:%s\n\n%sAgent 已啟動,完成後將更新此留言。",
			FeedbackMarker, in.TaskID, nowStr, mention)

	case TriggerCodingSuccess:
		return fmt.Sprintf("%s\n🤖 Task Syncer:任務完成 ✅\n- Task ID: %s\n- 狀態:done\n- PR: %s\n- 完成時間:%s\n- 摘要:%s\n\n%s請審核結果。",
			FeedbackMarker, in.TaskID, in.PrURL, nowStr, in.Message, mention)

	case TriggerCodingFailed:
		if in.FailureCount < in.MaxFailures {
			return fmt.Sprintf("%s\n🤖 Task Syncer:任務失敗 ❌\n- Task ID: %s\n- 狀態:failed(第 %d / %d 次失敗)\n- 失敗時間:%s\n- 原因:%s\n\n%s請審核結果。重開 issue 或重新加上觸發 label 可再次執行。",
				FeedbackMarker, in.TaskID, in.FailureCount, in.MaxFailures, nowStr, in.Message, mention)
		}
		return fmt.Sprintf("%s\n🤖 Task Syncer:任務失敗 ❌(已達失敗上限)\n- Task ID: %s\n- 狀態:failed(%d / %d 次,停止自動重試)\n- 失敗時間:%s\n- 原因:%s\n\n%s需人工介入。可直接於本機重置 failure_count,或刪除 task 目錄讓系統重建。",
			FeedbackMarker, in.TaskID, in.FailureCount, in.MaxFailures, nowStr, in.Message, mention)
	}
	return ""
}

func (r *githubFeedbackReporter) upsertComment(ctx context.Context, issueN int, existingID int64, body string) (int64, error) {
	if existingID == 0 {
		return r.createComment(ctx, issueN, body)
	}
	err := r.patchComment(ctx, existingID, body)
	if errors.Is(err, ErrSourceNotFound) {
		return r.createComment(ctx, issueN, body)
	}
	if err != nil {
		return existingID, err
	}
	r.lg.Log(logger.LevelInfo, "github.comment_updated", "", "", map[string]any{"task_id": fmt.Sprintf("github-%d", issueN), "issue_number": issueN, "comment_id": existingID})
	return existingID, nil
}

func (r *githubFeedbackReporter) createComment(ctx context.Context, issueN int, body string) (int64, error) {
	nStr := strconv.Itoa(issueN)
	stdout, err := r.runWithRetry(ctx, "gh", []string{"issue", "comment", nStr, "--repo", r.cfg.Source.Instance, "--body", body})
	if err != nil {
		return 0, err
	}
	url := strings.TrimSpace(string(stdout))
	idx := strings.LastIndex(url, "-")
	if idx == -1 {
		return 0, fmt.Errorf("could not parse comment id from %s", url)
	}
	cid, err := strconv.ParseInt(url[idx+1:], 10, 64)
	if err != nil {
		return 0, err
	}
	r.lg.Log(logger.LevelInfo, "github.comment_created", "", "", map[string]any{"task_id": fmt.Sprintf("github-%d", issueN), "issue_number": issueN, "comment_id": cid})
	return cid, nil
}

func (r *githubFeedbackReporter) patchComment(ctx context.Context, existingID int64, body string) error {
	cidStr := strconv.FormatInt(existingID, 10)
	apiPath := fmt.Sprintf("repos/%s/issues/comments/%s", r.cfg.Source.Instance, cidStr)
	_, err := r.runWithRetry(ctx, "gh", []string{"api", apiPath, "-X", "PATCH", "-f", "body=" + body})
	return err
}

func (r *githubFeedbackReporter) switchLabel(ctx context.Context, issueN int, fromPattern, to string) error {
	if err := r.ensureLabel(ctx, to); err != nil {
		return err
	}
	if fromPattern != "" {
		if err := r.removeLabelsMatching(ctx, issueN, fromPattern); err != nil {
			return err
		}
	}
	if to != "" {
		if err := r.addLabel(ctx, issueN, to); err != nil {
			return err
		}
	}
	r.lg.Log(logger.LevelInfo, "github.label_changed", "", "", map[string]any{"task_id": fmt.Sprintf("github-%d", issueN), "issue_number": issueN, "from": fromPattern, "to": to})
	return nil
}

func (r *githubFeedbackReporter) ensureLabel(ctx context.Context, labelName string) error {
	_, err := r.runWithRetry(ctx, "gh", []string{"label", "list", "--repo", r.cfg.Source.Instance, "--search", labelName, "--json", "name"})
	if errors.Is(err, ErrSourceNotFound) {
		// Attempt to create
		_, createErr := r.runWithRetry(ctx, "gh", []string{"label", "create", labelName, "--repo", r.cfg.Source.Instance})
		if createErr != nil {
			return createErr
		}
		r.lg.Log(logger.LevelInfo, "github.label_created", "", "", map[string]any{"label": labelName})
		return nil
	}
	return err
}

func (r *githubFeedbackReporter) removeLabelsMatching(ctx context.Context, issueN int, pattern string) error {
	// Simple pattern matching for "gogents:*"
	// First fetch existing labels
	nStr := strconv.Itoa(issueN)
	stdout, err := r.runWithRetry(ctx, "gh", []string{"issue", "view", nStr, "--repo", r.cfg.Source.Instance, "--json", "labels"})
	if err != nil {
		return err
	}
	var viewResp struct {
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(stdout, &viewResp); err != nil {
		return fmt.Errorf("%w: failed parsing view labels: %v", ErrSourceTransient, err)
	}

	var toRemove []string
	prefix := strings.TrimSuffix(pattern, "*")
	for _, l := range viewResp.Labels {
		if strings.HasPrefix(l.Name, prefix) {
			toRemove = append(toRemove, l.Name)
		}
	}

	for _, l := range toRemove {
		_, err := r.runWithRetry(ctx, "gh", []string{"issue", "edit", nStr, "--repo", r.cfg.Source.Instance, "--remove-label", l})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *githubFeedbackReporter) addLabel(ctx context.Context, issueN int, labelName string) error {
	nStr := strconv.Itoa(issueN)
	_, err := r.runWithRetry(ctx, "gh", []string{"issue", "edit", nStr, "--repo", r.cfg.Source.Instance, "--add-label", labelName})
	return err
}
