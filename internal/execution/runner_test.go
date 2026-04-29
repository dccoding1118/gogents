package execution

import (
	"context"
	"testing"

	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/logger"
)

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
