package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()
	if cerr := w.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}
	out, rerr := io.ReadAll(r)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	return string(out)
}

// CH-011 §3.2: `version` and `--version` are equivalent, exit 0, and print the
// identity to stdout.
func TestDispatch_Version(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		var code int
		out := captureStdout(t, func() { code = Dispatch([]string{arg}) })

		if code != 0 {
			t.Errorf("%s: want exit 0, got %d", arg, code)
		}
		if !strings.HasPrefix(out, "gogents ") {
			t.Errorf("%s: want output starting with `gogents `, got %q", arg, out)
		}
		for _, field := range []string{"commit:", "go:", "platform:"} {
			if !strings.Contains(out, field) {
				t.Errorf("%s: missing %q in %q", arg, field, out)
			}
		}
	}
}

// CH-011 §4: the no-args usage lists version alongside the other commands.
func TestDispatch_UsageListsVersion(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	code := Dispatch(nil)
	if cerr := w.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}
	os.Stderr = orig

	out, rerr := io.ReadAll(r)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	if code != 1 {
		t.Errorf("want exit 1 with no args, got %d", code)
	}
	if !strings.Contains(string(out), "version") {
		t.Errorf("usage should list version, got %q", out)
	}
}
