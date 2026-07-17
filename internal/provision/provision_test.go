package provision

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func localSpec() Spec {
	return Spec{
		Name:    "demo",
		RepoURL: "https://github.com/o/r",
		Branch:  "main",
		Agent:   "claude",
		Model:   "claude-sonnet-5",
		Source:  "local",
		Label:   "demo",
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestMaterializeLayout covers D09-01: every template file lands on its
// corresponding relative path, .tmpl loses its suffix, and modes are as designed.
func TestMaterializeLayout(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "demo")

	written, err := Materialize(Builtin(), ws, localSpec())
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	want := []string{".claude/CLAUDE.md", ".claude/skills/.gitkeep", ".gogents/runtime.yaml", ".mcp.json"}
	if strings.Join(written, ",") != strings.Join(want, ",") {
		t.Fatalf("written = %v, want %v", written, want)
	}
	for _, rel := range want {
		if _, sErr := os.Stat(filepath.Join(ws, filepath.FromSlash(rel))); sErr != nil {
			t.Fatalf("expected %s to exist: %v", rel, sErr)
		}
	}
	if _, sErr := os.Stat(filepath.Join(ws, ".gogents", "runtime.yaml.tmpl")); sErr == nil {
		t.Fatal("runtime.yaml.tmpl must not be copied verbatim")
	}

	fileInfo, err := os.Stat(filepath.Join(ws, ".gogents", "runtime.yaml"))
	if err != nil {
		t.Fatalf("stat runtime.yaml: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perm = %o, want 600", perm)
	}
	dirInfo, err := os.Stat(filepath.Join(ws, ".gogents"))
	if err != nil {
		t.Fatalf("stat .gogents: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o750 {
		t.Fatalf("dir perm = %o, want 750", perm)
	}

	entries, err := os.ReadDir(filepath.Join(ws, ".gogents"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Fatalf("tmp file left behind: %s", e.Name())
		}
	}
}

// TestMaterializeRuntimeYaml covers D09-02 through D09-05: rendered values and
// the conditional expansion of the source block.
func TestMaterializeRuntimeYaml(t *testing.T) {
	t.Run("D09-02/03 local renders spec values and omits instance", func(t *testing.T) {
		ws := t.TempDir()
		if _, err := Materialize(Builtin(), ws, localSpec()); err != nil {
			t.Fatalf("materialize: %v", err)
		}
		got := readFile(t, filepath.Join(ws, ".gogents", "runtime.yaml"))

		for _, want := range []string{
			"runtime_name: demo",
			`url: "https://github.com/o/r"`,
			`default_branch: "main"`,
			"type: local",
			`label: "demo"`,
			"type: claude",
			`model: "claude-sonnet-5"`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("runtime.yaml missing %q\n---\n%s", want, got)
			}
		}
		if strings.Contains(got, "instance:") {
			t.Errorf("local source must not emit instance\n---\n%s", got)
		}
	})

	t.Run("D09-04 github_issue emits instance and no path", func(t *testing.T) {
		ws := t.TempDir()
		spec := localSpec()
		spec.Source = "github_issue"
		spec.Instance = "o/r"
		if _, err := Materialize(Builtin(), ws, spec); err != nil {
			t.Fatalf("materialize: %v", err)
		}
		got := readFile(t, filepath.Join(ws, ".gogents", "runtime.yaml"))
		if !strings.Contains(got, `instance: "o/r"`) {
			t.Errorf("expected instance\n---\n%s", got)
		}
		if strings.Contains(got, "path:") {
			t.Errorf("empty TicketsPath must not emit path\n---\n%s", got)
		}
	})

	t.Run("D09-05 tickets path present and absent", func(t *testing.T) {
		ws := t.TempDir()
		spec := localSpec()
		spec.TicketsPath = "/inbox/tickets"
		if _, err := Materialize(Builtin(), ws, spec); err != nil {
			t.Fatalf("materialize: %v", err)
		}
		got := readFile(t, filepath.Join(ws, ".gogents", "runtime.yaml"))
		if !strings.Contains(got, `path: "/inbox/tickets"`) {
			t.Errorf("expected source.path\n---\n%s", got)
		}
	})
}

// TestMaterializeInvalidTemplate covers D09-06: an unknown field is an error,
// never a silently empty render.
func TestMaterializeInvalidTemplate(t *testing.T) {
	for name, src := range map[string]fs.FS{
		"unknown field": fstest.MapFS{"f.tmpl": &fstest.MapFile{Data: []byte("v: {{.Nope}}\n")}},
		"syntax error":  fstest.MapFS{"f.tmpl": &fstest.MapFile{Data: []byte("v: {{.Name\n")}},
	} {
		t.Run(name, func(t *testing.T) {
			ws := t.TempDir()
			_, err := Materialize(src, ws, localSpec())
			if !errors.Is(err, ErrTemplateInvalid) {
				t.Fatalf("err = %v, want ErrTemplateInvalid", err)
			}
			if _, sErr := os.Stat(filepath.Join(ws, "f")); sErr == nil {
				t.Fatal("a failed render must not leave the target file")
			}
		})
	}
}

// TestMaterializeOverwrite covers D09-07: same-named files are replaced, and
// nothing else in the workspace is touched.
func TestMaterializeOverwrite(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".claude"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".claude", "CLAUDE.md"), []byte("stale"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	keep := filepath.Join(ws, "keepme.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := Materialize(Builtin(), ws, localSpec()); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	if got := readFile(t, filepath.Join(ws, ".claude", "CLAUDE.md")); got == "stale" {
		t.Fatal("existing template file must be overwritten")
	}
	if got := readFile(t, keep); got != "mine" {
		t.Fatalf("unrelated file changed: %q", got)
	}
}

// TestMaterializeFailureKeepsWrittenFiles covers D09-08: a mid-way failure
// deletes nothing and reports what already landed.
func TestMaterializeFailureKeepsWrittenFiles(t *testing.T) {
	ws := t.TempDir()
	// A directory where .mcp.json must land: the rename onto it fails, and
	// .mcp.json sorts last, so earlier files are already written by then.
	if err := os.MkdirAll(filepath.Join(ws, ".mcp.json"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	written, err := Materialize(Builtin(), ws, localSpec())
	if !errors.Is(err, ErrIO) {
		t.Fatalf("err = %v, want ErrIO", err)
	}
	if len(written) == 0 {
		t.Fatal("written must report the files that landed before the failure")
	}
	for _, rel := range written {
		if _, sErr := os.Stat(filepath.Join(ws, filepath.FromSlash(rel))); sErr != nil {
			t.Fatalf("%s was deleted on failure; provision must never delete: %v", rel, sErr)
		}
	}
}

// TestMaterializeIsRepeatable covers D09-09.
func TestMaterializeIsRepeatable(t *testing.T) {
	ws := t.TempDir()
	if _, err := Materialize(Builtin(), ws, localSpec()); err != nil {
		t.Fatalf("first: %v", err)
	}
	first := readFile(t, filepath.Join(ws, ".gogents", "runtime.yaml"))
	if _, err := Materialize(Builtin(), ws, localSpec()); err != nil {
		t.Fatalf("second: %v", err)
	}
	if second := readFile(t, filepath.Join(ws, ".gogents", "runtime.yaml")); second != first {
		t.Fatalf("re-run diverged:\n%s\n---\n%s", first, second)
	}
}

// TestInspect covers D09-10.
func TestInspect(t *testing.T) {
	root := t.TempDir()

	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	nonEmpty := filepath.Join(root, "nonempty")
	if err := os.Mkdir(nonEmpty, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "f"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for name, tc := range map[string]struct {
		path string
		want TargetState
	}{
		"absent":    {filepath.Join(root, "nope"), TargetAbsent},
		"empty dir": {empty, TargetEmptyDir},
		"non-empty": {nonEmpty, TargetNonEmptyDir},
		"not a dir": {file, TargetNotDir},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Inspect(tc.path)
			if err != nil {
				t.Fatalf("inspect: %v", err)
			}
			if got != tc.want {
				t.Fatalf("state = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPlan covers D09-11: overwrite marking, suffix stripping, and that Plan is
// read-only.
func TestPlan(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, ".mcp.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	entries, err := Plan(Builtin(), ws)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	got := map[string]bool{}
	for _, e := range entries {
		got[e.Path] = e.Overwrite
		if strings.HasSuffix(e.Path, ".tmpl") {
			t.Errorf("plan path keeps .tmpl suffix: %s", e.Path)
		}
	}
	if overwrite, ok := got[".mcp.json"]; !ok || !overwrite {
		t.Errorf(".mcp.json should be marked overwrite: %+v", entries)
	}
	if overwrite, ok := got[".gogents/runtime.yaml"]; !ok || overwrite {
		t.Errorf("runtime.yaml should be planned as new: %+v", entries)
	}

	left, err := os.ReadDir(ws)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("Plan must not write anything, workspace holds %d entries", len(left))
	}
}

// TestBuiltin covers D09-12: the embedded tree is complete — in particular the
// dot-prefixed paths that embed's default rules would drop without `all:`.
func TestBuiltin(t *testing.T) {
	for _, want := range []string{
		".gogents/runtime.yaml.tmpl",
		".claude/CLAUDE.md",
		".claude/skills/.gitkeep",
		".mcp.json",
	} {
		if _, err := fs.Stat(Builtin(), want); err != nil {
			t.Errorf("embedded template missing %s: %v", want, err)
		}
	}
}
