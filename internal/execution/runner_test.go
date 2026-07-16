package execution

import (
	"context"
	"testing"

	"github.com/dccoding1118/gogents/internal/agent"
	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/internal/logger"
	"github.com/dccoding1118/gogents/internal/source"
)

// --- mocks for full-Run tests ---

type mockFetcher struct{ content source.TaskContent }

func (m *mockFetcher) Fetch(_ context.Context, _ string) (source.TaskContent, error) {
	return m.content, nil
}

type mockBranchGit struct{}

func (m *mockBranchGit) Fetch(_ context.Context, _ string) error                { return nil }
func (m *mockBranchGit) CheckoutBranch(_ context.Context, _, _, _ string) error { return nil }

type mockAgent struct{ lastWorkDir string }

func (m *mockAgent) Run(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
	m.lastWorkDir = req.WorkDir
	return agent.RunResult{ExitCode: 0}, nil
}

type mockFeedback struct{}

func (m *mockFeedback) Report(_ context.Context, _ source.FeedbackInput) source.FeedbackOutcome {
	return source.FeedbackOutcome{Result: source.FeedbackSuccess, CommentID: 1, LastLabel: "gogents:done"}
}

type mockVerifier struct {
	spec source.VerifySpec
	ok   bool
}

func (m *mockVerifier) VerifySpec(_ context.Context, _ string) (source.VerifySpec, bool, error) {
	return m.spec, m.ok, nil
}

// runnerHarness builds a runner over a real workspace/store with mocks and
// returns it plus captured runtime_state transitions.
func runnerHarness(t *testing.T, verifier source.TaskVerifier) (*runner, foundation.TaskStore, *mockAgent, *[]foundation.RuntimeState) {
	t.Helper()
	ws := t.TempDir()
	paths := foundation.NewWorkspacePaths(ws)
	if err := paths.Init(); err != nil {
		t.Fatal(err)
	}
	store := foundation.NewTaskStore(paths)
	// Seed a queued task so Step 1's queued→processing transition is legal.
	if err := store.Put(foundation.Task{ID: "t-1", Status: foundation.StatusQueued}); err != nil {
		t.Fatal(err)
	}

	var cfg foundation.Config
	cfg.Project.Repo.URL = "https://github.com/u/myrepo"
	cfg.Agent.Model = "m"
	cfg.TaskPolicy.MaxFailures = 3

	ag := &mockAgent{}
	var states []foundation.RuntimeState
	r := New(Deps{
		Store:           store,
		Paths:           paths,
		Config:          cfg,
		Logger:          &mockLogger{},
		ContentFetcher:  &mockFetcher{content: source.TaskContent{Title: "t", Body: "b"}},
		Verifier:        verifier,
		Git:             &mockBranchGit{},
		Agent:           ag,
		Feedback:        &mockFeedback{},
		SetRuntimeState: func(w foundation.RuntimeState) { states = append(states, w) },
	}).(*runner)
	return r, store, ag, &states
}

func hasState(states []foundation.RuntimeState, want foundation.RuntimeState) bool {
	for _, s := range states {
		if s == want {
			return true
		}
	}
	return false
}

func TestRunVerify(t *testing.T) {
	if code := runVerify(context.Background(), t.TempDir(), "exit 0"); code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
	if code := runVerify(context.Background(), t.TempDir(), "exit 7"); code != 7 {
		t.Errorf("expected 7, got %d", code)
	}
}

func TestRunner_WorkDirIsWorkspaceRoot(t *testing.T) {
	// D03-12: agent cwd is the workspace root, not the repo dir.
	r, _, ag, _ := runnerHarness(t, &mockVerifier{ok: false})
	_ = r.Run(context.Background(), foundation.Task{ID: "t-1", Status: foundation.StatusQueued})
	if ag.lastWorkDir != r.deps.Paths.Root() {
		t.Errorf("expected WorkDir=%s, got %s", r.deps.Paths.Root(), ag.lastWorkDir)
	}
}

func TestRunner_VerifyPass(t *testing.T) {
	// D03-13: ticket verify passes → task done; runtime_state visited verifying.
	r, store, _, states := runnerHarness(t, &mockVerifier{ok: true, spec: source.VerifySpec{Cmd: "exit 0"}})
	if err := r.Run(context.Background(), foundation.Task{ID: "t-1", Status: foundation.StatusQueued}); err != nil {
		t.Fatalf("run: %v", err)
	}
	task, _ := store.Get("t-1")
	if task.Status != foundation.StatusDone {
		t.Errorf("expected done, got %s", task.Status)
	}
	if !hasState(*states, foundation.RuntimeVerifying) {
		t.Errorf("expected verifying in %v", *states)
	}
	if !hasState(*states, foundation.RuntimeReporting) {
		t.Errorf("expected reporting in %v", *states)
	}
}

func TestRunner_VerifyFail(t *testing.T) {
	// D03-14: ticket verify fails → task failed, failure_count +1.
	r, store, _, states := runnerHarness(t, &mockVerifier{ok: true, spec: source.VerifySpec{Cmd: "exit 3"}})
	_ = r.Run(context.Background(), foundation.Task{ID: "t-1", Status: foundation.StatusQueued})
	task, _ := store.Get("t-1")
	if task.Status != foundation.StatusFailed {
		t.Errorf("expected failed, got %s", task.Status)
	}
	if task.Execution.FailureCount != 1 {
		t.Errorf("expected failure_count 1, got %d", task.Execution.FailureCount)
	}
	if !hasState(*states, foundation.RuntimeVerifying) {
		t.Errorf("expected verifying in %v", *states)
	}
}

func TestRunner_NoVerifyFallback(t *testing.T) {
	// D03-15: no ticket verify → result.json verdict path. With no result.json,
	// the fallback judge yields Missing → failed (never enters verifying).
	r, store, _, states := runnerHarness(t, &mockVerifier{ok: false})
	_ = r.Run(context.Background(), foundation.Task{ID: "t-1", Status: foundation.StatusQueued})
	task, _ := store.Get("t-1")
	if task.Status != foundation.StatusFailed {
		t.Errorf("expected failed (no result.json), got %s", task.Status)
	}
	if hasState(*states, foundation.RuntimeVerifying) {
		t.Errorf("should not enter verifying without a ticket verify: %v", *states)
	}
}

type mockLogger struct{}

func (m *mockLogger) Info(event, taskID, msg string, fields map[string]any)                    {}
func (m *mockLogger) Warning(event, taskID, msg string, fields map[string]any)                 {}
func (m *mockLogger) Error(event, taskID, msg string, fields map[string]any)                   {}
func (m *mockLogger) Log(level logger.Level, event, taskID, msg string, fields map[string]any) {}
func (m *mockLogger) Close() error                                                             { return nil }

func TestNoopExecutor(t *testing.T) {
	lg := &mockLogger{}
	exec := NewNoopExecutor(lg)
	err := exec.Run(context.Background(), foundation.Task{ID: "test-1"})
	if err != nil {
		t.Errorf("noop executor should not fail: %v", err)
	}
}

func TestHelpers(t *testing.T) {
	t.Run("truncate", func(t *testing.T) {
		if truncate("12345", 3) != "123..." {
			t.Error("truncate fail")
		}
		if truncate("12", 5) != "12" {
			t.Error("short truncate fail")
		}
	})

	t.Run("parseIssueNumber", func(t *testing.T) {
		if n, ok := parseIssueNumber("github-42"); !ok || n != 42 {
			t.Error("parse fail")
		}
		if _, ok := parseIssueNumber("bad"); ok {
			t.Error("should fail")
		}
	})

	t.Run("computeUpdatedLabels", func(t *testing.T) {
		existing := []string{"bug", "gogents:queued"}
		updated := computeUpdatedLabels(existing, "gogents:processing")
		if len(updated) != 2 || updated[0] != "bug" || updated[1] != "gogents:processing" {
			t.Errorf("compute labels fail: %v", updated)
		}
	})
}
