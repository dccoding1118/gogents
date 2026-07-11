package foundation

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalHome_Override(t *testing.T) {
	// D01-21: GOGENTS_HOME set → every global path (config.yaml, logs/) resolves
	// under the override, and GlobalConfigPath agrees with WorkspacePaths.
	custom := t.TempDir()
	t.Setenv("GOGENTS_HOME", custom)

	if got := GlobalHome(); got != custom {
		t.Fatalf("GlobalHome = %s, want %s", got, custom)
	}
	if got := GlobalConfigPath(); got != filepath.Join(custom, "config.yaml") {
		t.Fatalf("GlobalConfigPath = %s, want %s", got, filepath.Join(custom, "config.yaml"))
	}

	paths := NewWorkspacePaths(t.TempDir())
	if got := paths.GlobalConfigYaml(); got != filepath.Join(custom, "config.yaml") {
		t.Fatalf("GlobalConfigYaml = %s, want under %s", got, custom)
	}
	if got := paths.LogsDir(); !strings.HasPrefix(got, custom+string(filepath.Separator)) {
		t.Fatalf("LogsDir = %s, want under %s", got, custom)
	}
}

func TestGlobalHome_DefaultAndWorkspaceUnaffected(t *testing.T) {
	// D01-22: unset GOGENTS_HOME → $HOME/.gogents (backward compatible); the
	// per-workspace .gogents/ stays under the workspace, never the home root.
	home := t.TempDir()
	t.Setenv("GOGENTS_HOME", "")
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".gogents")
	if got := GlobalHome(); got != want {
		t.Fatalf("GlobalHome = %s, want %s", got, want)
	}

	ws := t.TempDir()
	paths := NewWorkspacePaths(ws)
	if got := paths.GlobalConfigYaml(); got != filepath.Join(want, "config.yaml") {
		t.Fatalf("GlobalConfigYaml = %s, want %s", got, filepath.Join(want, "config.yaml"))
	}
	if got := paths.RuntimeYaml(); !strings.HasPrefix(got, ws+string(filepath.Separator)) {
		t.Fatalf("RuntimeYaml = %s, want under workspace %s", got, ws)
	}
	if strings.HasPrefix(paths.RuntimeYaml(), want) {
		t.Fatalf("per-workspace path must not move under global home: %s", paths.RuntimeYaml())
	}
}
