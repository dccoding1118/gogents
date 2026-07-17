package version

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// setInjected sets the ldflags-injected vars for one test and restores them.
func setInjected(t *testing.T, v, c, d string) {
	t.Helper()
	ov, oc, od := version, commit, date
	version, commit, date = v, c, d
	t.Cleanup(func() { version, commit, date = ov, oc, od })
}

func buildInfo(mainVersion string, settings map[string]string) func() (*debug.BuildInfo, bool) {
	bi := &debug.BuildInfo{Main: debug.Module{Version: mainVersion}}
	for k, v := range settings {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: k, Value: v})
	}
	return func() (*debug.BuildInfo, bool) { return bi, true }
}

// CH-011 §3.1: injected values win over the build info stamps.
func TestResolve_InjectedWins(t *testing.T) {
	setInjected(t, "v1.2.3", "abcdef1234567890", "2026-07-17T08:30:00Z")

	got := resolve(buildInfo("v9.9.9", map[string]string{
		"vcs.revision": "0000000000000000",
		"vcs.time":     "2000-01-01T00:00:00Z",
	}))

	if got.Version != "v1.2.3" {
		t.Errorf("Version: want v1.2.3, got %s", got.Version)
	}
	if got.Commit != "abcdef1234567890" {
		t.Errorf("Commit: want the injected sha, got %s", got.Commit)
	}
	if got.Date != "2026-07-17T08:30:00Z" {
		t.Errorf("Date: want the injected date, got %s", got.Date)
	}
}

// CH-011 §3.1: with nothing injected, `go install module@version` supplies
// Main.Version and the VCS stamps supply commit and time.
func TestResolve_BuildInfoFallback(t *testing.T) {
	setInjected(t, "", "", "")

	got := resolve(buildInfo("v0.4.0", map[string]string{
		"vcs.revision": "1a2b3c4d5e6f7890",
		"vcs.time":     "2026-07-01T10:00:00Z",
		"vcs.modified": "false",
	}))

	if got.Version != "v0.4.0" {
		t.Errorf("Version: want v0.4.0 from Main.Version, got %s", got.Version)
	}
	if got.Commit != "1a2b3c4d5e6f7890" {
		t.Errorf("Commit: want the vcs.revision, got %s", got.Commit)
	}
	if got.Date != "2026-07-01T10:00:00Z" {
		t.Errorf("Date: want the vcs.time, got %s", got.Date)
	}
}

// CH-011 §3.1: a plain `go build` reports "(devel)", which identifies nothing;
// a modified work tree marks the commit dirty.
func TestResolve_DevelAndDirty(t *testing.T) {
	setInjected(t, "", "", "")

	got := resolve(buildInfo("(devel)", map[string]string{
		"vcs.revision": "1a2b3c4d5e6f7890",
		"vcs.modified": "true",
	}))

	if got.Version != "dev" {
		t.Errorf("Version: want dev for (devel), got %s", got.Version)
	}
	if got.Commit != "1a2b3c4d5e6f7890-dirty" {
		t.Errorf("Commit: want the -dirty suffix, got %s", got.Commit)
	}
	if got.Date != unknown {
		t.Errorf("Date: want unknown with no vcs.time, got %s", got.Date)
	}
}

// CH-011 §3.1: no injection and no build info at all still yields no empty
// fields.
func TestResolve_NoSourcesAtAll(t *testing.T) {
	setInjected(t, "", "", "")

	got := resolve(func() (*debug.BuildInfo, bool) { return nil, false })

	if got.Version != "dev" || got.Commit != unknown || got.Date != unknown {
		t.Errorf("want dev/unknown/unknown, got %s/%s/%s", got.Version, got.Commit, got.Date)
	}
	if got.GoVersion != runtime.Version() {
		t.Errorf("GoVersion: want %s, got %s", runtime.Version(), got.GoVersion)
	}
	if want := runtime.GOOS + "/" + runtime.GOARCH; got.Platform != want {
		t.Errorf("Platform: want %s, got %s", want, got.Platform)
	}
}

// CH-011 §3.2: the first line carries the version alone, so downstream tooling
// can parse it without a JSON contract; the commit shows 7 characters.
func TestString_Format(t *testing.T) {
	info := Info{
		Version:   "v1.2.3",
		Commit:    "1a2b3c4d5e6f7890",
		Date:      "2026-07-17T08:30:00Z",
		GoVersion: "go1.23.0",
		Platform:  "darwin/arm64",
	}

	lines := strings.Split(info.String(), "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 lines, got %d: %q", len(lines), info.String())
	}
	if lines[0] != "gogents v1.2.3" {
		t.Errorf("first line: want `gogents v1.2.3`, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "1a2b3c4 (2026-07-17T08:30:00Z)") {
		t.Errorf("commit line: want the short sha and date, got %q", lines[1])
	}
	if strings.Contains(lines[1], "1a2b3c4d") {
		t.Errorf("commit line: sha not trimmed to 7: %q", lines[1])
	}
}

// CH-011 §3.2: "unknown" is output as-is rather than trimmed, and a dirty
// commit keeps its suffix after trimming.
func TestShortCommit(t *testing.T) {
	cases := map[string]string{
		"1a2b3c4d5e6f7890":       "1a2b3c4",
		"1a2b3c4d5e6f7890-dirty": "1a2b3c4-dirty",
		unknown:                  unknown,
		"short":                  "short",
	}
	for in, want := range cases {
		if got := shortCommit(in); got != want {
			t.Errorf("shortCommit(%q): want %q, got %q", in, want, got)
		}
	}
}
