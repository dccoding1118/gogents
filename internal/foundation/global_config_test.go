package foundation

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestGlobalConfigStore_Upsert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	store := NewGlobalConfigStoreAt(path)

	// D01-18: new workspace registration.
	if err := store.Upsert(RuntimeRegistration{Name: "w1", Workspace: "/ws/one", RegisteredAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatalf("upsert new: %v", err)
	}
	gc, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(gc.Runtimes) != 1 || gc.Runtimes[0].Name != "w1" {
		t.Fatalf("expected one runtime w1, got %+v", gc.Runtimes)
	}

	// D01-19: same workspace path overwrites (keyed by workspace).
	if err := store.Upsert(RuntimeRegistration{Name: "w1-renamed", Workspace: "/ws/one", RegisteredAt: "2026-02-02T00:00:00Z"}); err != nil {
		t.Fatalf("upsert overwrite: %v", err)
	}
	gc, _ = store.Load()
	if len(gc.Runtimes) != 1 {
		t.Fatalf("expected still one runtime after overwrite, got %d", len(gc.Runtimes))
	}
	if gc.Runtimes[0].Name != "w1-renamed" || gc.Runtimes[0].RegisteredAt != "2026-02-02T00:00:00Z" {
		t.Fatalf("overwrite did not update name/registered_at: %+v", gc.Runtimes[0])
	}

	// Different workspace path appends.
	if err := store.Upsert(RuntimeRegistration{Name: "w2", Workspace: "/ws/two", RegisteredAt: "2026-03-03T00:00:00Z"}); err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	gc, _ = store.Load()
	if len(gc.Runtimes) != 2 {
		t.Fatalf("expected two runtimes, got %d", len(gc.Runtimes))
	}
}

func TestGlobalConfigStore_Resolve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	store := NewGlobalConfigStoreAt(path)
	_ = store.Upsert(RuntimeRegistration{Name: "dup", Workspace: "/ws/old", RegisteredAt: "2026-01-01T00:00:00Z"})
	_ = store.Upsert(RuntimeRegistration{Name: "dup", Workspace: "/ws/new", RegisteredAt: "2026-05-05T00:00:00Z"})
	_ = store.Upsert(RuntimeRegistration{Name: "solo", Workspace: "/ws/solo", RegisteredAt: "2026-01-01T00:00:00Z"})

	t.Run("D01-20 resolve by workspace path (exact)", func(t *testing.T) {
		r, err := store.Resolve("/ws/old")
		if err != nil || r.Workspace != "/ws/old" {
			t.Fatalf("resolve by path: %+v %v", r, err)
		}
	})

	t.Run("D01-20 resolve by name picks latest", func(t *testing.T) {
		r, err := store.Resolve("dup")
		if err != nil {
			t.Fatalf("resolve dup: %v", err)
		}
		if r.Workspace != "/ws/new" {
			t.Fatalf("expected latest /ws/new, got %s", r.Workspace)
		}
	})

	t.Run("D01-20 not registered", func(t *testing.T) {
		_, err := store.Resolve("missing")
		if !errors.Is(err, ErrRuntimeNotRegistered) {
			t.Fatalf("expected ErrRuntimeNotRegistered, got %v", err)
		}
	})
}

func TestGlobalConfigPath(t *testing.T) {
	if p := GlobalConfigPath(); filepath.Base(p) != "config.yaml" {
		t.Errorf("expected .../config.yaml, got %s", p)
	}
	if NewGlobalConfigStore() == nil {
		t.Error("NewGlobalConfigStore returned nil")
	}
}

func TestGlobalConfigStore_LoadMissing(t *testing.T) {
	store := NewGlobalConfigStoreAt(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	gc, err := store.Load()
	if err != nil {
		t.Fatalf("missing file should return zero + nil, got %v", err)
	}
	if len(gc.Runtimes) != 0 {
		t.Fatalf("expected empty registry, got %+v", gc.Runtimes)
	}
}
