// Package templates embeds the built-in workspace template tree so a `go
// install`-ed or downloaded binary can provision a workspace with no external
// files. The tree is also kept in the repo as plain files, for cloning and
// modifying (R13).
package templates

import (
	"embed"
	"io/fs"
)

// The `all:` prefix is required: embed's default rules skip files and
// directories whose names begin with `.`, which would drop .gogents/, .claude/
// and .mcp.json — i.e. the entire template.
//
//go:embed all:base
var baseFS embed.FS

// Base returns the built-in workspace template tree, rooted so that its entries
// are the workspace-relative paths to materialize (D09 §3.1).
func Base() fs.FS {
	sub, err := fs.Sub(baseFS, "base")
	if err != nil {
		// The embedded tree is fixed at build time; a failure here means the
		// binary was built without base/, which no runtime input can cause.
		panic("templates: embedded base tree missing: " + err.Error())
	}
	return sub
}
