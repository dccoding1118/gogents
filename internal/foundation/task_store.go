package foundation

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type Status string

const (
	StatusQueued     Status = "queued"
	StatusProcessing Status = "processing"
	StatusDone       Status = "done"
	StatusReporting  Status = "reporting"
	StatusFailed     Status = "failed"
	StatusDeleted    Status = "deleted"
)

var (
	ErrIllegalTransition = errors.New("illegal task state transition")
	ErrTaskIO            = errors.New("task I/O error")
	ErrTaskCorrupt       = errors.New("task corrupt or parse error")
)

type SourceFields struct {
	IssueNumber int       `yaml:"issue_number"`
	Title       string    `yaml:"title"`
	Labels      []string  `yaml:"labels"`
	Assignee    string    `yaml:"assignee,omitempty"`
	Assignees   []string  `yaml:"assignees,omitempty"`
	ClosedAt    *string   `yaml:"closed_at,omitempty"`
	State       string    `yaml:"state,omitempty"`
	UpdatedAt   time.Time `yaml:"updated_at,omitempty"`
	URL         string    `yaml:"url,omitempty"`
}

type ExecInfo struct {
	Branch         string     `yaml:"branch"`
	StartedAt      *time.Time `yaml:"started_at,omitempty"`
	FinishedAt     *time.Time `yaml:"finished_at,omitempty"`
	PrURL          string     `yaml:"pr_url,omitempty"`
	FailureCount   int        `yaml:"failure_count"`
	ReportingError string     `yaml:"reporting_error,omitempty"`
}

type FeedbackInfo struct {
	CommentID int64  `yaml:"comment_id,omitempty"`
	LastLabel string `yaml:"last_label,omitempty"`
}

type Task struct {
	ID        string       `yaml:"id"`
	Status    Status       `yaml:"status"`
	Source    SourceFields `yaml:"source"`
	Execution ExecInfo     `yaml:"execution"`
	Feedback  FeedbackInfo `yaml:"feedback"`
}

type TaskPatch struct {
	FailureCount   *int
	StartedAt      *time.Time
	FinishedAt     *time.Time
	PrURL          *string
	CommentID      *int64
	LastLabel      *string
	Source         *SourceFields
	SourceLabels   *[]string
	Branch         *string
	ReportingError *string
}

type TaskStore interface {
	List() ([]Task, error)
	Get(id string) (Task, error)
	Put(t Task) error
	Transition(id string, to Status, patch TaskPatch) (Task, error)
	// UpdateFields applies a TaskPatch without changing status.
	// Used for same-status field updates (e.g. reporting → reporting partial-success
	// in D03 §3.4) where Transition would be rejected by the state machine.
	UpdateFields(id string, patch TaskPatch) (Task, error)
}

type defaultTaskStore struct {
	paths WorkspacePaths
	muMap sync.Map // key: task id, value: *sync.Mutex
}

func NewTaskStore(paths WorkspacePaths) TaskStore {
	return &defaultTaskStore{
		paths: paths,
	}
}

func (s *defaultTaskStore) getLock(id string) *sync.Mutex {
	v, _ := s.muMap.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (s *defaultTaskStore) List() ([]Task, error) {
	entries, err := os.ReadDir(s.paths.TasksDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrTaskIO, err)
	}

	var tasks []Task
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		task, err := s.Get(e.Name())
		if err != nil {
			// 跳過損毀或不存在的任務 YAML，讓 Sync Round 能繼續
			if errors.Is(err, ErrTaskCorrupt) || errors.Is(err, ErrTaskIO) {
				log.Printf("[task_store] skipping invalid task directory %s: %v", e.Name(), err)
				continue
			}
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *defaultTaskStore) Get(id string) (Task, error) {
	var task Task
	data, err := os.ReadFile(s.paths.TaskYaml(id))
	if err != nil {
		if os.IsNotExist(err) {
			return task, fmt.Errorf("%w: not found", ErrTaskIO)
		}
		return task, fmt.Errorf("%w: %v", ErrTaskIO, err)
	}

	if err := yaml.Unmarshal(data, &task); err != nil {
		return task, fmt.Errorf("%w: %v", ErrTaskCorrupt, err)
	}
	// 舊 task.yaml 可能未寫入 issue_number；以 task ID 推回避免下游用到 0
	if task.Source.IssueNumber == 0 {
		if n := issueNumberFromID(task.ID); n != 0 {
			task.Source.IssueNumber = n
		}
	}
	return task, nil
}

// issueNumberFromID extracts the trailing integer from a task ID (e.g. "github-42").
// Returns 0 when the format does not match.
func issueNumberFromID(taskID string) int {
	idx := strings.LastIndex(taskID, "-")
	if idx < 0 || idx == len(taskID)-1 {
		return 0
	}
	n, err := strconv.Atoi(taskID[idx+1:])
	if err != nil {
		return 0
	}
	return n
}

func (s *defaultTaskStore) Put(t Task) error {
	mu := s.getLock(t.ID)
	mu.Lock()
	defer mu.Unlock()

	return s.writeTask(t)
}

// writeTask performs the actual file I/O for persisting a task.
// Caller MUST hold the lock for the task ID.
func (s *defaultTaskStore) writeTask(t Task) error {
	taskDir := s.paths.TaskDir(t.ID)
	if err := os.MkdirAll(taskDir, 0o750); err != nil {
		return fmt.Errorf("%w: %v", ErrTaskIO, err)
	}

	data, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	taskYamlPath := s.paths.TaskYaml(t.ID)
	tmpPath := filepath.Clean(fmt.Sprintf("%s.tmp.%d.%d", taskYamlPath, os.Getpid(), time.Now().UnixNano()))

	// write tmp
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- path derived from trusted workspace config
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTaskIO, err)
	}

	writeOK := false
	defer func() {
		if !writeOK {
			if cErr := f.Close(); cErr != nil {
				log.Printf("[task_store] failed to close tmp file on cleanup: %v", cErr)
			}
			if rErr := os.Remove(tmpPath); rErr != nil {
				log.Printf("[task_store] failed to remove tmp file on cleanup: %v", rErr)
			}
		}
	}()

	if _, wErr := f.Write(data); wErr != nil {
		return fmt.Errorf("%w: %v", ErrTaskIO, wErr)
	}
	if sErr := f.Sync(); sErr != nil {
		return fmt.Errorf("%w: %v", ErrTaskIO, sErr)
	}
	if cErr := f.Close(); cErr != nil {
		return fmt.Errorf("%w: %v", ErrTaskIO, cErr)
	}
	writeOK = true

	// rename ensures atomicity (POSIX)
	if rErr := os.Rename(tmpPath, taskYamlPath); rErr != nil {
		return fmt.Errorf("%w: %v", ErrTaskIO, rErr)
	}

	// sync parent dir for metadata durability
	s.syncDir(taskDir)

	return nil
}

// syncDir performs best-effort fsync on a directory.
func (s *defaultTaskStore) syncDir(dir string) {
	cleanDir := filepath.Clean(dir)
	dirF, err := os.Open(cleanDir) // #nosec G304 -- path from trusted workspace config
	if err != nil {
		return
	}
	_ = dirF.Sync() // best-effort
	if cErr := dirF.Close(); cErr != nil {
		log.Printf("[task_store] failed to close dir handle: %v", cErr)
	}
}

func (s *defaultTaskStore) Transition(id string, to Status, patch TaskPatch) (Task, error) {
	mu := s.getLock(id)
	mu.Lock()
	defer mu.Unlock()

	var task Task
	data, err := os.ReadFile(s.paths.TaskYaml(id))
	if err != nil && !os.IsNotExist(err) {
		return task, fmt.Errorf("%w: %v", ErrTaskIO, err)
	}
	if err == nil {
		if uerr := yaml.Unmarshal(data, &task); uerr != nil {
			return task, fmt.Errorf("%w: %v", ErrTaskCorrupt, uerr)
		}
		if task.Source.IssueNumber == 0 {
			if n := issueNumberFromID(task.ID); n != 0 {
				task.Source.IssueNumber = n
			}
		}
	} else {
		// New task
		task.ID = id
	}

	from := task.Status
	if from == "" {
		from = StatusQueued // or default
	}

	if !isValidTransition(from, to, task.Execution.FailureCount) {
		return task, fmt.Errorf("%w: from %s to %s", ErrIllegalTransition, from, to)
	}

	if to != "" {
		task.Status = to
	}
	applyPatch(&task, patch)

	// inline write (already holding lock, call writeTask directly)
	if wErr := s.writeTask(task); wErr != nil {
		return task, wErr
	}

	return task, nil
}

func (s *defaultTaskStore) UpdateFields(id string, patch TaskPatch) (Task, error) {
	mu := s.getLock(id)
	mu.Lock()
	defer mu.Unlock()

	var task Task
	data, err := os.ReadFile(s.paths.TaskYaml(id))
	if err != nil {
		if os.IsNotExist(err) {
			return task, fmt.Errorf("%w: not found", ErrTaskIO)
		}
		return task, fmt.Errorf("%w: %v", ErrTaskIO, err)
	}
	if uerr := yaml.Unmarshal(data, &task); uerr != nil {
		return task, fmt.Errorf("%w: %v", ErrTaskCorrupt, uerr)
	}
	if task.Source.IssueNumber == 0 {
		if n := issueNumberFromID(task.ID); n != 0 {
			task.Source.IssueNumber = n
		}
	}

	applyPatch(&task, patch)

	if wErr := s.writeTask(task); wErr != nil {
		return task, wErr
	}
	return task, nil
}

// applyPatch mutates a Task in place using non-nil fields of TaskPatch.
// Status changes belong to Transition; this helper does not touch task.Status.
func applyPatch(task *Task, patch TaskPatch) {
	if patch.FailureCount != nil {
		task.Execution.FailureCount = *patch.FailureCount
	}
	if patch.StartedAt != nil {
		task.Execution.StartedAt = patch.StartedAt
	}
	if patch.FinishedAt != nil {
		task.Execution.FinishedAt = patch.FinishedAt
	}
	if patch.PrURL != nil {
		task.Execution.PrURL = *patch.PrURL
	}
	if patch.Branch != nil {
		task.Execution.Branch = *patch.Branch
	}
	if patch.ReportingError != nil {
		task.Execution.ReportingError = *patch.ReportingError
	}
	if patch.CommentID != nil {
		task.Feedback.CommentID = *patch.CommentID
	}
	if patch.LastLabel != nil {
		task.Feedback.LastLabel = *patch.LastLabel
	}
	if patch.Source != nil {
		task.Source = *patch.Source
	}
	if patch.SourceLabels != nil {
		task.Source.Labels = *patch.SourceLabels
	}
}

func isValidTransition(from, to Status, failures int) bool {
	switch {
	case from == "" && to == StatusQueued:
		return true // initialization
	case from == StatusQueued && to == StatusProcessing:
		return true
	case from == StatusProcessing && to == StatusDone:
		return true
	case from == StatusProcessing && to == StatusReporting:
		return true
	case from == StatusProcessing && to == StatusFailed:
		return true
	case from == StatusProcessing && to == StatusQueued:
		return true // restart recovery (D02 §B1.8)
	case from == StatusReporting && to == StatusDone:
		return true
	case from == StatusReporting && to == StatusFailed:
		return true
	case from == StatusDone && to == StatusQueued:
		return true // R03: issue 重新開啟或被人為加回觸發 label
	case from == StatusFailed && to == StatusQueued:
		return failures < 3 // 根據 config，這裡在更上層會有邏輯判斷，這裡僅簡易判斷或放行給 config 控制
	case to == StatusDeleted:
		return true
	default:
		return false
	}
}
