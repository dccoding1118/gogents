package source

import (
	"errors"
	"testing"
	"time"

	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/internal/logger"
)

// mockTestLogger is a no-op logger for source tests.
type mockTestLogger struct{}

func (m *mockTestLogger) Info(event, taskID, msg string, fields map[string]any)    {}
func (m *mockTestLogger) Warning(event, taskID, msg string, fields map[string]any) {}
func (m *mockTestLogger) Error(event, taskID, msg string, fields map[string]any)   {}
func (m *mockTestLogger) Log(level logger.Level, event, taskID, message string, fields map[string]any) {
}
func (m *mockTestLogger) Close() error { return nil }

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		stdout string
		stderr string
		expect error
	}{
		// D05-03: 404 Not Found
		{
			name:   "HTTP 404 not found",
			code:   1,
			stderr: "gh: Not Found (HTTP 404)",
			expect: ErrSourceNotFound,
		},
		// D05-03: 410 Gone
		{
			name:   "HTTP 410 gone",
			code:   1,
			stderr: "gh: Gone (HTTP 410)",
			expect: ErrSourceNotFound,
		},
		// D05-04: 401 Unauthorized
		{
			name:   "HTTP 401 unauthorized",
			code:   1,
			stderr: "gh: Unauthorized (HTTP 401)",
			expect: ErrSourceAuth,
		},
		// D05-04: 403 Forbidden
		{
			name:   "HTTP 403 forbidden",
			code:   1,
			stderr: "gh: Forbidden (HTTP 403)",
			expect: ErrSourceAuth,
		},
		// D05-05: 429 Rate limit
		{
			name:   "HTTP 429 rate limit",
			code:   1,
			stderr: "gh: rate limit exceeded (HTTP 429)",
			expect: ErrSourceTransient,
		},
		// D05-05: 500 Server error
		{
			name:   "HTTP 500 server error",
			code:   1,
			stderr: "gh: Internal Server Error (HTTP 500)",
			expect: ErrSourceTransient,
		},
		// D05-05: network error via fallback
		{
			name:   "network timeout fallback",
			code:   1,
			stderr: "could not resolve host",
			expect: ErrSourceTransient,
		},
		// D05-05: timeout keyword
		{
			name:   "timeout keyword",
			code:   1,
			stderr: "connection timeout",
			expect: ErrSourceTransient,
		},
		// Fallback: not found text
		{
			name:   "text: not found",
			code:   1,
			stderr: "issue not found",
			expect: ErrSourceNotFound,
		},
		// Fallback: authentication text
		{
			name:   "text: authentication",
			code:   1,
			stderr: "authentication required",
			expect: ErrSourceAuth,
		},
		// Fallback: bad credentials
		{
			name:   "text: bad credentials",
			code:   1,
			stderr: "Bad Credentials returned",
			expect: ErrSourceAuth,
		},
		// Fallback: gh auth hint
		{
			name:   "text: gh auth",
			code:   1,
			stderr: "Try 'gh auth login'",
			expect: ErrSourceAuth,
		},
		// Fallback: rate limit text
		{
			name:   "text: rate limit",
			code:   1,
			stderr: "You have exceeded a secondary rate limit",
			expect: ErrSourceTransient,
		},
		// Fallback: abuse detection
		{
			name:   "text: abuse detection",
			code:   1,
			stderr: "abuse detection mechanism",
			expect: ErrSourceTransient,
		},
		// Code 0 with empty output → not found
		{
			name:   "code 0 empty output",
			code:   0,
			stdout: "",
			stderr: "",
			expect: ErrSourceNotFound,
		},
		// Non-zero exit with unknown stderr → transient (default)
		{
			name:   "unknown non-zero exit",
			code:   1,
			stderr: "some unknown error",
			expect: ErrSourceTransient,
		},
		// Exit 0 with valid output → nil
		{
			name:   "success",
			code:   0,
			stdout: `[{"number":1}]`,
			stderr: "",
			expect: nil,
		},
		// Other 4xx (e.g. 422 Unprocessable)
		{
			name:   "HTTP 422 unprocessable",
			code:   1,
			stderr: "gh: Validation Failed (HTTP 422)",
			expect: ErrSourceTransient,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.code, []byte(tc.stdout), []byte(tc.stderr))
			if tc.expect == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if !errors.Is(got, tc.expect) {
				t.Errorf("expected %v, got %v", tc.expect, got)
			}
		})
	}
}

func TestMapIssueToTask(t *testing.T) {
	// D05-12: Verify OPEN/CLOSED mapping to system internal enum.
	src := &githubIssueSource{
		cfg: foundation.Config{},
		lg:  &mockTestLogger{},
	}

	t.Run("OPEN state", func(t *testing.T) {
		issue := ghIssue{
			Number:    42,
			Title:     "Test Issue",
			State:     "OPEN",
			URL:       "https://github.com/user/repo/issues/42",
			UpdatedAt: time.Now(),
			Labels: []struct {
				Name string `json:"name"`
			}{{Name: "bug"}},
			Assignees: []struct {
				Login string `json:"login"`
			}{{Login: "user1"}},
		}

		task := src.mapIssueToTask(issue)

		if task.TaskID != "github-42" {
			t.Errorf("expected github-42, got %s", task.TaskID)
		}
		if task.State != SourceStateOpen {
			t.Errorf("expected open, got %s", task.State)
		}
		if task.Title != "Test Issue" {
			t.Errorf("expected 'Test Issue', got %s", task.Title)
		}
		if len(task.Labels) != 1 || task.Labels[0] != "bug" {
			t.Errorf("expected [bug], got %v", task.Labels)
		}
		if len(task.Assignees) != 1 || task.Assignees[0] != "user1" {
			t.Errorf("expected [user1], got %v", task.Assignees)
		}
	})

	t.Run("CLOSED state", func(t *testing.T) {
		issue := ghIssue{
			Number: 99,
			Title:  "Closed Issue",
			State:  "CLOSED",
		}

		task := src.mapIssueToTask(issue)

		if task.State != SourceStateClosed {
			t.Errorf("expected closed, got %s", task.State)
		}
	})
}

func TestMapAPIIssueToTask(t *testing.T) {
	// D05-12: Verify gh api response (snake_case) mapping.
	src := &githubIssueSource{
		cfg: foundation.Config{},
		lg:  &mockTestLogger{},
	}

	t.Run("open state", func(t *testing.T) {
		issue := ghAPIIssue{
			Number:    7,
			Title:     "API Issue",
			State:     "open",
			HTMLURL:   "https://github.com/user/repo/issues/7",
			UpdatedAt: time.Now(),
			Labels: []struct {
				Name string `json:"name"`
			}{{Name: "enhancement"}},
			Assignees: []struct {
				Login string `json:"login"`
			}{{Login: "dev1"}, {Login: "dev2"}},
		}

		task := src.mapAPIIssueToTask(issue)

		if task.TaskID != "github-7" {
			t.Errorf("expected github-7, got %s", task.TaskID)
		}
		if task.State != SourceStateOpen {
			t.Errorf("expected open, got %s", task.State)
		}
		if task.URL != "https://github.com/user/repo/issues/7" {
			t.Errorf("expected HTML URL, got %s", task.URL)
		}
		if len(task.Assignees) != 2 {
			t.Errorf("expected 2 assignees, got %d", len(task.Assignees))
		}
	})

	t.Run("closed state", func(t *testing.T) {
		issue := ghAPIIssue{
			Number: 8,
			State:  "closed",
		}

		task := src.mapAPIIssueToTask(issue)

		if task.State != SourceStateClosed {
			t.Errorf("expected closed, got %s", task.State)
		}
	})
}

func TestDispatchers(t *testing.T) {
	lg := &mockTestLogger{}

	t.Run("NewTaskSourceFromConfig: github_issue", func(t *testing.T) {
		cfg := foundation.Config{}
		cfg.Source.Type = "github_issue"
		src, err := NewTaskSourceFromConfig(cfg, lg)
		if err != nil {
			t.Fatal(err)
		}
		if src.Kind() != "github_issue" {
			t.Errorf("expected github_issue, got %s", src.Kind())
		}
	})

	t.Run("NewTaskSourceFromConfig: unsupported", func(t *testing.T) {
		cfg := foundation.Config{}
		cfg.Source.Type = "jira"
		_, err := NewTaskSourceFromConfig(cfg, lg)
		if err == nil {
			t.Error("expected error for unsupported source type")
		}
	})

	t.Run("NewSourceProbeFromConfig: github_issue", func(t *testing.T) {
		cfg := foundation.Config{}
		cfg.Source.Type = "github_issue"
		probe, err := NewSourceProbeFromConfig(cfg, lg)
		if err != nil {
			t.Fatal(err)
		}
		if probe == nil {
			t.Error("probe should not be nil")
		}
	})

	t.Run("NewSourceProbeFromConfig: unsupported", func(t *testing.T) {
		cfg := foundation.Config{}
		cfg.Source.Type = "unsupported"
		_, err := NewSourceProbeFromConfig(cfg, lg)
		if err == nil {
			t.Error("expected error for unsupported source type")
		}
	})

	t.Run("NewFeedbackReporterFromConfig: github_issue", func(t *testing.T) {
		cfg := foundation.Config{}
		cfg.Source.Type = "github_issue"
		reporter, err := NewFeedbackReporterFromConfig(cfg, lg)
		if err != nil {
			t.Fatal(err)
		}
		if reporter == nil {
			t.Error("reporter should not be nil")
		}
	})

	t.Run("NewFeedbackReporterFromConfig: unsupported", func(t *testing.T) {
		cfg := foundation.Config{}
		cfg.Source.Type = "gitlab"
		_, err := NewFeedbackReporterFromConfig(cfg, lg)
		if err == nil {
			t.Error("expected error for unsupported source type")
		}
	})

	t.Run("NewTaskContentFetcherFromConfig: github_issue", func(t *testing.T) {
		cfg := foundation.Config{}
		cfg.Source.Type = "github_issue"
		fetcher, err := NewTaskContentFetcherFromConfig(cfg, lg)
		if err != nil {
			t.Fatal(err)
		}
		if fetcher == nil {
			t.Error("fetcher should not be nil")
		}
	})

	t.Run("NewTaskContentFetcherFromConfig: unsupported", func(t *testing.T) {
		cfg := foundation.Config{}
		cfg.Source.Type = "trello"
		_, err := NewTaskContentFetcherFromConfig(cfg, lg)
		if err == nil {
			t.Error("expected error for unsupported source type")
		}
	})
}

func TestIsGogentsFeedbackComment(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		expect bool
	}{
		{
			name:   "with marker",
			body:   "<!-- gogents-feedback -->\n🤖 Task Syncer\nTask github-1 synced",
			expect: true,
		},
		{
			name:   "with marker and leading whitespace",
			body:   "  \n<!-- gogents-feedback -->\nSome content",
			expect: true,
		},
		{
			name:   "legacy prefix",
			body:   "🤖 Task Syncer has completed the task",
			expect: true,
		},
		{
			name:   "normal user comment",
			body:   "Please fix the bug in line 42",
			expect: false,
		},
		{
			name:   "empty body",
			body:   "",
			expect: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isGogentsFeedbackComment(tc.body)
			if got != tc.expect {
				t.Errorf("isGogentsFeedbackComment(%q) = %v, want %v", tc.body, got, tc.expect)
			}
		})
	}
}

func TestParseGitHubIssueNumber(t *testing.T) {
	cases := []struct {
		taskID string
		num    int
		ok     bool
	}{
		{"github-42", 42, true},
		{"github-1", 1, true},
		{"github-0", 0, true},
		{"invalid", 0, false},
		{"github-", 0, false},
		{"gitlab-42", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.taskID, func(t *testing.T) {
			n, ok := parseGitHubIssueNumber(tc.taskID)
			if ok != tc.ok || n != tc.num {
				t.Errorf("parseGitHubIssueNumber(%q) = (%d, %v), want (%d, %v)", tc.taskID, n, ok, tc.num, tc.ok)
			}
		})
	}
}
