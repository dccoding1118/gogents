package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dccoding/gogents/internal/daemon"
	"github.com/dccoding/gogents/internal/foundation"
)

func newRegistry(t *testing.T) (foundation.GlobalConfigStore, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	return foundation.NewGlobalConfigStoreAt(cfgPath), cfgPath
}

// writeState creates <ws>/.gogents/state.yaml with the given pid/runtime_state.
func writeState(t *testing.T, ws string, pid int, ws2 foundation.RuntimeState, currentTask string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(ws, ".gogents"), 0o750); err != nil {
		t.Fatal(err)
	}
	ss := daemon.NewStateStore(filepath.Join(ws, ".gogents", "state.yaml"))
	var st daemon.State
	st.Daemon.PID = pid
	st.RuntimeState = ws2
	st.CurrentTask.TaskID = currentTask
	st.Sync.LastSyncStatus = "success"
	if err := ss.Save(st); err != nil {
		t.Fatal(err)
	}
}

func TestList(t *testing.T) {
	gcfg, _ := newRegistry(t)
	_ = gcfg.Upsert(foundation.RuntimeRegistration{Name: "w1", Workspace: "/ws/1", RegisteredAt: "2026-01-01T00:00:00Z"})
	_ = gcfg.Upsert(foundation.RuntimeRegistration{Name: "w2", Workspace: "/ws/2", RegisteredAt: "2026-02-01T00:00:00Z"})

	views, err := List(gcfg)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("D08-04 expected 2 runtimes, got %d", len(views))
	}
}

func TestList_Empty(t *testing.T) {
	gcfg, _ := newRegistry(t)
	views, err := List(gcfg)
	if err != nil || len(views) != 0 {
		t.Fatalf("D08-04 empty registry: %v %d", err, len(views))
	}
}

func TestStatus_Alive(t *testing.T) {
	gcfg, _ := newRegistry(t)
	ws := t.TempDir()
	writeState(t, ws, os.Getpid(), foundation.RuntimeExecuting, "t-1")
	_ = gcfg.Upsert(foundation.RuntimeRegistration{Name: "w", Workspace: ws, RegisteredAt: "2026-01-01T00:00:00Z"})

	view, err := Status(gcfg, ws)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !view.Alive {
		t.Errorf("D08-05 expected alive=true for current process pid")
	}
	if view.RuntimeState != "executing" {
		t.Errorf("D08-05 expected runtime_state executing, got %s", view.RuntimeState)
	}
	if view.CurrentTask != "t-1" {
		t.Errorf("current_task mismatch: %s", view.CurrentTask)
	}
}

func TestStatus_Down(t *testing.T) {
	gcfg, _ := newRegistry(t)
	ws := t.TempDir()
	// pid 0 is never a live process → reported down regardless of stored state.
	writeState(t, ws, 0, foundation.RuntimeExecuting, "t-1")
	_ = gcfg.Upsert(foundation.RuntimeRegistration{Name: "w", Workspace: ws, RegisteredAt: "2026-01-01T00:00:00Z"})

	view, err := Status(gcfg, ws)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if view.Alive {
		t.Errorf("D08-06 expected alive=false")
	}
	if view.RuntimeState != "down" {
		t.Errorf("D08-06 expected runtime_state down, got %s", view.RuntimeState)
	}
}

func TestStatus_NotRegistered(t *testing.T) {
	gcfg, _ := newRegistry(t)
	_, err := Status(gcfg, "nope")
	if err == nil {
		t.Fatal("D08-07 expected error for unregistered target")
	}
}

func TestStatus_NoStateYaml(t *testing.T) {
	gcfg, _ := newRegistry(t)
	ws := t.TempDir() // registered but never started (no state.yaml)
	_ = gcfg.Upsert(foundation.RuntimeRegistration{Name: "w", Workspace: ws, RegisteredAt: "2026-01-01T00:00:00Z"})
	if _, err := Status(gcfg, ws); err == nil {
		t.Fatal("expected error when state.yaml is missing")
	}
}

func TestLogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	gcfg, _ := newRegistry(t)
	ws := t.TempDir()
	paths := foundation.NewWorkspacePaths(ws)
	if err := os.MkdirAll(paths.LogsDir(), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.DaemonLog(), []byte("log line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = gcfg.Upsert(foundation.RuntimeRegistration{Name: "w", Workspace: ws, RegisteredAt: "2026-01-01T00:00:00Z"})

	t.Run("D08-09 daemon log", func(t *testing.T) {
		p, err := Logs(gcfg, ws, false)
		if err != nil || p != paths.DaemonLog() {
			t.Fatalf("logs: %v path=%s", err, p)
		}
	})

	t.Run("D08-09 missing target", func(t *testing.T) {
		if _, err := Logs(gcfg, "nope", false); err == nil {
			t.Fatal("expected error for unregistered target")
		}
	})
}
