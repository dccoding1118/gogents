// Package version resolves the identity of the running binary: the release
// version, source commit, build time, toolchain and target platform. It is the
// single decision point for how those values are derived (CH-011 §3.1).
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Injected at build time by the release pipeline via -ldflags -X. Left empty
// for local builds, which fall back to the VCS stamps in the build info.
var (
	version string
	commit  string
	date    string
)

const unknown = "unknown"

// Info is the resolved identity of the binary.
type Info struct {
	Version   string
	Commit    string
	Date      string
	GoVersion string
	Platform  string
}

// Get resolves the binary's identity, preferring the ldflags-injected values
// and falling back to the build info stamps.
func Get() Info {
	return resolve(debug.ReadBuildInfo)
}

// resolve takes the build info reader as a parameter so tests can supply one.
func resolve(readBuildInfo func() (*debug.BuildInfo, bool)) Info {
	info := Info{
		Version:   version,
		Commit:    commit,
		Date:      date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	if bi, ok := readBuildInfo(); ok && bi != nil {
		fillFromBuildInfo(&info, bi)
	}

	if info.Version == "" {
		info.Version = "dev"
	}
	if info.Commit == "" {
		info.Commit = unknown
	}
	if info.Date == "" {
		info.Date = unknown
	}
	return info
}

// fillFromBuildInfo fills only the fields ldflags left empty.
func fillFromBuildInfo(info *Info, bi *debug.BuildInfo) {
	// A plain `go build` reports "(devel)", which identifies nothing.
	if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		info.Version = bi.Main.Version
	}

	var revision string
	var modified bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			if info.Date == "" {
				info.Date = s.Value
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if info.Commit == "" && revision != "" {
		info.Commit = revision
		if modified {
			info.Commit += "-dirty"
		}
	}
}

// String renders the identity for human reading. The first line carries the
// version alone so downstream tooling can parse it without a JSON contract.
func (i Info) String() string {
	return fmt.Sprintf("gogents %s\ncommit:   %s (%s)\ngo:       %s\nplatform: %s",
		i.Version, shortCommit(i.Commit), i.Date, i.GoVersion, i.Platform)
}

// shortCommit trims a full SHA to its first 7 characters, preserving any
// "-dirty" suffix and leaving non-SHA values (e.g. "unknown") untouched.
func shortCommit(c string) string {
	const shaLen = 7
	suffix := ""
	base := c
	if n := len(c) - len("-dirty"); n > 0 && c[n:] == "-dirty" {
		base, suffix = c[:n], "-dirty"
	}
	if len(base) <= shaLen || base == unknown {
		return c
	}
	return base[:shaLen] + suffix
}
