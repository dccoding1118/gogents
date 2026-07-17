package foundation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAtomicWrite covers D01-25: content, perm, parent creation, overwrite, and
// no tmp file left behind.
func TestAtomicWrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "dir", "f.yaml")

	if err := AtomicWrite(path, []byte("first"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // test path
	if err != nil || string(data) != "first" {
		t.Fatalf("content = %q (%v), want \"first\"", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perm = %o, want 600", perm)
	}

	if wErr := AtomicWrite(path, []byte("second"), 0o600); wErr != nil {
		t.Fatalf("overwrite: %v", wErr)
	}
	data, err = os.ReadFile(path) //nolint:gosec // test path
	if err != nil || string(data) != "second" {
		t.Fatalf("content = %q (%v), want \"second\"", data, err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Fatalf("tmp file left behind: %s", e.Name())
		}
	}

	t.Run("unwritable target reports an error", func(t *testing.T) {
		if wErr := AtomicWrite(filepath.Join(root, "nested", "dir", "f.yaml", "deeper"), []byte("x"), 0o600); wErr == nil {
			t.Fatal("expected an error when the parent path is a file")
		}
	})
}
