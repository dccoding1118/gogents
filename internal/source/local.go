package source

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/internal/logger"
)

// defaultTicketInbox is the am ticket inbox used when source.path is empty.
const defaultTicketInbox = "~/.am/tickets"

// ticketFile mirrors the subset of the am ticket schema gogents reads or writes.
// Governance fields owned by am (mission, depends_on, approvals, risk, ...) are
// captured by the inline Extra map so they survive a write-back untouched.
type ticketFile struct {
	ID        string         `yaml:"id,omitempty"`
	Worker    string         `yaml:"worker,omitempty"`
	Goal      string         `yaml:"goal,omitempty"`
	Context   string         `yaml:"context,omitempty"`
	Verify    *ticketVerify  `yaml:"verify,omitempty"`
	Status    string         `yaml:"status,omitempty"`
	Result    any            `yaml:"result,omitempty"`
	UpdatedAt string         `yaml:"updated_at,omitempty"`
	Extra     map[string]any `yaml:",inline"`
}

type ticketVerify struct {
	Cmd   string         `yaml:"cmd,omitempty"`
	Extra map[string]any `yaml:",inline"`
}

// expandTicketDir resolves source.path, defaulting to ~/.am/tickets and
// expanding a leading ~/.
func expandTicketDir(p string) string {
	if strings.TrimSpace(p) == "" {
		p = defaultTicketInbox
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func ticketPath(dir, taskID string) string {
	return filepath.Join(dir, taskID+".yaml")
}

// parseTicket reads and unmarshals a ticket YAML file.
func parseTicket(path string) (ticketFile, error) {
	var t ticketFile
	data, err := os.ReadFile(path) //nolint:gosec // path derived from configured inbox
	if err != nil {
		return t, err
	}
	if uErr := yaml.Unmarshal(data, &t); uErr != nil {
		return t, uErr
	}
	return t, nil
}

// ticketID returns the ticket's id, falling back to the file name stem.
func ticketID(t ticketFile, path string) string {
	if t.ID != "" {
		return t.ID
	}
	return strings.TrimSuffix(filepath.Base(path), ".yaml")
}

func mapTicketToSourceTask(t ticketFile, path string) SourceTask {
	var updated time.Time
	if t.UpdatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, t.UpdatedAt); err == nil {
			updated = parsed
		}
	}
	if updated.IsZero() {
		if info, err := os.Stat(path); err == nil {
			updated = info.ModTime()
		}
	}
	return SourceTask{
		TaskID:    ticketID(t, path),
		State:     SourceStateOpen,
		UpdatedAt: updated,
		URL:       "file://" + path,
		Title:     t.Goal,
		Labels:    []string{t.Worker},
	}
}

// ---------------------------------------------------------------------------
// TaskSource
// ---------------------------------------------------------------------------

type localTicketSource struct {
	dir   string
	label string
	lg    logger.Logger
}

func newLocalTicketSource(cfg foundation.Config, lg logger.Logger) TaskSource {
	return &localTicketSource{
		dir:   expandTicketDir(cfg.Source.Path),
		label: cfg.Source.Filter.Label,
		lg:    lg,
	}
}

func (s *localTicketSource) Kind() string { return "local" }

func (s *localTicketSource) List(_ context.Context, filter SourceFilter) ([]SourceTask, error) {
	want := s.label
	if len(filter.Labels) > 0 {
		want = filter.Labels[0]
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read ticket inbox %s: %v", ErrSourceTransient, s.dir, err)
	}

	var out []SourceTask
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		p := filepath.Join(s.dir, e.Name())
		t, pErr := parseTicket(p)
		if pErr != nil {
			s.lg.Warning("source.ticket_parse_failed", "", "failed to parse ticket", map[string]any{
				"path":   p,
				"reason": pErr.Error(),
			})
			continue
		}
		if t.Worker != want {
			continue
		}
		// am's dispatch gate: only status == queued means the ticket's
		// depends_on are satisfied and it may be claimed. blocked / terminal /
		// missing states are left for am to advance.
		if t.Status != "queued" {
			continue
		}
		out = append(out, mapTicketToSourceTask(t, p))
	}
	return out, nil
}

func (s *localTicketSource) Get(_ context.Context, taskID string) (SourceTask, error) {
	p := ticketPath(s.dir, taskID)
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return SourceTask{}, ErrSourceNotFound
		}
		return SourceTask{}, fmt.Errorf("%w: %v", ErrSourceTransient, err)
	}
	t, err := parseTicket(p)
	if err != nil {
		return SourceTask{}, fmt.Errorf("%w: %v", ErrSourceTransient, err)
	}
	return mapTicketToSourceTask(t, p), nil
}

// ---------------------------------------------------------------------------
// SourceProbe
// ---------------------------------------------------------------------------

type localTicketProbe struct {
	dir string
}

func newLocalTicketProbe(cfg foundation.Config, _ logger.Logger) SourceProbe {
	return &localTicketProbe{dir: expandTicketDir(cfg.Source.Path)}
}

func (p *localTicketProbe) CheckSource() error {
	info, err := os.Stat(p.dir)
	if err != nil {
		return fmt.Errorf("ticket inbox %s not accessible: %w", p.dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("ticket inbox %s is not a directory", p.dir)
	}
	if _, err := os.ReadDir(p.dir); err != nil {
		return fmt.Errorf("ticket inbox %s not readable: %w", p.dir, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// TaskContentFetcher
// ---------------------------------------------------------------------------

type localTicketContentFetcher struct {
	dir string
	lg  logger.Logger
}

func newLocalTicketContentFetcher(cfg foundation.Config, lg logger.Logger) TaskContentFetcher {
	return &localTicketContentFetcher{dir: expandTicketDir(cfg.Source.Path), lg: lg}
}

func (f *localTicketContentFetcher) Fetch(_ context.Context, taskID string) (TaskContent, error) {
	p := ticketPath(f.dir, taskID)
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return TaskContent{}, ErrSourceNotFound
		}
		return TaskContent{}, fmt.Errorf("%w: %v", ErrSourceTransient, err)
	}
	t, err := parseTicket(p)
	if err != nil {
		return TaskContent{}, fmt.Errorf("%w: %v", ErrSourceTransient, err)
	}
	return TaskContent{
		URL:    "file://" + p,
		Title:  t.Goal,
		Body:   t.Context,
		Author: "am",
	}, nil
}

// ---------------------------------------------------------------------------
// TaskVerifier
// ---------------------------------------------------------------------------

type localTicketVerifier struct {
	dir string
}

func newLocalTicketVerifier(cfg foundation.Config, _ logger.Logger) TaskVerifier {
	return &localTicketVerifier{dir: expandTicketDir(cfg.Source.Path)}
}

func (v *localTicketVerifier) VerifySpec(_ context.Context, taskID string) (VerifySpec, bool, error) {
	p := ticketPath(v.dir, taskID)
	t, err := parseTicket(p)
	if err != nil {
		if os.IsNotExist(err) {
			return VerifySpec{}, false, nil
		}
		return VerifySpec{}, false, fmt.Errorf("%w: %v", ErrSourceTransient, err)
	}
	if t.Verify == nil || strings.TrimSpace(t.Verify.Cmd) == "" {
		return VerifySpec{}, false, nil
	}
	return VerifySpec{Cmd: t.Verify.Cmd}, true, nil
}

// ---------------------------------------------------------------------------
// FeedbackReporter
// ---------------------------------------------------------------------------

type localTicketReporter struct {
	dir string
	lg  logger.Logger
}

func newLocalTicketReporter(cfg foundation.Config, lg logger.Logger) FeedbackReporter {
	return &localTicketReporter{dir: expandTicketDir(cfg.Source.Path), lg: lg}
}

func (r *localTicketReporter) Report(_ context.Context, in FeedbackInput) FeedbackOutcome {
	var status, resultType string
	var result map[string]any

	switch in.Trigger {
	case TriggerTaskCreated, TriggerProcessingStarted:
		// am owns the ticket lifecycle up to dispatch; gogents only writes the
		// terminal execution result. Nothing to write here.
		return FeedbackOutcome{Result: FeedbackSuccess}

	case TriggerCodingSuccess:
		status = "done"
		if strings.TrimSpace(in.PrURL) != "" {
			resultType = "code"
			result = map[string]any{"type": "code", "pr_url": in.PrURL, "message": in.Message}
		} else {
			resultType = "artifacts"
			result = map[string]any{"type": "artifacts", "message": in.Message}
		}

	case TriggerCodingFailed:
		status = "failed"
		resultType = "error"
		result = map[string]any{"type": "error", "message": in.Message}

	default:
		r.lg.Error("source.ticket_report_failed", in.TaskID, "bad trigger", map[string]any{
			"task_id": in.TaskID,
			"trigger": string(in.Trigger),
		})
		return FeedbackOutcome{Result: FeedbackFailure, Error: "bad_trigger"}
	}

	if err := r.writeBack(in.TaskID, status, result); err != nil {
		r.lg.Warning("source.ticket_report_failed", in.TaskID, "failed to write ticket", map[string]any{
			"task_id": in.TaskID,
			"reason":  err.Error(),
		})
		return FeedbackOutcome{Result: FeedbackFailure, Error: "ticket_write: " + err.Error()}
	}

	r.lg.Info("source.ticket_reported", in.TaskID, "ticket terminal state written", map[string]any{
		"task_id":     in.TaskID,
		"status":      status,
		"result_type": resultType,
	})
	return FeedbackOutcome{Result: FeedbackSuccess}
}

// writeBack reads the ticket (preserving governance fields), sets the terminal
// status/result/updated_at, and atomically rewrites it.
func (r *localTicketReporter) writeBack(taskID, status string, result map[string]any) error {
	p := ticketPath(r.dir, taskID)
	t, err := parseTicket(p)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		t = ticketFile{ID: taskID}
	}

	t.Status = status
	t.Result = result
	t.UpdatedAt = foundation.Now().Format(time.RFC3339)

	return writeTicketAtomic(p, t)
}

// writeTicketAtomic persists a ticket using write-tmp → fsync → rename.
func writeTicketAtomic(path string, t ticketFile) error {
	data, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("failed to marshal ticket: %w", err)
	}
	if mErr := os.MkdirAll(filepath.Dir(path), 0o750); mErr != nil {
		return fmt.Errorf("failed to create ticket dir: %w", mErr)
	}

	tmpPath := filepath.Clean(fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano()))
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- trusted path
	if err != nil {
		return err
	}

	writeOK := false
	defer func() {
		if !writeOK {
			if cErr := f.Close(); cErr != nil {
				log.Printf("[local] failed to close tmp ticket on cleanup: %v", cErr)
			}
			if rErr := os.Remove(tmpPath); rErr != nil {
				log.Printf("[local] failed to remove tmp ticket on cleanup: %v", rErr)
			}
		}
	}()

	if _, wErr := f.Write(data); wErr != nil {
		return wErr
	}
	if sErr := f.Sync(); sErr != nil {
		return sErr
	}
	if cErr := f.Close(); cErr != nil {
		return cErr
	}
	writeOK = true

	return os.Rename(tmpPath, path)
}
