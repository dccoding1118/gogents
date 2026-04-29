package foundation

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskStore(t *testing.T) {
	tmpDir := t.TempDir()
	paths := NewWorkspacePaths(tmpDir)
	_ = paths.Init()
	store := NewTaskStore(paths)

	t.Run("Put and Get", func(t *testing.T) {
		task := Task{ID: "github-1", Status: StatusQueued}
		if err := store.Put(task); err != nil {
			t.Fatal(err)
		}
		got, err := store.Get("github-1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Source.IssueNumber != 1 {
			t.Errorf("issue number extract failed: %d", got.Source.IssueNumber)
		}
	})

	t.Run("List", func(t *testing.T) {
		// Create a non-directory file to trigger skip branch in List
		_ = os.WriteFile(filepath.Join(paths.TasksDir(), "some-file"), []byte(""), 0o600)

		tasks, _ := store.List()
		if len(tasks) == 0 {
			t.Error("list empty")
		}
	})

	t.Run("Transition", func(t *testing.T) {
		now := time.Now()
		_, err := store.Transition("github-1", StatusProcessing, TaskPatch{StartedAt: &now})
		if err != nil {
			t.Fatal(err)
		}
		// Illegal: Queued -> Done
		err = store.Put(Task{ID: "github-2", Status: StatusQueued})
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.Transition("github-2", StatusDone, TaskPatch{})
		if err == nil {
			t.Error("expect error for illegal transition")
		}
	})

	t.Run("UpdateFields and Patch", func(t *testing.T) {
		url := "http"
		failCount := 1
		now := time.Now()
		cid := int64(42)

		_, err := store.UpdateFields("github-1", TaskPatch{
			PrURL:          &url,
			FailureCount:   &failCount,
			StartedAt:      &now,
			FinishedAt:     &now,
			CommentID:      &cid,
			LastLabel:      &url,
			SourceLabels:   &[]string{"A"},
			ReportingError: &url,
			Branch:         &url,
			Source:         &SourceFields{Title: "T"},
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Errors and Corrupt", func(t *testing.T) {
		_, err := store.Get("none")
		if err == nil {
			t.Error("expect error for missing file")
		}

		id := "corrupt"
		yamlFile := paths.TaskYaml(id)
		_ = os.MkdirAll(filepath.Dir(yamlFile), 0o750)
		_ = os.WriteFile(yamlFile, []byte("{"), 0o600)

		// List should skip corrupt file (log only).
		// We temporarily silence logs to avoid cluttering test output with expected errors.
		oldOutput := log.Writer()
		log.SetOutput(io.Discard)
		defer log.SetOutput(oldOutput)

		_, _ = store.List()

		_, err = store.Get(id)
		if !errors.Is(err, ErrTaskCorrupt) {
			t.Error("expect corrupt error")
		}

		// Transition NOT error on missing file because it treats as new task
		// But new task + empty 'to' might be illegal depending on implementation
		// Actually, let's test an illegal transition for a new task
		_, err = store.Transition("new-tx", StatusDone, TaskPatch{})
		if !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("expected ErrIllegalTransition for new task to StatusDone, got %v", err)
		}
	})
}
