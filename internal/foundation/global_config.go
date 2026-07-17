package foundation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrRuntimeNotRegistered is returned by Resolve when no registered runtime
// matches the given workspace path or name.
var ErrRuntimeNotRegistered = errors.New("runtime not found in ~/.gogents/config.yaml")

// GlobalConfigStore reads and maintains the runtime registry at
// <global-home>/config.yaml (R12). `gogents start` calls Upsert; the
// observability CLI calls Load / Resolve; `gogents runtime remove` calls Remove.
// Upsert and Remove are keyed by workspace path (see Resolve's matching rule).
type GlobalConfigStore interface {
	Load() (GlobalConfig, error)
	Upsert(reg RuntimeRegistration) error
	Resolve(workspaceOrName string) (RuntimeRegistration, error)
	Remove(workspaceOrName string) error
}

// GlobalConfigPath returns the config.yaml path under the global home root
// (GlobalHome; overridable via GOGENTS_HOME).
func GlobalConfigPath() string {
	return filepath.Join(GlobalHome(), "config.yaml")
}

type globalConfigStore struct {
	path string
	mu   sync.Mutex
}

// NewGlobalConfigStore returns a store bound to ~/.gogents/config.yaml.
func NewGlobalConfigStore() GlobalConfigStore {
	return &globalConfigStore{path: GlobalConfigPath()}
}

// NewGlobalConfigStoreAt returns a store bound to an explicit path (tests).
func NewGlobalConfigStoreAt(path string) GlobalConfigStore {
	return &globalConfigStore{path: path}
}

func (s *globalConfigStore) Load() (GlobalConfig, error) {
	var gc GlobalConfig
	data, err := os.ReadFile(s.path) //nolint:gosec // path is ~/.gogents/config.yaml
	if err != nil {
		if os.IsNotExist(err) {
			return gc, nil
		}
		return gc, fmt.Errorf("failed to read global config: %w", err)
	}
	if uErr := yaml.Unmarshal(data, &gc); uErr != nil {
		return gc, fmt.Errorf("%w: failed to parse global config: %v", ErrConfigParse, uErr)
	}
	return gc, nil
}

func (s *globalConfigStore) Upsert(reg RuntimeRegistration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	gc, err := s.loadUnlocked()
	if err != nil {
		return err
	}

	if reg.RegisteredAt == "" {
		reg.RegisteredAt = Now().Format(time.RFC3339)
	}

	replaced := false
	for i := range gc.Runtimes {
		if gc.Runtimes[i].Workspace == reg.Workspace {
			gc.Runtimes[i] = reg
			replaced = true
			break
		}
	}
	if !replaced {
		gc.Runtimes = append(gc.Runtimes, reg)
	}

	return s.atomicWrite(gc)
}

func (s *globalConfigStore) Resolve(workspaceOrName string) (RuntimeRegistration, error) {
	gc, err := s.Load()
	if err != nil {
		return RuntimeRegistration{}, err
	}
	idx := matchRuntimeIndex(gc.Runtimes, workspaceOrName)
	if idx < 0 {
		return RuntimeRegistration{}, ErrRuntimeNotRegistered
	}
	return gc.Runtimes[idx], nil
}

func (s *globalConfigStore) Remove(workspaceOrName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	gc, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	idx := matchRuntimeIndex(gc.Runtimes, workspaceOrName)
	if idx < 0 {
		return ErrRuntimeNotRegistered
	}
	gc.Runtimes = append(gc.Runtimes[:idx], gc.Runtimes[idx+1:]...)
	return s.atomicWrite(gc)
}

// matchRuntimeIndex applies the registry lookup rule shared by Resolve and
// Remove: an exact workspace-path match wins (the primary key); otherwise match
// by name, picking the most recently registered when several share a name (e.g.
// a workspace deleted and recreated at a new path). Returns -1 when no match.
func matchRuntimeIndex(runtimes []RuntimeRegistration, workspaceOrName string) int {
	for i := range runtimes {
		if runtimes[i].Workspace == workspaceOrName {
			return i
		}
	}
	best := -1
	for i := range runtimes {
		if runtimes[i].Name != workspaceOrName {
			continue
		}
		if best < 0 || runtimes[i].RegisteredAt >= runtimes[best].RegisteredAt {
			best = i
		}
	}
	return best
}

func (s *globalConfigStore) loadUnlocked() (GlobalConfig, error) {
	var gc GlobalConfig
	data, err := os.ReadFile(s.path) //nolint:gosec // trusted path
	if err != nil {
		if os.IsNotExist(err) {
			return gc, nil
		}
		return gc, fmt.Errorf("failed to read global config: %w", err)
	}
	if uErr := yaml.Unmarshal(data, &gc); uErr != nil {
		return gc, fmt.Errorf("%w: failed to parse global config: %v", ErrConfigParse, uErr)
	}
	return gc, nil
}

// atomicWrite persists the registry through the shared AtomicWrite helper.
// Caller MUST hold the lock.
func (s *globalConfigStore) atomicWrite(gc GlobalConfig) error {
	data, err := yaml.Marshal(gc)
	if err != nil {
		return fmt.Errorf("failed to marshal global config: %w", err)
	}
	if wErr := AtomicWrite(s.path, data, 0o600); wErr != nil {
		return fmt.Errorf("failed to persist global config: %w", wErr)
	}
	return nil
}
