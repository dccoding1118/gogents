package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dccoding1118/gogents/internal/foundation"
)

func localCfg(dir, label string) foundation.Config {
	var cfg foundation.Config
	cfg.Source.Type = "local"
	cfg.Source.Path = dir
	cfg.Source.Filter.Label = label
	return cfg
}

func writeTicket(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write ticket %s: %v", name, err)
	}
}

func TestLocalSource_List(t *testing.T) {
	dir := t.TempDir()
	writeTicket(t, dir, "t-1.yaml", "id: t-1\nworker: backend\ngoal: do a\ncontext: ctx a\nstatus: queued\nupdated_at: 2026-01-01T00:00:00Z\n")
	writeTicket(t, dir, "t-2.yaml", "id: t-2\nworker: frontend\ngoal: do b\nstatus: queued\n")
	writeTicket(t, dir, "bad.yaml", "this is not a mapping")

	src := newLocalTicketSource(localCfg(dir, "backend"), &mockTestLogger{})

	t.Run("D07-01 filter by label", func(t *testing.T) {
		tasks, err := src.List(context.Background(), SourceFilter{Labels: []string{"backend"}})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(tasks) != 1 || tasks[0].TaskID != "t-1" {
			t.Fatalf("expected only backend ticket t-1, got %+v", tasks)
		}
		if tasks[0].Title != "do a" {
			t.Errorf("title not mapped from goal: %q", tasks[0].Title)
		}
	})

	t.Run("D07-02 parse failure skipped", func(t *testing.T) {
		// bad.yaml (worker mismatch anyway) must not abort the round; the good
		// backend ticket still comes back.
		tasks, err := src.List(context.Background(), SourceFilter{Labels: []string{"backend"}})
		if err != nil {
			t.Fatalf("list should not fail on one bad ticket: %v", err)
		}
		if len(tasks) != 1 {
			t.Fatalf("expected 1 task, got %d", len(tasks))
		}
	})
}

// TestLocalSource_ListStatusGate covers the producer dispatch gate: only status ==
// queued tickets are claimed; blocked / terminal / missing states are skipped.
func TestLocalSource_ListStatusGate(t *testing.T) {
	dir := t.TempDir()
	writeTicket(t, dir, "queued.yaml", "id: queued\nworker: backend\ngoal: g\nstatus: queued\n")
	writeTicket(t, dir, "blocked.yaml", "id: blocked\nworker: backend\ngoal: g\nstatus: blocked\n")
	writeTicket(t, dir, "done.yaml", "id: done\nworker: backend\ngoal: g\nstatus: done\n")
	writeTicket(t, dir, "failed.yaml", "id: failed\nworker: backend\ngoal: g\nstatus: failed\n")
	writeTicket(t, dir, "review.yaml", "id: review\nworker: backend\ngoal: g\nstatus: awaiting_review\n")
	writeTicket(t, dir, "nostatus.yaml", "id: nostatus\nworker: backend\ngoal: g\n")
	writeTicket(t, dir, "empty.yaml", "id: empty\nworker: backend\ngoal: g\nstatus: \"\"\n")

	src := newLocalTicketSource(localCfg(dir, "backend"), &mockTestLogger{})

	t.Run("D07-01a only queued claimed", func(t *testing.T) {
		tasks, err := src.List(context.Background(), SourceFilter{Labels: []string{"backend"}})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(tasks) != 1 || tasks[0].TaskID != "queued" {
			t.Fatalf("expected only the queued ticket, got %+v", tasks)
		}
	})
}

// TestLocalSource_ListLabelStatusCross covers the label × status cross filter:
// a queued ticket for another worker, and a non-queued ticket for this worker,
// are both skipped; only this worker's queued ticket is returned.
func TestLocalSource_ListLabelStatusCross(t *testing.T) {
	dir := t.TempDir()
	writeTicket(t, dir, "mine-queued.yaml", "id: mine-queued\nworker: backend\ngoal: g\nstatus: queued\n")
	writeTicket(t, dir, "mine-blocked.yaml", "id: mine-blocked\nworker: backend\ngoal: g\nstatus: blocked\n")
	writeTicket(t, dir, "other-queued.yaml", "id: other-queued\nworker: frontend\ngoal: g\nstatus: queued\n")

	src := newLocalTicketSource(localCfg(dir, "backend"), &mockTestLogger{})

	tasks, err := src.List(context.Background(), SourceFilter{Labels: []string{"backend"}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 1 || tasks[0].TaskID != "mine-queued" {
		t.Fatalf("D07-01b expected only mine-queued, got %+v", tasks)
	}
}

func TestLocalSource_Get(t *testing.T) {
	dir := t.TempDir()
	writeTicket(t, dir, "t-1.yaml", "id: t-1\nworker: backend\ngoal: g\n")
	src := newLocalTicketSource(localCfg(dir, "backend"), &mockTestLogger{})

	t.Run("found", func(t *testing.T) {
		st, err := src.Get(context.Background(), "t-1")
		if err != nil || st.TaskID != "t-1" {
			t.Fatalf("get t-1: %+v %v", st, err)
		}
	})

	t.Run("D07-03 not found", func(t *testing.T) {
		_, err := src.Get(context.Background(), "missing")
		if !errors.Is(err, ErrSourceNotFound) {
			t.Fatalf("expected ErrSourceNotFound, got %v", err)
		}
	})
}

func TestLocalProbe(t *testing.T) {
	t.Run("readable dir passes", func(t *testing.T) {
		p := newLocalTicketProbe(localCfg(t.TempDir(), "x"), &mockTestLogger{})
		if err := p.CheckSource(); err != nil {
			t.Fatalf("expected pass, got %v", err)
		}
	})

	t.Run("D07-05 missing dir fails", func(t *testing.T) {
		p := newLocalTicketProbe(localCfg(filepath.Join(t.TempDir(), "nope"), "x"), &mockTestLogger{})
		if err := p.CheckSource(); err == nil {
			t.Fatal("expected error for missing inbox")
		}
	})
}

// D07-05a — source.path resolution: default follows GOGENTS_HOME, explicit
// values are taken as-is apart from a leading ~/.
func TestExpandTicketDir(t *testing.T) {
	t.Run("empty path defaults under global home", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("GOGENTS_HOME", home)
		if got, want := expandTicketDir(""), filepath.Join(home, "tickets"); got != want {
			t.Fatalf("expandTicketDir(\"\") = %q, want %q", got, want)
		}
	})

	t.Run("explicit path wins over global home", func(t *testing.T) {
		t.Setenv("GOGENTS_HOME", t.TempDir())
		if got := expandTicketDir("/srv/tickets"); got != "/srv/tickets" {
			t.Fatalf("expandTicketDir = %q, want /srv/tickets", got)
		}
	})

	t.Run("leading ~/ expands to user home", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no user home")
		}
		if got, want := expandTicketDir("~/inbox"), filepath.Join(home, "inbox"); got != want {
			t.Fatalf("expandTicketDir = %q, want %q", got, want)
		}
	})
}

func TestLocalContentFetcher(t *testing.T) {
	dir := t.TempDir()
	writeTicket(t, dir, "t-1.yaml", "id: t-1\nworker: backend\ngoal: My Goal\ncontext: The body\n")
	f := newLocalTicketContentFetcher(localCfg(dir, "backend"), &mockTestLogger{})

	c, err := f.Fetch(context.Background(), "t-1")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if c.Title != "My Goal" || c.Body != "The body" {
		t.Errorf("D07-06 content mismatch: %+v", c)
	}
	if len(c.Comments) != 0 {
		t.Errorf("local content should have no comments, got %d", len(c.Comments))
	}

	t.Run("missing ticket → not found", func(t *testing.T) {
		if _, err := f.Fetch(context.Background(), "missing"); !errors.Is(err, ErrSourceNotFound) {
			t.Errorf("expected ErrSourceNotFound, got %v", err)
		}
	})
}

func TestLocalVerifier_MissingTicket(t *testing.T) {
	v := newLocalTicketVerifier(localCfg(t.TempDir(), "backend"), &mockTestLogger{})
	_, ok, err := v.VerifySpec(context.Background(), "gone")
	if ok || err != nil {
		t.Errorf("missing ticket should yield ok=false, nil err; got ok=%t err=%v", ok, err)
	}
}

func TestLocalVerifier(t *testing.T) {
	dir := t.TempDir()
	writeTicket(t, dir, "with.yaml", "id: with\nworker: backend\nverify:\n  cmd: \"pytest\"\n")
	writeTicket(t, dir, "without.yaml", "id: without\nworker: backend\n")
	v := newLocalTicketVerifier(localCfg(dir, "backend"), &mockTestLogger{})

	t.Run("D07-07 declared", func(t *testing.T) {
		spec, ok, err := v.VerifySpec(context.Background(), "with")
		if err != nil || !ok || spec.Cmd != "pytest" {
			t.Fatalf("expected declared verify, got %+v ok=%t err=%v", spec, ok, err)
		}
	})
	t.Run("D07-07 not declared", func(t *testing.T) {
		_, ok, err := v.VerifySpec(context.Background(), "without")
		if err != nil || ok {
			t.Fatalf("expected ok=false, got ok=%t err=%v", ok, err)
		}
	})
}

func TestLocalReporter_WriteBack(t *testing.T) {
	dir := t.TempDir()
	// Ticket carries producer-owned governance fields; they must survive write-back.
	writeTicket(t, dir, "t-1.yaml", "id: t-1\nworker: backend\ngoal: g\nmission: m-42\ndepends_on:\n  - t-0\nstatus: running\n")
	r := newLocalTicketReporter(localCfg(dir, "backend"), &mockTestLogger{})

	t.Run("D07-08 success code result", func(t *testing.T) {
		out := r.Report(context.Background(), FeedbackInput{TaskID: "t-1", Trigger: TriggerCodingSuccess, PrURL: "https://pr/1", Message: "done"})
		if out.Result != FeedbackSuccess {
			t.Fatalf("expected success, got %+v", out)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "t-1.yaml")) //nolint:gosec // test tmp path
		s := string(raw)
		if !strings.Contains(s, "status: done") {
			t.Errorf("status not done: %s", s)
		}
		if !strings.Contains(s, "type: code") || !strings.Contains(s, "https://pr/1") {
			t.Errorf("code result missing: %s", s)
		}
		// D07-11: governance fields preserved.
		if !strings.Contains(s, "mission: m-42") || !strings.Contains(s, "t-0") {
			t.Errorf("governance fields lost: %s", s)
		}
	})

	t.Run("D07-09 success artifacts (no pr_url)", func(t *testing.T) {
		out := r.Report(context.Background(), FeedbackInput{TaskID: "t-1", Trigger: TriggerCodingSuccess, Message: "built"})
		if out.Result != FeedbackSuccess {
			t.Fatalf("expected success, got %+v", out)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "t-1.yaml")) //nolint:gosec // test tmp path
		if !strings.Contains(string(raw), "type: artifacts") {
			t.Errorf("artifacts result missing: %s", string(raw))
		}
	})

	t.Run("D07-10 failed result", func(t *testing.T) {
		out := r.Report(context.Background(), FeedbackInput{TaskID: "t-1", Trigger: TriggerCodingFailed, Message: "boom"})
		if out.Result != FeedbackSuccess {
			t.Fatalf("write should succeed, got %+v", out)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "t-1.yaml")) //nolint:gosec // test tmp path
		if !strings.Contains(string(raw), "status: failed") || !strings.Contains(string(raw), "type: error") {
			t.Errorf("failed result missing: %s", string(raw))
		}
	})

	t.Run("task_created is a no-op success", func(t *testing.T) {
		out := r.Report(context.Background(), FeedbackInput{TaskID: "t-1", Trigger: TriggerTaskCreated})
		if out.Result != FeedbackSuccess {
			t.Fatalf("task_created should be no-op success, got %+v", out)
		}
	})
}

func TestLocalReporter_WriteFailure(t *testing.T) {
	// Point the inbox at a path whose parent is a regular file so MkdirAll fails
	// regardless of the test uid.
	base := t.TempDir()
	filePath := filepath.Join(base, "afile")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newLocalTicketReporter(localCfg(filePath, "backend"), &mockTestLogger{})
	out := r.Report(context.Background(), FeedbackInput{TaskID: "t-1", Trigger: TriggerCodingFailed, Message: "boom"})
	if out.Result != FeedbackFailure {
		t.Fatalf("D07-12 expected FeedbackFailure on write error, got %+v", out)
	}
}

func TestLocalDispatch(t *testing.T) {
	cfg := localCfg(t.TempDir(), "backend")
	lg := &mockTestLogger{}

	if s, err := NewTaskSourceFromConfig(cfg, lg); err != nil || s.Kind() != "local" {
		t.Fatalf("D07 dispatch source: %v kind=%v", err, s)
	}
	if _, err := NewSourceProbeFromConfig(cfg, lg); err != nil {
		t.Fatalf("dispatch probe: %v", err)
	}
	if _, err := NewFeedbackReporterFromConfig(cfg, lg); err != nil {
		t.Fatalf("dispatch feedback: %v", err)
	}
	if _, err := NewTaskContentFetcherFromConfig(cfg, lg); err != nil {
		t.Fatalf("dispatch fetcher: %v", err)
	}
	if _, err := NewTaskVerifierFromConfig(cfg, lg); err != nil {
		t.Fatalf("dispatch verifier: %v", err)
	}

	// github verifier reports ok=false (fallback).
	var gh foundation.Config
	gh.Source.Type = "github_issue"
	v, _ := NewTaskVerifierFromConfig(gh, lg)
	if _, ok, _ := v.VerifySpec(context.Background(), "github-1"); ok {
		t.Error("github verifier should report ok=false")
	}
}
