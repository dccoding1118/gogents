package cli

// query.go holds the read-only observability logic behind `gogents runtime
// list/status` and `gogents logs` (R12 / D08). These functions never modify
// config.yaml, state.yaml, or task.yaml.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/dccoding1118/gogents/internal/daemon"
	"github.com/dccoding1118/gogents/internal/foundation"
)

// ErrStateNotFound is returned by Status when the target is registered but has
// no state.yaml (never started). The --json layer maps it to code
// "state_not_found" (D08 §3.4).
var ErrStateNotFound = errors.New("state.yaml not found")

// RuntimeView is one row of `runtime list`. The json tags are the stable
// machine-readable contract consumed by external adapters (D08 §3.4).
type RuntimeView struct {
	Name         string `json:"name"`
	Workspace    string `json:"workspace"`
	RegisteredAt string `json:"registered_at"` // RFC3339 string
}

// StatusView is the output of `runtime status`. Its JSON form is emitted via a
// custom MarshalJSON so that a zero LastSyncAt renders as "" rather than
// 0001-01-01T… (D08 §3.4).
type StatusView struct {
	RuntimeView
	RuntimeState   string // state.yaml value; "down" when the PID is not alive.
	Pid            int
	Alive          bool
	CurrentTask    string
	LastSyncAt     time.Time
	LastSyncStatus string
}

// MarshalJSON renders StatusView as the flat runtime-status contract. LastSyncAt
// is RFC3339, or "" when zero (never synced).
func (s StatusView) MarshalJSON() ([]byte, error) {
	lastSync := ""
	if !s.LastSyncAt.IsZero() {
		lastSync = s.LastSyncAt.Format(time.RFC3339)
	}
	return json.Marshal(struct {
		Name           string `json:"name"`
		Workspace      string `json:"workspace"`
		RegisteredAt   string `json:"registered_at"`
		RuntimeState   string `json:"runtime_state"`
		Pid            int    `json:"pid"`
		Alive          bool   `json:"alive"`
		CurrentTask    string `json:"current_task"`
		LastSyncAt     string `json:"last_sync_at"`
		LastSyncStatus string `json:"last_sync_status"`
	}{
		Name:           s.Name,
		Workspace:      s.Workspace,
		RegisteredAt:   s.RegisteredAt,
		RuntimeState:   s.RuntimeState,
		Pid:            s.Pid,
		Alive:          s.Alive,
		CurrentTask:    s.CurrentTask,
		LastSyncAt:     lastSync,
		LastSyncStatus: s.LastSyncStatus,
	})
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
		return StatusView{}, fmt.Errorf("runtime %q registered but state.yaml not found (never started?): %s: %w", target, statePath, ErrStateNotFound)
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
