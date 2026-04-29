package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// StateStore provides atomic read/write for state.yaml (D02 §4.5).
// Style matches D01 TaskStore (write-tmp → fsync → rename).
type StateStore interface {
	Load() (State, error)
	Save(s State) error
	UpdateSync(fn func(*SyncState)) error
	UpdateCurrentTask(fn func(*CurrentTask)) error
}

// State is the top-level structure of state.yaml.
type State struct {
	Daemon      DaemonState `yaml:"daemon"`
	Sync        SyncState   `yaml:"sync"`
	CurrentTask CurrentTask `yaml:"current_task"`
}

// DaemonState holds daemon process metadata.
type DaemonState struct {
	PID       int       `yaml:"pid"`
	StartedAt time.Time `yaml:"started_at"`
	Workspace string    `yaml:"workspace"`
}

// SyncState holds the latest sync result.
type SyncState struct {
	LastSyncAt     *time.Time `yaml:"last_sync_at,omitempty"`
	LastSyncStatus string     `yaml:"last_sync_status,omitempty"` // "success" or "failed"
	LastSyncError  string     `yaml:"last_sync_error,omitempty"`
}

// CurrentTask holds the currently executing task info.
type CurrentTask struct {
	TaskID    string     `yaml:"task_id,omitempty"`
	StartedAt *time.Time `yaml:"started_at,omitempty"`
}

type defaultStateStore struct {
	path string
	mu   sync.Mutex
}

// NewStateStore creates a StateStore that reads/writes the given file path.
func NewStateStore(path string) StateStore {
	return &defaultStateStore{path: path}
}

func (s *defaultStateStore) Load() (State, error) {
	var state State
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil // zero value + no error per §4.5
		}
		return state, fmt.Errorf("failed to read state.yaml: %w", err)
	}

	if uErr := yaml.Unmarshal(data, &state); uErr != nil {
		return state, fmt.Errorf("failed to parse state.yaml: %w", uErr)
	}
	return state, nil
}

func (s *defaultStateStore) Save(st State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.atomicWrite(st)
}

func (s *defaultStateStore) UpdateSync(fn func(*SyncState)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	fn(&st.Sync)
	return s.atomicWrite(st)
}

func (s *defaultStateStore) UpdateCurrentTask(fn func(*CurrentTask)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	fn(&st.CurrentTask)
	return s.atomicWrite(st)
}

// loadUnlocked reads state without acquiring the mutex. Caller MUST hold the lock.
func (s *defaultStateStore) loadUnlocked() (State, error) {
	var state State
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, fmt.Errorf("failed to read state.yaml: %w", err)
	}

	if uErr := yaml.Unmarshal(data, &state); uErr != nil {
		return state, fmt.Errorf("failed to parse state.yaml: %w", uErr)
	}
	return state, nil
}

// atomicWrite persists state using write-tmp → fsync → rename pattern.
// Caller MUST hold the lock.
func (s *defaultStateStore) atomicWrite(st State) error {
	data, err := yaml.Marshal(st)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	tmpPath := filepath.Clean(fmt.Sprintf("%s.tmp.%d.%d", s.path, os.Getpid(), time.Now().UnixNano()))

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- trusted path
	if err != nil {
		return fmt.Errorf("failed to create tmp state file: %w", err)
	}

	writeOK := false
	defer func() {
		if !writeOK {
			if cErr := f.Close(); cErr != nil {
				log.Printf("[state_store] failed to close tmp file on cleanup: %v", cErr)
			}
			if rErr := os.Remove(tmpPath); rErr != nil {
				log.Printf("[state_store] failed to remove tmp file on cleanup: %v", rErr)
			}
		}
	}()

	if _, wErr := f.Write(data); wErr != nil {
		return fmt.Errorf("failed to write state: %w", wErr)
	}
	if sErr := f.Sync(); sErr != nil {
		return fmt.Errorf("failed to sync state: %w", sErr)
	}
	if cErr := f.Close(); cErr != nil {
		return fmt.Errorf("failed to close state file: %w", cErr)
	}
	writeOK = true

	if rErr := os.Rename(tmpPath, s.path); rErr != nil {
		return fmt.Errorf("failed to rename state file: %w", rErr)
	}

	// Best-effort dir sync for metadata durability.
	dir := filepath.Dir(s.path)
	cleanDir := filepath.Clean(dir)
	dirF, dErr := os.Open(cleanDir) // #nosec G304 -- trusted path
	if dErr == nil {
		_ = dirF.Sync()
		if cErr := dirF.Close(); cErr != nil {
			log.Printf("[state_store] failed to close dir handle: %v", cErr)
		}
	}

	return nil
}
