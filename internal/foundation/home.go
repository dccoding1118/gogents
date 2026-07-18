package foundation

import (
	"os"
	"path/filepath"
)

// GlobalHome resolves the global home root — the single source of truth shared
// by WorkspacePaths (config.yaml, logs/) and GlobalConfigStore. Keeping the
// resolution in one place prevents the registry and the observability paths from
// drifting apart when the root is overridden. Resolution order:
//
//  1. $GOGENTS_HOME if non-empty (lets dev/prod hold independent registries and
//     logs on one machine);
//  2. else $HOME/.gogents (default, backward compatible);
//  3. else /tmp/.gogents (when $HOME is unresolvable).
//
// The override moves the global home root only; per-workspace .gogents/
// (runtime.yaml / state.yaml / tasks/) is relative to the workspace and
// unaffected.
func GlobalHome() string {
	if h := os.Getenv("GOGENTS_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, ".gogents")
}
