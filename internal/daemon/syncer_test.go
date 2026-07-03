package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/logger"
	"github.com/dccoding/gogents/internal/source"
)

type mockSource struct {
	listRes []source.SourceTask
	listErr error
	getRes  source.SourceTask
	getErr  map[string]error
}

func (m *mockSource) List(ctx context.Context, filter source.SourceFilter) ([]source.SourceTask, error) {
	return m.listRes, m.listErr
}

func (m *mockSource) Get(ctx context.Context, id string) (source.SourceTask, error) {
	if err, ok := m.getErr[id]; ok {
		return source.SourceTask{}, err
	}
	return m.getRes, nil
}
func (m *mockSource) Kind() string { return "mock" }

type mockFeedback struct {
	reportRes source.FeedbackOutcome
}

// Compile-time interface assertion (prevents unused lint while reserving the mock for future tests).
var _ source.FeedbackReporter = (*mockFeedback)(nil)

func (m *mockFeedback) Report(ctx context.Context, input source.FeedbackInput) source.FeedbackOutcome {
	return m.reportRes
}

type mockLogger struct{}

func (m *mockLogger) Info(event, taskID, msg string, fields map[string]any) {}

func (m *mockLogger) Warning(event, taskID, msg string, fields map[string]any) {}

func (m *mockLogger) Error(event, taskID, msg string, fields map[string]any) {}

func (m *mockLogger) Log(level logger.Level, event, taskID, message string, fields map[string]any) {}

func (m *mockLogger) Close() error { return nil }

type mockStore struct {
	tasks []foundation.Task
}

func (m *mockStore) List() ([]foundation.Task, error) { return m.tasks, nil }
func (m *mockStore) Get(id string) (foundation.Task, error) {
	for _, t := range m.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return foundation.Task{}, errors.New("not found")
}

func (m *mockStore) Put(t foundation.Task) error {
	m.tasks = append(m.tasks, t)
	return nil
}

func (m *mockStore) Transition(id string, to foundation.Status, patch foundation.TaskPatch) (foundation.Task, error) {
	for i, t := range m.tasks {
		if t.ID == id {
			m.tasks[i].Status = to
			return m.tasks[i], nil
		}
	}
	return foundation.Task{}, errors.New("not found")
}

func (m *mockStore) UpdateFields(id string, patch foundation.TaskPatch) (foundation.Task, error) {
	return foundation.Task{}, nil
}

func TestSyncer_Tick(t *testing.T) {
	lg := &mockLogger{}
	cfg := foundation.Config{}
	cfg.Source.Filter.Label = "queued"

	t.Run("D02-01 Create new task", func(t *testing.T) {
		src := &mockSource{
			listRes: []source.SourceTask{
				{TaskID: "github-1", Title: "T1", State: source.SourceStateOpen},
			},
		}
		store := &mockStore{}
		syncer := NewSyncer(store, src, nil, lg, cfg)

		outcome, err := syncer.Tick(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(outcome.Created) != 1 || outcome.Created[0] != "github-1" {
			t.Errorf("expected 1 created task, got %v", outcome.Created)
		}
		if len(store.tasks) != 1 {
			t.Errorf("expected 1 task in store, got %d", len(store.tasks))
		}
	})

	t.Run("D02-02 Task deleted from source", func(t *testing.T) {
		src := &mockSource{
			getErr: map[string]error{"github-1": source.ErrSourceNotFound},
		}
		store := &mockStore{
			tasks: []foundation.Task{
				{ID: "github-1", Status: foundation.StatusQueued},
			},
		}
		syncer := NewSyncer(store, src, nil, lg, cfg)

		outcome, err := syncer.Tick(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(outcome.Deleted) != 1 {
			t.Errorf("expected 1 deleted, got %v", outcome.Deleted)
		}
		if store.tasks[0].Status != foundation.StatusDeleted {
			t.Errorf("expected status deleted, got %s", store.tasks[0].Status)
		}
	})

	t.Run("D02-03 Source update but keep status", func(t *testing.T) {
		updatedAt := time.Now()
		src := &mockSource{
			getRes: source.SourceTask{
				TaskID:    "github-1",
				State:     source.SourceStateOpen,
				UpdatedAt: updatedAt,
			},
		}
		store := &mockStore{
			tasks: []foundation.Task{
				{
					ID:     "github-1",
					Status: foundation.StatusQueued,
					Source: foundation.SourceFields{UpdatedAt: updatedAt.Add(-time.Hour), State: "open"},
				},
			},
		}
		syncer := NewSyncer(store, src, nil, lg, cfg)

		outcome, err := syncer.Tick(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Unchanged > 0 {
			t.Errorf("expected task updated, but got unchanged")
		}
	})

	t.Run("D02-04 Close processing task", func(t *testing.T) {
		src := &mockSource{
			getRes: source.SourceTask{
				TaskID: "github-1",
				State:  source.SourceStateClosed,
			},
		}
		store := &mockStore{
			tasks: []foundation.Task{
				{ID: "github-1", Status: foundation.StatusProcessing},
			},
		}
		syncer := NewSyncer(store, src, nil, lg, cfg)

		_, _ = syncer.Tick(context.Background())
		if store.tasks[0].Status != foundation.StatusDone {
			t.Errorf("expected status done, got %s", store.tasks[0].Status)
		}
	})
}

func TestApplySourceTransition(t *testing.T) {
	t.Run("D02-07 Logic rules", func(t *testing.T) {
		cases := []struct {
			local  foundation.Status
			remote source.SourceTask
			fails  int
			expect foundation.Status
		}{
			{foundation.StatusProcessing, source.SourceTask{State: source.SourceStateClosed}, 0, foundation.StatusDone},
			{foundation.StatusDone, source.SourceTask{Labels: []string{source.LabelQueued}}, 0, foundation.StatusQueued},
			{foundation.StatusFailed, source.SourceTask{Labels: []string{source.LabelQueued}}, 1, foundation.StatusQueued},
			{foundation.StatusFailed, source.SourceTask{Labels: []string{source.LabelQueued}}, 5, foundation.StatusFailed}, // over max
		}

		for _, tc := range cases {
			got := applySourceTransition(tc.local, tc.remote, tc.fails, 3)
			if got != tc.expect {
				t.Errorf("local %s, remote state %s -> expect %s, got %s", tc.local, tc.remote.State, tc.expect, got)
			}
		}
	})
}

func TestSyncer_Helpers(t *testing.T) {
	t.Run("parseIssueNumberFromTaskID", func(t *testing.T) {
		if n := parseIssueNumberFromTaskID("github-42"); n != 42 {
			t.Errorf("expect 42, got %d", n)
		}
		if n := parseIssueNumberFromTaskID("invalid"); n != 0 {
			t.Errorf("expect 0, got %d", n)
		}
	})

	t.Run("pickNextTask", func(t *testing.T) {
		tasks := []foundation.Task{
			{ID: "B", Status: foundation.StatusQueued},
			{ID: "A", Status: foundation.StatusQueued},
			{ID: "C", Status: foundation.StatusProcessing},
		}
		pick := pickNextTask(tasks)
		if pick == nil || pick.ID != "A" {
			t.Errorf("expected A, got %v", pick)
		}
	})
}
