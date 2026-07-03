package foundation

import (
	"errors"
	"fmt"
	"log"
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
// ~/.gogents/config.yaml (R12). `gogents start` calls Upsert; the observability
// CLI calls Load / Resolve. Upsert is keyed by workspace path.
type GlobalConfigStore interface {
	Load() (GlobalConfig, error)
	Upsert(reg RuntimeRegistration) error
	Resolve(workspaceOrName string) (RuntimeRegistration, error)
}

// GlobalConfigPath returns the canonical ~/.gogents/config.yaml path.
func GlobalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, ".gogents", "config.yaml")
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

	// Exact workspace path match wins (the primary key).
	for _, r := range gc.Runtimes {
		if r.Workspace == workspaceOrName {
			return r, nil
		}
	}

	// Otherwise match by name; pick the most recently registered when several
	// share a name (e.g. a workspace deleted and recreated at a new path).
	var best *RuntimeRegistration
	for i := range gc.Runtimes {
		if gc.Runtimes[i].Name != workspaceOrName {
			continue
		}
		if best == nil || gc.Runtimes[i].RegisteredAt >= best.RegisteredAt {
			best = &gc.Runtimes[i]
		}
	}
	if best != nil {
		return *best, nil
	}
	return RuntimeRegistration{}, ErrRuntimeNotRegistered
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

// atomicWrite persists the registry using write-tmp → fsync → rename.
// Caller MUST hold the lock.
func (s *globalConfigStore) atomicWrite(gc GlobalConfig) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	data, err := yaml.Marshal(gc)
	if err != nil {
		return fmt.Errorf("failed to marshal global config: %w", err)
	}

	tmpPath := filepath.Clean(fmt.Sprintf("%s.tmp.%d.%d", s.path, os.Getpid(), time.Now().UnixNano()))
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- trusted path
	if err != nil {
		return fmt.Errorf("failed to create tmp config file: %w", err)
	}

	writeOK := false
	defer func() {
		if !writeOK {
			if cErr := f.Close(); cErr != nil {
				log.Printf("[global_config] failed to close tmp file on cleanup: %v", cErr)
			}
			if rErr := os.Remove(tmpPath); rErr != nil {
				log.Printf("[global_config] failed to remove tmp file on cleanup: %v", rErr)
			}
		}
	}()

	if _, wErr := f.Write(data); wErr != nil {
		return fmt.Errorf("failed to write global config: %w", wErr)
	}
	if sErr := f.Sync(); sErr != nil {
		return fmt.Errorf("failed to sync global config: %w", sErr)
	}
	if cErr := f.Close(); cErr != nil {
		return fmt.Errorf("failed to close global config: %w", cErr)
	}
	writeOK = true

	if rErr := os.Rename(tmpPath, s.path); rErr != nil {
		return fmt.Errorf("failed to rename global config: %w", rErr)
	}
	return nil
}
