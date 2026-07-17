package provision

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree materializes a template tree on disk. Contents double as markers:
// a test reads them back to prove which tree Resolve picked.
func writeTree(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return dir
}

// markerTree is a minimal tree whose marker file identifies it.
func markerTree(t *testing.T, dir, marker string) string {
	t.Helper()
	return writeTree(t, dir, map[string]string{"marker": marker})
}

// chdir moves into dir for the duration of the test. Resolve reads the current
// directory for the name form, so the tests have to actually be somewhere.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if cErr := os.Chdir(dir); cErr != nil {
		t.Fatalf("chdir %s: %v", dir, cErr)
	}
	t.Cleanup(func() {
		if rErr := os.Chdir(old); rErr != nil {
			t.Fatalf("restore cwd: %v", rErr)
		}
	})
}

// wantMarker asserts which tree was resolved by reading its marker file.
func wantMarker(t *testing.T, src fs.FS, want string) {
	t.Helper()
	data, err := fs.ReadFile(src, "marker")
	if err != nil {
		t.Fatalf("resolved tree has no marker (wanted %q): %v", want, err)
	}
	if string(data) != want {
		t.Fatalf("resolved tree = %q, want %q", data, want)
	}
}

// wantBuiltin asserts the resolved tree is the embedded one.
func wantBuiltin(t *testing.T, src fs.FS) {
	t.Helper()
	if _, err := fs.Stat(src, ".gogents/runtime.yaml.tmpl"); err != nil {
		t.Fatalf("expected the built-in tree: %v", err)
	}
}

// TestResolveBuiltin covers D09-14: the built-in tree is what an omitted
// --template selects, and GOGENTS_TEMPLATES does not change that.
func TestResolveBuiltin(t *testing.T) {
	t.Run("no template flag", func(t *testing.T) {
		t.Setenv(envTemplates, "")
		src, err := Resolve("")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		wantBuiltin(t, src)
	})

	t.Run("GOGENTS_TEMPLATES set is not enough", func(t *testing.T) {
		base := t.TempDir()
		markerTree(t, filepath.Join(base, "svc"), "external")
		t.Setenv(envTemplates, base)

		src, err := Resolve("")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		// The env var is a base for name lookups, not a way to replace the
		// built-in tree wholesale.
		wantBuiltin(t, src)
	})
}

// TestResolvePathForm covers D09-15: a value with a separator is a path, and
// GOGENTS_TEMPLATES never applies to it.
func TestResolvePathForm(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	markerTree(t, filepath.Join(root, "my-tpl"), "path-form")

	markerTree(t, filepath.Join(root, "a", "b"), "nested")

	for name, tc := range map[string]struct{ template, want string }{
		"dot slash":      {"./my-tpl", "path-form"},
		"absolute":       {filepath.Join(root, "my-tpl"), "path-form"},
		"bare separator": {"a/b", "nested"},
	} {
		t.Run(name, func(t *testing.T) {
			// A decoy under the env var: a path form must ignore it.
			decoy := t.TempDir()
			markerTree(t, filepath.Join(decoy, "my-tpl"), "decoy")
			markerTree(t, filepath.Join(decoy, "a", "b"), "decoy")
			t.Setenv(envTemplates, decoy)

			src, err := Resolve(tc.template)
			if err != nil {
				t.Fatalf("resolve %q: %v", tc.template, err)
			}
			wantMarker(t, src, tc.want)
		})
	}

	t.Run("parent relative", func(t *testing.T) {
		t.Setenv(envTemplates, "")
		chdir(t, filepath.Join(root, "my-tpl"))
		src, err := Resolve("../my-tpl")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		wantMarker(t, src, "path-form")
	})
}

// TestResolveNameForm covers D09-16, D09-17 and D09-19: where a bare name is
// looked up, and that the form is decided by the literal value alone.
func TestResolveNameForm(t *testing.T) {
	t.Run("D09-16 GOGENTS_TEMPLATES set wins over the current directory", func(t *testing.T) {
		root := t.TempDir()
		chdir(t, root)
		markerTree(t, filepath.Join(root, "svc"), "cwd")

		base := t.TempDir()
		markerTree(t, filepath.Join(base, "svc"), "env")
		t.Setenv(envTemplates, base)

		src, err := Resolve("svc")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		wantMarker(t, src, "env")
	})

	t.Run("D09-17 GOGENTS_TEMPLATES unset falls to the current directory", func(t *testing.T) {
		root := t.TempDir()
		chdir(t, root)
		markerTree(t, filepath.Join(root, "svc"), "cwd")
		t.Setenv(envTemplates, "")

		src, err := Resolve("svc")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		wantMarker(t, src, "cwd")
	})

	t.Run("D09-19 ./name is a path even when the name would resolve", func(t *testing.T) {
		root := t.TempDir()
		chdir(t, root)
		markerTree(t, filepath.Join(root, "svc"), "cwd")

		base := t.TempDir()
		markerTree(t, filepath.Join(base, "svc"), "env")
		t.Setenv(envTemplates, base)

		src, err := Resolve("./svc")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		// Same directory as the bare name would find in D09-17, but the leading
		// ./ makes it a path, so the env var is out of the picture.
		wantMarker(t, src, "cwd")
	})
}

// TestResolveNameFormNoFallback covers D09-18: the two name lookups are
// exclusive, not a search path.
func TestResolveNameFormNoFallback(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	// A tree the fallback would find, if there were a fallback.
	markerTree(t, filepath.Join(root, "svc"), "cwd")

	base := t.TempDir()
	t.Setenv(envTemplates, base)

	_, err := Resolve("svc")
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("err = %v, want ErrTemplateNotFound", err)
	}
}

// TestResolveNotFound covers D09-20: unresolvable sources fail at Resolve, with
// a message naming the path actually looked at — and never fall back to the
// built-in tree.
func TestResolveNotFound(t *testing.T) {
	t.Run("missing path", func(t *testing.T) {
		t.Setenv(envTemplates, "")
		root := t.TempDir()
		chdir(t, root)

		_, err := Resolve("./nope")
		if !errors.Is(err, ErrTemplateNotFound) {
			t.Fatalf("err = %v, want ErrTemplateNotFound", err)
		}
		if want := filepath.Join(root, "nope"); !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must name the path looked at (%s)", err, want)
		}
	})

	t.Run("path is a regular file", func(t *testing.T) {
		t.Setenv(envTemplates, "")
		root := t.TempDir()
		file := filepath.Join(root, "tpl")
		if err := os.WriteFile(file, []byte("not a tree"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}

		_, err := Resolve(file)
		if !errors.Is(err, ErrTemplateNotFound) {
			t.Fatalf("err = %v, want ErrTemplateNotFound", err)
		}
	})

	t.Run("message names the env var when the name came from it", func(t *testing.T) {
		base := t.TempDir()
		t.Setenv(envTemplates, base)
		chdir(t, t.TempDir())

		_, err := Resolve("svc")
		if !errors.Is(err, ErrTemplateNotFound) {
			t.Fatalf("err = %v, want ErrTemplateNotFound", err)
		}
		if !strings.Contains(err.Error(), envTemplates) || !strings.Contains(err.Error(), filepath.Join(base, "svc")) {
			t.Errorf("error %q must name both %s and the resolved path", err, envTemplates)
		}
	})
}

// TestResolveNoLayering covers D09-21: the resolved tree is the whole content,
// never merged with the built-in one.
func TestResolveNoLayering(t *testing.T) {
	t.Setenv(envTemplates, "")
	root := t.TempDir()
	// A tree with a runtime.yaml.tmpl but deliberately no .mcp.json, which the
	// built-in tree does have.
	tpl := writeTree(t, filepath.Join(root, "my-tpl"), map[string]string{
		".gogents/runtime.yaml.tmpl": "runtime_name: \"{{.Name}}\"\n",
	})

	src, err := Resolve(tpl)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	ws := filepath.Join(root, "ws")
	written, err := Materialize(src, ws, localSpec())
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	if len(written) != 1 || written[0] != ".gogents/runtime.yaml" {
		t.Fatalf("written = %v, want only the external tree's file", written)
	}
	if _, sErr := os.Stat(filepath.Join(ws, ".mcp.json")); sErr == nil {
		t.Fatal(".mcp.json came from the built-in tree; sources must not be layered")
	}
	if got := readFile(t, filepath.Join(ws, ".gogents", "runtime.yaml")); got != "runtime_name: \"demo\"\n" {
		t.Fatalf("external template not rendered: %q", got)
	}
}

// TestWalkRejectsNonRegular covers D09-13: an external tree can hold things the
// embedded one cannot, and skipping them silently would provision a workspace
// that looks complete but is missing files.
func TestWalkRejectsNonRegular(t *testing.T) {
	t.Setenv(envTemplates, "")
	root := t.TempDir()
	tpl := writeTree(t, filepath.Join(root, "my-tpl"), map[string]string{
		".mcp.json": "{}\n",
	})
	if err := os.Symlink("/etc/passwd", filepath.Join(tpl, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	src, err := Resolve(tpl)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	t.Run("Materialize", func(t *testing.T) {
		_, mErr := Materialize(src, filepath.Join(root, "ws"), localSpec())
		if !errors.Is(mErr, ErrTemplateInvalid) {
			t.Fatalf("err = %v, want ErrTemplateInvalid", mErr)
		}
	})

	t.Run("Plan", func(t *testing.T) {
		// Plan walks the same tree: an invalid template must fail before the
		// user is asked to confirm an overwrite.
		_, pErr := Plan(src, filepath.Join(root, "ws2"))
		if !errors.Is(pErr, ErrTemplateInvalid) {
			t.Fatalf("err = %v, want ErrTemplateInvalid", pErr)
		}
	})
}
