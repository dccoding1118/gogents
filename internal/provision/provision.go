// Package provision materializes a workspace from a template tree (R13 / D09).
// A template's relative paths are the workspace's relative paths — `*.tmpl`
// files are rendered through text/template and lose the suffix, everything else
// is copied verbatim. It never deletes anything under the target: failures leave
// what already landed in place and report it to the caller.
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

var (
	// ErrTemplateInvalid marks a template that cannot be parsed or rendered
	// (syntax error, or a field the Spec does not carry).
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
		target := strings.TrimSuffix(p, ".tmpl")
		_, sErr := os.Stat(filepath.Join(workspace, filepath.FromSlash(target)))
		entries = append(entries, Entry{Path: target, Overwrite: sErr == nil})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: walk template: %v", ErrIO, err)
	}
	return entries, nil
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
