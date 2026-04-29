package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Level 採 string 而非 int，便於直接序列化與 grep
type Level string

const (
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

// Logger 是所有模組唯一對外事件出口
type Logger interface {
	// 核心：呼叫端自行指定 level / event / task_id / message / fields
	Log(level Level, event, taskID, message string, fields map[string]any)

	// 便利方法
	Info(event, taskID, message string, fields map[string]any)
	Warning(event, taskID, message string, fields map[string]any)
	Error(event, taskID, message string, fields map[string]any)

	// 生命週期
	Close() error
}

type defaultLogger struct {
	mu         sync.Mutex
	daemonFile *os.File
	errorFile  *os.File
	closed     bool
}

// LogEntry 中沒有導出的欄位因為我們自己實作 marshal。
// 不過為了內建 json 解析，可以加上 json tags。
type LogEntry struct {
	TS      string         `json:"ts"`
	Level   Level          `json:"level"`
	Event   string         `json:"event"`
	TaskID  string         `json:"task_id,omitempty"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

var (
	activeTZMu sync.RWMutex
	activeTZ   *time.Location
)

func init() {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		// Fallback for some environments (like scratch containers without tzdata),
		// but since this is running on an actual machine, it's safer to use FixedZone as fallback just in case.
		loc = time.FixedZone("UTC+08:00", 8*3600)
	}
	activeTZ = loc
}

// SetTimezone replaces the timezone used to format log timestamps process-wide.
// Daemon startup calls this once after runtime.yaml is parsed so log lines
// agree with the operator's configured zone. nil is a no-op.
func SetTimezone(loc *time.Location) {
	if loc == nil {
		return
	}
	activeTZMu.Lock()
	activeTZ = loc
	activeTZMu.Unlock()
}

func currentTZ() *time.Location {
	activeTZMu.RLock()
	defer activeTZMu.RUnlock()
	return activeTZ
}

// Open 建構 logger（由 D02 啟動時呼叫；路徑由 D01 WorkspacePaths 提供）
func Open(daemonPath, errorPath string) (Logger, error) {
	flags := os.O_APPEND | os.O_CREATE | os.O_WRONLY
	daemonFile, err := os.OpenFile(daemonPath, flags, 0o600) // #nosec G304 -- trusted path from workspace config
	if err != nil {
		return nil, fmt.Errorf("failed to open daemon log file: %w", err)
	}

	errorFile, err := os.OpenFile(errorPath, flags, 0o600) // #nosec G304 -- trusted path from workspace config
	if err != nil {
		if cErr := daemonFile.Close(); cErr != nil {
			return nil, fmt.Errorf("failed to close daemon file after error: %w", cErr)
		}
		return nil, fmt.Errorf("failed to open error log file: %w", err)
	}

	return &defaultLogger{
		daemonFile: daemonFile,
		errorFile:  errorFile,
	}, nil
}

func (l *defaultLogger) Info(event, taskID, message string, fields map[string]any) {
	l.Log(LevelInfo, event, taskID, message, fields)
}

func (l *defaultLogger) Warning(event, taskID, message string, fields map[string]any) {
	l.Log(LevelWarning, event, taskID, message, fields)
}

func (l *defaultLogger) Error(event, taskID, message string, fields map[string]any) {
	l.Log(LevelError, event, taskID, message, fields)
}

func (l *defaultLogger) Log(level Level, event, taskID, message string, fields map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return
	}

	ts := time.Now().In(currentTZ()).Format("2006-01-02T15:04:05.000Z07:00")
	entry := LogEntry{
		TS:      ts,
		Level:   level,
		Event:   event,
		TaskID:  taskID,
		Message: message,
		Fields:  fields,
	}

	bytes, err := json.Marshal(entry)
	if err != nil {
		fallbackMsg := fmt.Sprintf("[logger] write failed: json marshal error for event %s: %v\n", event, err)
		fmt.Fprint(os.Stderr, fallbackMsg)
		return
	}
	bytes = append(bytes, '\n')

	// daemon.jsonl is the canonical sink for every entry regardless of level.
	if _, err := l.daemonFile.Write(bytes); err != nil {
		fmt.Fprintf(os.Stderr, "[logger] write failed to daemon file: %v\n", err)
	}

	// Console split: errors go to stderr (and error.jsonl); everything else
	// goes to stdout. Splitting prevents a single entry from showing twice
	// when the user merges the two streams (e.g. `2>&1`).
	if level == LevelError {
		if _, err := l.errorFile.Write(bytes); err != nil {
			fmt.Fprintf(os.Stderr, "[logger] write failed to error file: %v\n", err)
		}
		if _, err := os.Stderr.Write(bytes); err != nil {
			fmt.Fprintf(os.Stderr, "[logger] write failed to stderr: %v\n", err)
		}
		return
	}
	if _, err := os.Stdout.Write(bytes); err != nil {
		fmt.Fprintf(os.Stderr, "[logger] write failed to stdout: %v\n", err)
	}
}

func (l *defaultLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}
	l.closed = true

	var lastErr error
	if err := l.daemonFile.Sync(); err != nil {
		lastErr = err
	}
	if err := l.daemonFile.Close(); err != nil {
		lastErr = err
	}

	if err := l.errorFile.Sync(); err != nil {
		lastErr = err
	}
	if err := l.errorFile.Close(); err != nil {
		lastErr = err
	}

	return lastErr
}
