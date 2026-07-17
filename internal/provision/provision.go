// Package provision materializes a workspace from a template tree (R13 / D09).
// A template's relative paths are the workspace's relative paths — `*.tmpl`
// files are rendered through text/template and lose the suffix, everything else
// is copied verbatim. It never deletes anything under the target: failures leave
// what already landed in place and report it to the caller.
//
// Resolve is the only place that decides where a template tree comes from;
// everything after it works on an fs.FS and is oblivious to whether the tree is
// the built-in one or a directory the user pointed at.
//
// Interactive confirmation lives in the command layer: this package only
// classifies the target (Inspect) and lists what would land (Plan).
package provision

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/templates"
)

// envTemplates names the directory that holds the user's template trees, one
// subdirectory per template name. It is independent of GOGENTS_HOME: the global
// home is a state directory for the registry and logs, never a template location.
const envTemplates = "GOGENTS_TEMPLATES"

var (
	// ErrTemplateNotFound marks a template source that does not resolve to a
	// directory. It is always reported before anything is written, which is why
	// it is distinct from ErrTemplateInvalid — that one can leave files behind.
	ErrTemplateNotFound = errors.New("template not found")
	// ErrTemplateInvalid marks a template that cannot be parsed or rendered
	// (syntax error, or a field the Spec does not carry), or a tree holding
	// something other than regular files and directories.
	ErrTemplateInvalid = errors.New("template invalid")
	// ErrWorkspaceExists marks a target path occupied by a non-directory.
	ErrWorkspaceExists = errors.New("workspace path exists and is not a directory")
	// ErrIO marks a filesystem failure while inspecting or writing.
	ErrIO = errors.New("provision io error")
)

// Spec is the template data model: its exported fields are exactly the
// variables a template may reference ({{.Name}} and friends, D09 §3.2).
type Spec struct {
	Name        string
	RepoURL     string
	Branch      string
	Agent       string
	Model       string
	Source      string
	Label       string
	Instance    string
	TicketsPath string
}

// TargetState classifies a target workspace path (D09 §3.3).
type TargetState int

const (
	// TargetAbsent — the path does not exist.
	TargetAbsent TargetState = iota
	// TargetEmptyDir — an existing directory holding nothing.
	TargetEmptyDir
	// TargetNonEmptyDir — an existing directory holding something; the command
	// layer must confirm before materializing into it.
	TargetNonEmptyDir
	// TargetNotDir — the path exists but is not a directory; never provisionable.
	TargetNotDir
)

// Entry is one item of a materialize plan: Path is the workspace-relative path
// that would land (already stripped of any .tmpl suffix), Overwrite reports
// whether a file already exists there.
type Entry struct {
	Path      string
	Overwrite bool
}

// Builtin returns the template tree embedded in the binary.
func Builtin() fs.FS { return templates.Base() }

// Resolve picks the one template tree to provision from (D09 §3.1). template is
// the --template value; "" means the flag was omitted and selects the built-in
// tree. The sources are mutually exclusive and never layered: whichever matches
// first is the whole tree.
//
//	path form (contains a separator) → that directory, ignoring $GOGENTS_TEMPLATES
//	name form, $GOGENTS_TEMPLATES set → $GOGENTS_TEMPLATES/<name>
//	name form, $GOGENTS_TEMPLATES unset → <cwd>/<name>
//
// An external source that does not resolve to a directory is an error rather
// than a fall back to the built-in tree: the user named a tree, and silently
// handing them a different one produces a workspace that looks provisioned but
// holds something else entirely.
func Resolve(template string) (fs.FS, error) {
	if template == "" {
		// The embedded tree is fixed at build time; there is nothing to check.
		return Builtin(), nil
	}

	dir, origin := template, "the path given"
	if !isPathForm(template) {
		if base := os.Getenv(envTemplates); base != "" {
			dir = filepath.Join(base, template)
			origin = "$" + envTemplates
		} else {
			origin = "the current directory ($" + envTemplates + " is not set)"
		}
	}

	// Check here rather than leaving it to os.DirFS, which validates nothing:
	// the failure would surface during the walk, where it is no longer
	// distinguishable from a tree that went missing mid-provision.
	info, err := os.Stat(dir) //nolint:gosec // dir is the template the user asked for; pointing at an arbitrary path is the feature
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s (resolved from %s)", ErrTemplateNotFound, absOrRaw(dir), origin)
		}
		return nil, fmt.Errorf("%w: stat %s: %v", ErrIO, absOrRaw(dir), err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory (resolved from %s)", ErrTemplateNotFound, absOrRaw(dir), origin)
	}
	return os.DirFS(dir), nil
}

// isPathForm reports whether template names a location rather than a template
// name. The check is on the literal value, not on what exists: `rust-svc` stays
// a name even when ./rust-svc happens to be there, so resolution depends only on
// the input. Write ./rust-svc to mean the directory.
func isPathForm(template string) bool {
	return strings.ContainsRune(template, '/') || strings.ContainsRune(template, filepath.Separator)
}

// absOrRaw makes a path absolute for error messages, falling back to the raw
// value: a resolution error must not be masked by a failure to pretty-print it.
func absOrRaw(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// Inspect classifies workspace so the command layer can decide whether to
// proceed, prompt, or refuse. It reads nothing but directory metadata.
func Inspect(workspace string) (TargetState, error) {
	info, err := os.Stat(workspace)
	if err != nil {
		if os.IsNotExist(err) {
			return TargetAbsent, nil
		}
		return TargetAbsent, fmt.Errorf("%w: stat %s: %v", ErrIO, workspace, err)
	}
	if !info.IsDir() {
		return TargetNotDir, nil
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return TargetAbsent, fmt.Errorf("%w: read %s: %v", ErrIO, workspace, err)
	}
	if len(entries) == 0 {
		return TargetEmptyDir, nil
	}
	return TargetNonEmptyDir, nil
}

// Plan walks src and reports what Materialize would write, marking the entries
// that would overwrite an existing file. It writes nothing.
func Plan(src fs.FS, workspace string) ([]Entry, error) {
	var entries []Entry
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if rErr := checkRegular(p, d); rErr != nil {
			return rErr
		}
		target := strings.TrimSuffix(p, ".tmpl")
		_, sErr := os.Stat(filepath.Join(workspace, filepath.FromSlash(target)))
		entries = append(entries, Entry{Path: target, Overwrite: sErr == nil})
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrTemplateInvalid) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: walk template: %v", ErrIO, err)
	}
	return entries, nil
}

// checkRegular rejects anything that is neither a regular file nor a directory.
// An external tree can hold symlinks, devices and sockets, which the embedded
// tree never can. Skipping them silently would provision a workspace that looks
// complete but is missing files, so the walk stops instead (D09 B1a).
func checkRegular(p string, d fs.DirEntry) error {
	if d.Type().IsRegular() {
		return nil
	}
	return fmt.Errorf("%w: %s is not a regular file (%s)", ErrTemplateInvalid, p, d.Type())
}

// Materialize expands src into workspace. written lists the workspace-relative
// paths that landed; it is returned on failure too, so the caller can tell the
// user what remains — nothing is rolled back or deleted (D09 B5).
func Materialize(src fs.FS, workspace string, spec Spec) (written []string, err error) {
	walkErr := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("%w: walk template: %v", ErrIO, walkErr)
		}

		dst := filepath.Join(workspace, filepath.FromSlash(p))
		if d.IsDir() {
			if mErr := os.MkdirAll(dst, 0o750); mErr != nil {
				return fmt.Errorf("%w: create dir %s: %v", ErrIO, dst, mErr)
			}
			return nil
		}
		if cErr := checkRegular(p, d); cErr != nil {
			return cErr
		}

		data, rErr := fs.ReadFile(src, p)
		if rErr != nil {
			return fmt.Errorf("%w: read template %s: %v", ErrIO, p, rErr)
		}

		target := p
		if strings.HasSuffix(p, ".tmpl") {
			rendered, tErr := render(p, data, spec)
			if tErr != nil {
				return tErr
			}
			data = rendered
			target = strings.TrimSuffix(p, ".tmpl")
			dst = filepath.Join(workspace, filepath.FromSlash(target))
		}

		if aErr := foundation.AtomicWrite(dst, data, 0o600); aErr != nil {
			return fmt.Errorf("%w: write %s: %v", ErrIO, dst, aErr)
		}
		written = append(written, target)
		return nil
	})

	return written, walkErr
}

// render expands one template against spec. missingkey=error keeps a template
// referencing an unknown key from silently rendering empty.
func render(name string, data []byte, spec Spec) ([]byte, error) {
	t, err := template.New(filepath.Base(name)).Option("missingkey=error").Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrTemplateInvalid, name, err)
	}
	var buf bytes.Buffer
	if eErr := t.Execute(&buf, spec); eErr != nil {
		return nil, fmt.Errorf("%w: render %s: %v", ErrTemplateInvalid, name, eErr)
	}
	return buf.Bytes(), nil
}
