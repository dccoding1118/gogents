package cli

// query.go holds the read-only observability logic behind `gogents runtime
// list/status` and `gogents logs` (R12 / D08). These functions never modify
// config.yaml, state.yaml, or task.yaml.

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/dccoding/gogents/internal/daemon"
	"github.com/dccoding/gogents/internal/foundation"
)

// RuntimeView is one row of `runtime list`.
type RuntimeView struct {
	Name         string
	Workspace    string
	RegisteredAt string
}

// StatusView is the output of `runtime status`.
type StatusView struct {
	RuntimeView
	RuntimeState   string // state.yaml value; "down" when the PID is not alive.
	Pid            int
	Alive          bool
	CurrentTask    string
	LastSyncAt     time.Time
	LastSyncStatus string
}

// List returns all registered runtimes (R12 B2).
func List(gcfg foundation.GlobalConfigStore) ([]RuntimeView, error) {
	gc, err := gcfg.Load()
	if err != nil {
		return nil, err
	}
	out := make([]RuntimeView, 0, len(gc.Runtimes))
	for _, r := range gc.Runtimes {
		out = append(out, RuntimeView{Name: r.Name, Workspace: r.Workspace, RegisteredAt: r.RegisteredAt})
	}
	return out, nil
}

// Status resolves the target to a runtime, reads its state.yaml, and checks the
// recorded PID. A PID that is no longer alive is reported as runtime_state=down
// regardless of the persisted value (R12 B3).
func Status(gcfg foundation.GlobalConfigStore, target string) (StatusView, error) {
	reg, err := gcfg.Resolve(target)
	if err != nil {
		return StatusView{}, err
	}

	paths := foundation.NewWorkspacePaths(reg.Workspace)
	statePath := paths.StateYaml()
	if _, sErr := os.Stat(statePath); sErr != nil {
		return StatusView{}, fmt.Errorf("runtime %q registered but state.yaml not found (never started?): %s", target, statePath)
	}

	st, err := daemon.NewStateStore(statePath).Load()
	if err != nil {
		return StatusView{}, err
	}

	alive := pidAlive(st.Daemon.PID)
	view := StatusView{
		RuntimeView:    RuntimeView{Name: reg.Name, Workspace: reg.Workspace, RegisteredAt: reg.RegisteredAt},
		RuntimeState:   string(st.RuntimeState),
		Pid:            st.Daemon.PID,
		Alive:          alive,
		CurrentTask:    st.CurrentTask.TaskID,
		LastSyncStatus: st.Sync.LastSyncStatus,
	}
	if st.Sync.LastSyncAt != nil {
		view.LastSyncAt = *st.Sync.LastSyncAt
	}
	switch {
	case !alive:
		view.RuntimeState = "down"
	case view.RuntimeState == "":
		view.RuntimeState = string(foundation.RuntimeIdle)
	}
	return view, nil
}

// Logs resolves the target and returns the path to its daemon.jsonl (or
// error.jsonl when errOnly) for the caller to stream (R12 B4).
func Logs(gcfg foundation.GlobalConfigStore, target string, errOnly bool) (string, error) {
	reg, err := gcfg.Resolve(target)
	if err != nil {
		return "", err
	}
	paths := foundation.NewWorkspacePaths(reg.Workspace)
	p := paths.DaemonLog()
	if errOnly {
		p = paths.ErrorLog()
	}
	if _, sErr := os.Stat(p); sErr != nil {
		return "", fmt.Errorf("no logs for runtime %q: %w", target, sErr)
	}
	return p, nil
}

// pidAlive reports whether a process with the given PID exists. It does not
// verify the process is actually a gogents daemon (PID reuse is rare; R12 §3.3).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means the process exists but is owned by another user.
	return errors.Is(err, syscall.EPERM)
}
