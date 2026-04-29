package logger

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// silenceConsole temporarily redirects os.Stdout and os.Stderr to /dev/null
// so that logger's console output does not pollute test reports.
// The original descriptors are restored via t.Cleanup.
func silenceConsole(t *testing.T) {
	t.Helper()

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("failed to open %s: %v", os.DevNull, err)
	}

	origStdout := os.Stdout
	origStderr := os.Stderr
	os.Stdout = devNull
	os.Stderr = devNull

	t.Cleanup(func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
		_ = devNull.Close()
	})
}

func TestLogger_InfoWritesToDaemonAndStdout(t *testing.T) {
	silenceConsole(t)

	// D04-01: Info event appears in daemon.jsonl and stdout.
	tmpDir := t.TempDir()
	daemonPath := filepath.Join(tmpDir, "daemon.jsonl")
	errorPath := filepath.Join(tmpDir, "error.jsonl")

	lg, err := Open(daemonPath, errorPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()

	lg.Info("sync.tick_start", "github-1", "sync started", map[string]any{"round": 1})

	_ = lg.Close()

	data, err := os.ReadFile(daemonPath) //nolint:gosec // test temp path
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("daemon.jsonl is empty after Info write")
	}

	var entry LogEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("invalid JSONL: %v", err)
	}
	if entry.Level != LevelInfo {
		t.Errorf("expected level info, got %s", entry.Level)
	}
	if entry.Event != "sync.tick_start" {
		t.Errorf("expected event sync.tick_start, got %s", entry.Event)
	}

	// error.jsonl should be empty for Info events.
	errData, _ := os.ReadFile(errorPath) //nolint:gosec // test temp path
	if len(errData) > 0 {
		t.Error("error.jsonl should be empty for Info events")
	}
}

func TestLogger_ErrorWritesToAllSinks(t *testing.T) {
	silenceConsole(t)

	// D04-02: Error event appears in daemon.jsonl, error.jsonl, and stderr.
	tmpDir := t.TempDir()
	daemonPath := filepath.Join(tmpDir, "daemon.jsonl")
	errorPath := filepath.Join(tmpDir, "error.jsonl")

	lg, err := Open(daemonPath, errorPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()

	lg.Error("sync.fatal", "", "something broke", nil)

	_ = lg.Close()

	// Daemon file should have the entry.
	daemonData, _ := os.ReadFile(daemonPath) //nolint:gosec // test temp path
	if len(daemonData) == 0 {
		t.Fatal("daemon.jsonl is empty after Error write")
	}

	// Error file should also have the entry.
	errData, _ := os.ReadFile(errorPath) //nolint:gosec // test temp path
	if len(errData) == 0 {
		t.Fatal("error.jsonl is empty after Error write")
	}

	var entry LogEntry
	if err := json.Unmarshal(errData[:len(errData)-1], &entry); err != nil {
		t.Fatalf("invalid JSONL in error file: %v", err)
	}
	if entry.Level != LevelError {
		t.Errorf("expected level error, got %s", entry.Level)
	}
}

func TestLogger_TimestampTimezone(t *testing.T) {
	silenceConsole(t)

	// D04-03: ts field must use Asia/Taipei (UTC+8) format.
	tmpDir := t.TempDir()
	daemonPath := filepath.Join(tmpDir, "daemon.jsonl")
	errorPath := filepath.Join(tmpDir, "error.jsonl")

	lg, err := Open(daemonPath, errorPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()

	lg.Info("test.tz", "", "tz test", nil)

	_ = lg.Close()

	data, _ := os.ReadFile(daemonPath) //nolint:gosec // test temp path
	var entry LogEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("invalid JSONL: %v", err)
	}

	// Verify timestamp contains +08:00 timezone offset.
	if !strings.Contains(entry.TS, "+08:00") {
		t.Errorf("expected +08:00 timezone, got ts: %s", entry.TS)
	}

	// Verify millisecond precision (format: 2006-01-02T15:04:05.000Z07:00).
	parsed, pErr := time.Parse("2006-01-02T15:04:05.000Z07:00", entry.TS)
	if pErr != nil {
		t.Errorf("ts format invalid: %v", pErr)
	}
	if parsed.IsZero() {
		t.Error("parsed time is zero")
	}
}

func TestLogger_FieldConsistency(t *testing.T) {
	silenceConsole(t)

	// D04-04: level, event, message are required; fields serialize correctly.
	tmpDir := t.TempDir()
	daemonPath := filepath.Join(tmpDir, "daemon.jsonl")
	errorPath := filepath.Join(tmpDir, "error.jsonl")

	lg, err := Open(daemonPath, errorPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()

	lg.Info("test.fields", "task-42", "field test", map[string]any{
		"count": 42,
		"name":  "test",
	})

	_ = lg.Close()

	data, _ := os.ReadFile(daemonPath) //nolint:gosec // test temp path
	var entry LogEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("invalid JSONL: %v", err)
	}

	if entry.Level == "" {
		t.Error("level is empty")
	}
	if entry.Event != "test.fields" {
		t.Errorf("expected event test.fields, got %s", entry.Event)
	}
	if entry.Message != "field test" {
		t.Errorf("expected message 'field test', got %s", entry.Message)
	}
	if entry.TaskID != "task-42" {
		t.Errorf("expected task_id 'task-42', got %s", entry.TaskID)
	}
	if entry.Fields == nil {
		t.Fatal("fields should not be nil")
	}
	if entry.Fields["name"] != "test" {
		t.Errorf("expected fields.name = 'test', got %v", entry.Fields["name"])
	}
}

func TestLogger_ConcurrentWrites(t *testing.T) {
	silenceConsole(t)

	// D04-05: Multiple goroutines writing concurrently produce non-interleaved lines.
	tmpDir := t.TempDir()
	daemonPath := filepath.Join(tmpDir, "daemon.jsonl")
	errorPath := filepath.Join(tmpDir, "error.jsonl")

	lg, err := Open(daemonPath, errorPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()

	const goroutines = 10
	const writes = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writes; j++ {
				lg.Info("concurrent.write", "", "msg", map[string]any{"goroutine": id, "seq": j})
			}
		}(i)
	}
	wg.Wait()

	_ = lg.Close()

	// Verify all lines are valid JSON.
	f, err := os.Open(daemonPath) //nolint:gosec // test temp path
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		var entry LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Errorf("line %d is not valid JSON: %v (content: %s)", lineCount, err, scanner.Text())
		}
	}

	expected := goroutines * writes
	if lineCount != expected {
		t.Errorf("expected %d lines, got %d", expected, lineCount)
	}
}

func TestLogger_OpenPermissionError(t *testing.T) {
	// D04-06: Open returns error when logs directory is not writable.
	_, err := Open("/nonexistent/dir/daemon.jsonl", "/nonexistent/dir/error.jsonl")
	if err == nil {
		t.Error("expected error for non-writable directory")
	}
}

func TestLogger_CloseIdempotent(t *testing.T) {
	silenceConsole(t)

	// D04-08: Close() is idempotent; Log() after Close() is no-op and does not panic.
	tmpDir := t.TempDir()
	daemonPath := filepath.Join(tmpDir, "daemon.jsonl")
	errorPath := filepath.Join(tmpDir, "error.jsonl")

	lg, err := Open(daemonPath, errorPath)
	if err != nil {
		t.Fatal(err)
	}

	// First close should succeed.
	if err := lg.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}

	// Second close should be idempotent.
	if err := lg.Close(); err != nil {
		t.Errorf("second close should be nil, got %v", err)
	}

	// Log after close should be a no-op (not panic).
	lg.Info("test.after_close", "", "should be ignored", nil)
	lg.Error("test.after_close", "", "should be ignored too", nil)
}

func TestSetTimezone(t *testing.T) {
	// SetTimezone(nil) should be a no-op.
	SetTimezone(nil)

	// SetTimezone with a valid location should update the timezone.
	utc := time.UTC
	SetTimezone(utc)
	if currentTZ() != utc {
		t.Error("SetTimezone did not update timezone")
	}

	// Restore to Asia/Taipei for other tests.
	taipei, _ := time.LoadLocation("Asia/Taipei")
	if taipei == nil {
		taipei = time.FixedZone("UTC+08:00", 8*3600)
	}
	SetTimezone(taipei)
}

func TestConvenienceMethods(t *testing.T) {
	silenceConsole(t)

	// Verify that Info, Warning, Error convenience methods delegate to Log.
	tmpDir := t.TempDir()
	daemonPath := filepath.Join(tmpDir, "daemon.jsonl")
	errorPath := filepath.Join(tmpDir, "error.jsonl")

	lg, err := Open(daemonPath, errorPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()

	lg.Info("e1", "", "info msg", nil)
	lg.Warning("e2", "", "warning msg", nil)
	lg.Error("e3", "", "error msg", nil)

	_ = lg.Close()

	f, err := os.Open(daemonPath) //nolint:gosec // test temp path
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	expectedLevels := []Level{LevelInfo, LevelWarning, LevelError}
	i := 0
	for scanner.Scan() {
		if i >= len(expectedLevels) {
			break
		}
		var entry LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Errorf("line %d invalid JSON: %v", i+1, err)
			continue
		}
		if entry.Level != expectedLevels[i] {
			t.Errorf("line %d: expected level %s, got %s", i+1, expectedLevels[i], entry.Level)
		}
		i++
	}
	if i != len(expectedLevels) {
		t.Errorf("expected %d entries, got %d", len(expectedLevels), i)
	}
}
