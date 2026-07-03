package source

import (
	"context"
	"errors"
	"time"
)

// TaskSource is the abstract interface for task origin systems.
// Owned by D02 (daemon-sync); implemented by concrete strategies (e.g. D05 github-client).
// Syncer interacts with task sources exclusively through this interface.
type TaskSource interface {
	// Kind returns the source type identifier (e.g. "github_issue") for log fields.
	Kind() string

	// List returns source tasks matching the given filter criteria.
	List(ctx context.Context, filter SourceFilter) ([]SourceTask, error)

	// Get retrieves a single source task by its task ID (e.g. "github-42").
	Get(ctx context.Context, taskID string) (SourceTask, error)
}

// SourceFilter specifies criteria for listing source tasks.
type SourceFilter struct {
	Labels []string    // All labels must match (AND semantics).
	State  SourceState // Typically SourceStateOpen.
}

// SourceState represents the state of a source task.
type SourceState string

const (
	SourceStateOpen   SourceState = "open"
	SourceStateClosed SourceState = "closed"
)

// SourceTask is the minimal field set needed during sync.
// Does not include body/comments — those are for execution stage (Stage 3).
type SourceTask struct {
	TaskID    string // e.g. "github-42"; format decided by the concrete implementation.
	State     SourceState
	UpdatedAt time.Time
	URL       string
	Title     string
	Labels    []string
	Assignees []string
}

// Sentinel errors for TaskSource implementations.
// Implementations MUST classify underlying errors into one of these three categories.
var (
	ErrSourceNotFound   = errors.New("source task not found")   // Task explicitly deleted; D02 transitions to deleted.
	ErrSourceAuth       = errors.New("source auth failed")      // Auth failure; D02 logs and retries next cycle.
	ErrSourceTransient  = errors.New("source transient error")  // Network/5xx/rate-limit; D02 retries next cycle.
	ErrSourceProgrammer = errors.New("source programmer error") // Logic error; D02 treats as per-source error.
)

// SourceProbe checks the availability and authorization of a specific source backend.
// Used by D01 Preflight to validate toolchain readiness before entering the main loop.
type SourceProbe interface {
	CheckSource() error
}

// TaskContentFetcher retrieves the full task content (body + comments) needed
// for input.md assembly during execution. Owned by D03 (consumer side); concrete
// implementation lives in the same source package alongside TaskSource so the
// gh runner can be shared.
type TaskContentFetcher interface {
	Fetch(ctx context.Context, taskID string) (TaskContent, error)
}

// TaskContent is the full content of a task fetched at execution time.
type TaskContent struct {
	URL      string
	Title    string
	Body     string
	Author   string
	Comments []Comment
}

// Comment is a single user comment on the source task.
type Comment struct {
	Author    string
	CreatedAt time.Time
	Body      string
}

// TaskVerifier reports the ticket-declared verification command for a task.
// Owned by D03 (consumer). github_issue returns ok=false (falls back to the
// result.json verdict); local reads verify.cmd from the ticket file.
type TaskVerifier interface {
	// VerifySpec returns the verify spec and whether one is declared. When ok is
	// false the caller falls back to the result.json coding verdict (R07).
	VerifySpec(ctx context.Context, taskID string) (VerifySpec, bool, error)
}

// VerifySpec is a ticket-declared verification command. Exit code 0 = pass.
type VerifySpec struct {
	Cmd string
}
