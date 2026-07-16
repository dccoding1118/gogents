package cli

import (
	"path/filepath"
	"testing"

	"github.com/dccoding1118/gogents/internal/foundation"
)

// TestRuntimeRemove drives the full `gogents runtime remove` dispatch path. It
// also exercises GOGENTS_HOME resolution: the handler builds its store from
// GlobalConfigPath(), which must land on the same config.yaml we seed here.
func TestRuntimeRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOGENTS_HOME", home)

	store := foundation.NewGlobalConfigStoreAt(filepath.Join(home, "config.yaml"))
	_ = store.Upsert(foundation.RuntimeRegistration{Name: "keep", Workspace: "/ws/keep", RegisteredAt: "2026-01-01T00:00:00Z"})
	_ = store.Upsert(foundation.RuntimeRegistration{Name: "drop", Workspace: "/ws/drop", RegisteredAt: "2026-02-01T00:00:00Z"})

	t.Run("D08-14 remove hit", func(t *testing.T) {
		if code := Dispatch([]string{"runtime", "remove", "/ws/drop"}); code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}
		gc, _ := store.Load()
		if len(gc.Runtimes) != 1 || gc.Runtimes[0].Workspace != "/ws/keep" {
			t.Fatalf("expected only /ws/keep left, got %+v", gc.Runtimes)
		}
	})

	t.Run("D08-15 remove miss leaves registry unchanged", func(t *testing.T) {
		before, _ := store.Load()
		if code := Dispatch([]string{"runtime", "remove", "/ws/missing"}); code == 0 {
			t.Fatalf("expected non-zero exit for unregistered target")
		}
		after, _ := store.Load()
		if len(after.Runtimes) != len(before.Runtimes) {
			t.Fatalf("registry must not change on miss: before %d after %d", len(before.Runtimes), len(after.Runtimes))
		}
	})
}
