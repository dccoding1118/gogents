package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/logger"
)

type githubIssueSource struct {
	cfg foundation.Config
	lg  logger.Logger
	ghRunner
}

type ghRunner struct {
	lg logger.Logger
}

func newGitHubIssueSource(cfg foundation.Config, lg logger.Logger) TaskSource {
	return &githubIssueSource{cfg: cfg, lg: lg, ghRunner: ghRunner{lg: lg}}
}

func (s *githubIssueSource) Kind() string { return "github_issue" }

// ghIssue is the schema returned by `gh issue list --json ...` (camelCase).
type ghIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
	URL       string    `json:"url"`
	State     string    `json:"state"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

// ghAPIIssue is the schema returned by `gh api repos/{owner}/{repo}/issues/{n}`
// (GitHub REST, snake_case). Used by Get() so we can rely on HTTP status codes
// in stderr ("(HTTP 404)") rather than text-matching the error message.
type ghAPIIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
	State     string    `json:"state"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

// httpStatusRe extracts the HTTP status code surfaced by `gh api` on errors,
// e.g. `gh: Not Found (HTTP 404)`.
var httpStatusRe = regexp.MustCompile(`\(HTTP (\d{3})\)`)

func classify(code int, stdout, stderr []byte) error {
	outStr := string(stdout)
	errStr := string(stderr)

	// Prefer the HTTP status code emitted by `gh api`. This is a contract-level
	// signal and survives GitHub error-message rewordings.
	if m := httpStatusRe.FindStringSubmatch(errStr); m != nil {
		if status, err := strconv.Atoi(m[1]); err == nil {
			switch {
			case status == 404, status == 410:
				// 410 Gone is what GitHub returns for an issue that was
				// explicitly deleted (vs. 404 for one that never existed).
				// From the syncer's perspective both mean "the source task
				// is no longer reachable" → transition to deleted.
				return ErrSourceNotFound
			case status == 401, status == 403:
				return ErrSourceAuth
			case status == 429, status >= 500:
				return ErrSourceTransient
			case status >= 400:
				// Other 4xx (validation, unprocessable) — treat as transient
				// so the round retries; a real fix needs operator attention.
				return ErrSourceTransient
			}
		}
	}

	// Fallback for `gh` subcommands that don't surface an HTTP status code
	// (e.g. `gh issue list`, `gh label list`). Lowercase comparisons so casing
	// drift on GitHub's side does not regress detection.
	errLower := strings.ToLower(errStr)
	if (code == 0 && len(outStr) == 0) ||
		strings.Contains(errLower, "not found") ||
		strings.Contains(errLower, "could not resolve to an issue") ||
		strings.Contains(errLower, "could not resolve to a pull request") {
		return ErrSourceNotFound
	}
	if strings.Contains(errLower, "authentication") ||
		strings.Contains(errLower, "bad credentials") ||
		strings.Contains(errLower, "gh auth") {
		return ErrSourceAuth
	}
	if strings.Contains(errLower, "rate limit") || strings.Contains(errLower, "abuse detection") {
		return ErrSourceTransient
	}
	if code != 0 && (strings.Contains(errLower, "network") ||
		strings.Contains(errLower, "timeout") ||
		strings.Contains(errLower, "could not resolve host")) {
		return ErrSourceTransient
	}
	if code != 0 {
		return ErrSourceTransient // Console default
	}

	return nil
}

func (r *ghRunner) runWithRetry(ctx context.Context, cmdName string, args []string) ([]byte, error) {
	backoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		cmd := exec.CommandContext(ctx, cmdName, args...)
		outWriter := &strings.Builder{}
		errWriter := &strings.Builder{}
		cmd.Stdout = outWriter
		cmd.Stderr = errWriter

		err := cmd.Run()
		stdout := []byte(outWriter.String())
		stderr := []byte(errWriter.String())

		var code int
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else {
				code = -1 // Cannot spawn
			}
		}

		// check purely if it is a success
		if code == 0 {
			cls := classify(code, stdout, stderr)
			if cls == nil {
				return stdout, nil
			}
		}

		cls := classify(code, stdout, stderr)
		if code == -1 {
			cls = ErrSourceAuth
		}

		level := logger.LevelWarning
		if attempt == 3 || errors.Is(cls, ErrSourceNotFound) || errors.Is(cls, ErrSourceAuth) {
			level = logger.LevelError
		}

		errStr := string(stderr)
		if len(errStr) > 1024 {
			errStr = errStr[:1024] + "..."
		}

		clsStr := "unknown"
		switch {
		case errors.Is(cls, ErrSourceNotFound):
			clsStr = "not_found"
		case errors.Is(cls, ErrSourceAuth):
			clsStr = "auth"
		case errors.Is(cls, ErrSourceTransient):
			clsStr = "transient"
		}

		r.lg.Log(level, "github.cli_failed", "", "gh command failed", map[string]any{
			"command":        cmdName + " " + strings.Join(args, " "),
			"exit_code":      code,
			"stderr":         errStr,
			"classification": clsStr,
			"attempt":        attempt,
			"max_attempts":   3,
		})

		switch {
		case errors.Is(cls, ErrSourceNotFound):
			return nil, ErrSourceNotFound
		case errors.Is(cls, ErrSourceAuth):
			return nil, ErrSourceAuth
		case errors.Is(cls, ErrSourceTransient):
			if attempt == 3 {
				return nil, ErrSourceTransient
			}
			//nolint:gosec // attempt will clearly be 1, 2, or 3
			base := float64(backoffs[attempt-1])
			//nolint:gosec // cryptographically secure random not required for jitter
			jitter := 0.5 + rand.Float64() // 0.5 to 1.5 multiplier
			sleepDur := time.Duration(base * jitter)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(sleepDur):
			}
		}
	}

	return nil, ErrSourceTransient
}

func (s *githubIssueSource) List(ctx context.Context, filter SourceFilter) ([]SourceTask, error) {
	args := []string{"issue", "list", "--repo", s.cfg.Source.Instance, "--json", "number,state,updatedAt,url,title,labels,assignees", "--limit", "200"}
	switch filter.State {
	case SourceStateOpen:
		args = append(args, "--state", "open")
	case SourceStateClosed:
		args = append(args, "--state", "closed")
	}

	for _, l := range filter.Labels {
		args = append(args, "--label", l)
	}

	stdout, err := s.runWithRetry(ctx, "gh", args)
	if err != nil {
		return nil, err
	}

	var issues []ghIssue
	if err := json.Unmarshal(stdout, &issues); err != nil {
		return nil, fmt.Errorf("%w: failed to parse gh output: %v", ErrSourceTransient, err)
	}

	tasks := make([]SourceTask, len(issues))
	for i, issue := range issues {
		tasks[i] = s.mapIssueToTask(issue)
	}
	return tasks, nil
}

// Get retrieves a single issue via `gh api`. Using the REST API (instead of
// `gh issue view`) lets classify() rely on the HTTP status code in stderr
// — a stable contract — so a deleted issue is correctly reported as
// ErrSourceNotFound and the syncer can transition the task to deleted.
func (s *githubIssueSource) Get(ctx context.Context, taskID string) (SourceTask, error) {
	parts := strings.Split(taskID, "-")
	if len(parts) != 2 {
		return SourceTask{}, ErrSourceProgrammer
	}
	number := parts[1]

	apiPath := fmt.Sprintf("repos/%s/issues/%s", s.cfg.Source.Instance, number)
	stdout, err := s.runWithRetry(ctx, "gh", []string{"api", apiPath})
	if err != nil {
		return SourceTask{}, err
	}

	var issue ghAPIIssue
	if err := json.Unmarshal(stdout, &issue); err != nil {
		return SourceTask{}, fmt.Errorf("%w: failed to parse gh output: %v", ErrSourceTransient, err)
	}

	return s.mapAPIIssueToTask(issue), nil
}

func (s *githubIssueSource) mapIssueToTask(issue ghIssue) SourceTask {
	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.Name
	}
	assignees := make([]string, len(issue.Assignees))
	for i, a := range issue.Assignees {
		assignees[i] = a.Login
	}

	state := SourceStateOpen
	if strings.ToLower(issue.State) == "closed" {
		state = SourceStateClosed
	}

	return SourceTask{
		TaskID:    fmt.Sprintf("github-%d", issue.Number),
		State:     state,
		UpdatedAt: issue.UpdatedAt,
		URL:       issue.URL,
		Title:     issue.Title,
		Labels:    labels,
		Assignees: assignees,
	}
}

func (s *githubIssueSource) mapAPIIssueToTask(issue ghAPIIssue) SourceTask {
	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.Name
	}
	assignees := make([]string, len(issue.Assignees))
	for i, a := range issue.Assignees {
		assignees[i] = a.Login
	}

	state := SourceStateOpen
	if strings.ToLower(issue.State) == "closed" {
		state = SourceStateClosed
	}

	return SourceTask{
		TaskID:    fmt.Sprintf("github-%d", issue.Number),
		State:     state,
		UpdatedAt: issue.UpdatedAt,
		URL:       issue.HTMLURL,
		Title:     issue.Title,
		Labels:    labels,
		Assignees: assignees,
	}
}

type githubIssueProbe struct {
	cfg foundation.Config
	lg  logger.Logger
}

func newGitHubIssueProbe(cfg foundation.Config, lg logger.Logger) SourceProbe {
	return &githubIssueProbe{cfg: cfg, lg: lg}
}

func (p *githubIssueProbe) CheckSource() error {
	// 1. Check gh installation
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("%w: gh CLI not found in PATH", ErrSourceTransient)
	}

	// 2. Check auth status
	authCmd := exec.Command("gh", "auth", "status")
	if err := authCmd.Run(); err != nil {
		return fmt.Errorf("%w: gh auth status failed; please run 'gh auth login'", ErrSourceAuth)
	}

	// 3. Check repo access
	//nolint:gosec // p.cfg.Source.Instance is user-determined configuration.
	repoCmd := exec.Command("gh", "repo", "view", p.cfg.Source.Instance, "--json", "id")
	if err := repoCmd.Run(); err != nil {
		return fmt.Errorf("%w: cannot access repo %s; check permissions", ErrSourceTransient, p.cfg.Source.Instance)
	}

	return nil
}
