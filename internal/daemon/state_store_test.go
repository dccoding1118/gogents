package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dccoding/gogents/internal/foundation"
)

func TestStateStore(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.yaml")
	store := NewStateStore(path)

	t.Run("D02-11 Load/Save lifecycle", func(t *testing.T) {
		// Load non-existent
		st, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if st.Daemon.PID != 0 {
			t.Errorf("expected empty PID, got %d", st.Daemon.PID)
		}

		// Save
		st.Daemon.PID = 1234
		st.Daemon.StartedAt = time.Now()
		err = store.Save(st)
		if err != nil {
			t.Fatal(err)
		}

		// Load back
		st2, _ := store.Load()
		if st2.Daemon.PID != 1234 {
			t.Errorf("load failed, got PID %d", st2.Daemon.PID)
		}
	})

	t.Run("UpdateSync helpers", func(t *testing.T) {
		now := time.Now()
		err := store.UpdateSync(func(s *SyncState) {
			s.LastSyncStatus = "success"
			s.LastSyncAt = &now
		})
		if err != nil {
			t.Fatal(err)
		}

		st, _ := store.Load()
		if st.Sync.LastSyncStatus != "success" {
			t.Error("update sync failed")
		}
	})

	t.Run("UpdateCurrentTask helpers", func(t *testing.T) {
		err := store.UpdateCurrentTask(func(ct *CurrentTask) {
			ct.TaskID = "t-1"
		})
		if err != nil {
			t.Fatal(err)
		}

		st, _ := store.Load()
		if st.CurrentTask.TaskID != "t-1" {
			t.Error("update task failed")
		}
	})

	t.Run("D02-14 SetRuntimeState", func(t *testing.T) {
		if err := store.SetRuntimeState(foundation.RuntimeVerifying); err != nil {
			t.Fatal(err)
		}
		st, _ := store.Load()
		if st.RuntimeState != foundation.RuntimeVerifying {
			t.Errorf("expected verifying, got %s", st.RuntimeState)
		}
		// Ensure it does not clobber unrelated fields.
		if st.CurrentTask.TaskID != "t-1" {
			t.Errorf("SetRuntimeState clobbered current_task: %s", st.CurrentTask.TaskID)
		}
	})

	t.Run("atomicWrite corruption prevention", func(t *testing.T) {
		// Mock a dir with no permission (hard on linux, but we can try a file as path)
		// Or just manually call atomicWrite with bad path
		s := defaultStateStore{path: "/invalid/path/state.yaml"}
		err := s.atomicWrite(State{})
		if err == nil {
			t.Error("expect error for invalid path")
		}
	})
}
