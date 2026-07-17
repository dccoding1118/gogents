package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/internal/provision"
)

func localOpts(ws string) CreateOpts {
	return CreateOpts{
		Name:      "demo",
		Workspace: ws,
		RepoURL:   "https://github.com/o/r",
		Agent:     "claude",
		Model:     "claude-sonnet-5",
		Source:    "local",
	}
}

func neverConfirm(t *testing.T) func([]provision.Entry) bool {
	t.Helper()
	return func([]provision.Entry) bool {
		t.Fatal("confirm must not be called")
		return false
	}
}

// TestCreateOptsValidate covers D08-16 (required flags and enums), D08-17
// (defaults) and D08-18 (per-source compatibility).
func TestCreateOptsValidate(t *testing.T) {
	t.Run("D08-16 required flags and enums", func(t *testing.T) {
		for name, mutate := range map[string]func(*CreateOpts){
			"no repo":    func(o *CreateOpts) { o.RepoURL = "" },
			"no agent":   func(o *CreateOpts) { o.Agent = "" },
			"no model":   func(o *CreateOpts) { o.Model = "" },
			"no source":  func(o *CreateOpts) { o.Source = "" },
			"no name":    func(o *CreateOpts) { o.Name = "" },
			"bad source": func(o *CreateOpts) { o.Source = "jira" },
			"bad agent":  func(o *CreateOpts) { o.Agent = "gemini" },
		} {
			t.Run(name, func(t *testing.T) {
				o := localOpts("")
				mutate(&o)
				if err := o.Validate(); err == nil {
					t.Fatal("expected validation error")
				}
			})
		}
	})

	t.Run("D08-17 defaults", func(t *testing.T) {
		o := localOpts("")
		if err := o.Validate(); err != nil {
			t.Fatalf("validate: %v", err)
		}
		if want := "." + string(filepath.Separator) + "demo"; o.Workspace != want {
			t.Errorf("workspace = %q, want %q", o.Workspace, want)
		}
		if o.Label != "demo" {
			t.Errorf("label = %q, want the runtime name", o.Label)
		}
		if o.Branch != "main" {
			t.Errorf("branch = %q, want main", o.Branch)
		}
	})

	t.Run("D08-17 explicit values survive", func(t *testing.T) {
		o := localOpts("/tmp/ws")
		o.Label = "custom"
		o.Branch = "develop"
		if err := o.Validate(); err != nil {
			t.Fatalf("validate: %v", err)
		}
		if o.Workspace != "/tmp/ws" || o.Label != "custom" || o.Branch != "develop" {
			t.Errorf("defaults clobbered explicit values: %+v", o)
		}
	})

	t.Run("D08-18 per-source compatibility", func(t *testing.T) {
		for name, mutate := range map[string]func(*CreateOpts){
			"github_issue without instance": func(o *CreateOpts) { o.Source = "github_issue" },
			"local with instance":           func(o *CreateOpts) { o.Instance = "o/r" },
			"github_issue with tickets-dir": func(o *CreateOpts) {
				o.Source = "github_issue"
				o.Instance = "o/r"
				o.TicketsPath = "/inbox"
			},
		} {
			t.Run(name, func(t *testing.T) {
				o := localOpts("")
				mutate(&o)
				if err := o.Validate(); err == nil {
					t.Fatal("expected validation error")
				}
			})
		}
	})
}

// TestCreateTargetStates covers D08-19 through D08-23: when the confirmation is
// asked for, and what happens with each answer.
func TestCreateTargetStates(t *testing.T) {
	t.Run("D08-19 absent target provisions without asking", func(t *testing.T) {
		ws := filepath.Join(t.TempDir(), "demo")
		written, err := Create(localOpts(ws), neverConfirm(t))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if len(written) == 0 {
			t.Fatal("expected files to be written")
		}
		if _, sErr := os.Stat(filepath.Join(ws, ".gogents", "runtime.yaml")); sErr != nil {
			t.Fatalf("runtime.yaml missing: %v", sErr)
		}
	})

	t.Run("D08-19 empty dir provisions without asking", func(t *testing.T) {
		ws := t.TempDir()
		if _, err := Create(localOpts(ws), neverConfirm(t)); err != nil {
			t.Fatalf("create: %v", err)
		}
	})

	t.Run("D08-20 declined confirmation writes nothing", func(t *testing.T) {
		ws := t.TempDir()
		if err := os.WriteFile(filepath.Join(ws, "keepme.txt"), []byte("mine"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}

		asked := false
		_, err := Create(localOpts(ws), func([]provision.Entry) bool { asked = true; return false })
		if !errors.Is(err, ErrWorkspaceNotEmpty) {
			t.Fatalf("err = %v, want ErrWorkspaceNotEmpty", err)
		}
		if !asked {
			t.Error("a non-empty workspace must trigger the confirmation")
		}
		entries, _ := os.ReadDir(ws)
		if len(entries) != 1 {
			t.Fatalf("declining must write nothing, found %d entries", len(entries))
		}
	})

	t.Run("D08-21 accepted confirmation provisions and spares other files", func(t *testing.T) {
		ws := t.TempDir()
		keep := filepath.Join(ws, "keepme.txt")
		if err := os.WriteFile(keep, []byte("mine"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}

		if _, err := Create(localOpts(ws), func([]provision.Entry) bool { return true }); err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, sErr := os.Stat(filepath.Join(ws, ".gogents", "runtime.yaml")); sErr != nil {
			t.Fatalf("runtime.yaml missing: %v", sErr)
		}
		data, err := os.ReadFile(keep) //nolint:gosec // test path
		if err != nil || string(data) != "mine" {
			t.Fatalf("unrelated file must survive: %v %q", err, data)
		}
	})

	t.Run("D08-22 --force skips the confirmation", func(t *testing.T) {
		ws := t.TempDir()
		if err := os.WriteFile(filepath.Join(ws, "keepme.txt"), []byte("mine"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		o := localOpts(ws)
		o.Force = true
		if _, err := Create(o, neverConfirm(t)); err != nil {
			t.Fatalf("create: %v", err)
		}
	})

	t.Run("D08-23 target file is refused, force included", func(t *testing.T) {
		ws := filepath.Join(t.TempDir(), "afile")
		if err := os.WriteFile(ws, []byte("mine"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}

		o := localOpts(ws)
		o.Force = true
		if _, err := Create(o, neverConfirm(t)); !errors.Is(err, provision.ErrWorkspaceExists) {
			t.Fatalf("err = %v, want ErrWorkspaceExists", err)
		}
		data, err := os.ReadFile(ws) //nolint:gosec // test path
		if err != nil || string(data) != "mine" {
			t.Fatalf("the existing file must be untouched: %v %q", err, data)
		}
	})
}

// TestCreateDoesNotRegisterOrClone covers D08-24.
func TestCreateDoesNotRegisterOrClone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOGENTS_HOME", home)

	ws := filepath.Join(t.TempDir(), "demo")
	if _, err := Create(localOpts(ws), neverConfirm(t)); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("create must not touch the runtime registry; that happens on start")
	}
	entries, err := os.ReadDir(ws)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "r" {
			t.Fatal("create must not clone the repo; the daemon does that at startup")
		}
	}
}

// TestRuntimeCreateDispatch covers D08-26: argument routing through Dispatch —
// the name is positional (flag parsing stops at the first non-flag argument), and
// create takes no --json.
func TestRuntimeCreateDispatch(t *testing.T) {
	t.Setenv("GOGENTS_HOME", t.TempDir())

	t.Run("flags after the positional name are parsed", func(t *testing.T) {
		ws := filepath.Join(t.TempDir(), "demo")
		code := Dispatch([]string{
			"runtime", "create", "demo",
			"--workspace", ws,
			"--repo", "https://github.com/o/r",
			"--agent", "claude", "--model", "claude-sonnet-5",
			"--source", "local",
		})
		if code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		if _, err := os.Stat(filepath.Join(ws, ".gogents", "runtime.yaml")); err != nil {
			t.Fatalf("runtime.yaml missing: %v", err)
		}
	})

	for name, args := range map[string][]string{
		"no name":                {"runtime", "create"},
		"name looks like a flag": {"runtime", "create", "--repo", "r"},
		"unknown --json":         {"runtime", "create", "demo", "--repo", "r", "--agent", "claude", "--model", "m", "--source", "local", "--json"},
		"missing --repo":         {"runtime", "create", "demo", "--agent", "claude", "--model", "m", "--source", "local"},
	} {
		t.Run(name, func(t *testing.T) {
			if code := Dispatch(args); code == 0 {
				t.Fatal("expected a non-zero exit")
			}
		})
	}
}

// TestCreateOutputStarts covers D08-25: the generated runtime.yaml satisfies the
// config schema, so `gogents start` accepts the workspace as-is.
func TestCreateOutputStarts(t *testing.T) {
	t.Setenv("GOGENTS_HOME", t.TempDir())

	for name, mutate := range map[string]func(*CreateOpts){
		"local": func(*CreateOpts) {},
		"github_issue": func(o *CreateOpts) {
			o.Source = "github_issue"
			o.Instance = "o/r"
			o.Label = "gogents:queued"
		},
	} {
		t.Run(name, func(t *testing.T) {
			ws := filepath.Join(t.TempDir(), "demo")
			o := localOpts(ws)
			mutate(&o)
			if _, err := Create(o, neverConfirm(t)); err != nil {
				t.Fatalf("create: %v", err)
			}

			cfg, err := foundation.NewConfigLoader(foundation.NewWorkspacePaths(ws)).Load(ws)
			if err != nil {
				t.Fatalf("generated runtime.yaml rejected by the schema: %v", err)
			}
			if cfg.RuntimeName != "demo" || cfg.Agent.Model != "claude-sonnet-5" {
				t.Fatalf("unexpected config: %+v", cfg)
			}
		})
	}
}
