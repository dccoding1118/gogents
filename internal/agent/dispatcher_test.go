package agent

import (
	"errors"
	"testing"

	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/logger"
)

// mockTestLogger is a no-op logger for agent tests.
type mockTestLogger struct{}

func (m *mockTestLogger) Info(event, taskID, msg string, fields map[string]any)    {}
func (m *mockTestLogger) Warning(event, taskID, msg string, fields map[string]any) {}
func (m *mockTestLogger) Error(event, taskID, msg string, fields map[string]any)   {}
func (m *mockTestLogger) Log(level logger.Level, event, taskID, message string, fields map[string]any) {
}
func (m *mockTestLogger) Close() error { return nil }

func TestNewAgentProbeFromConfig(t *testing.T) {
	lg := &mockTestLogger{}

	cases := []struct {
		name      string
		agentType string
		wantErr   error
	}{
		// D06-08: gemini returns a probe.
		{name: "gemini", agentType: "gemini", wantErr: nil},
		// D06-08: claude returns a probe.
		{name: "claude", agentType: "claude", wantErr: nil},
		// D06-09: unsupported type returns ErrAgentUnsupportedType.
		{name: "unsupported", agentType: "gpt4", wantErr: ErrAgentUnsupportedType},
		// D06-09: empty type returns ErrAgentUnsupportedType.
		{name: "empty", agentType: "", wantErr: ErrAgentUnsupportedType},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := foundation.Config{}
			cfg.Agent.Type = tc.agentType

			probe, err := NewAgentProbeFromConfig(cfg, lg)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("expected %v, got %v", tc.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if probe == nil {
				t.Error("probe should not be nil")
			}
		})
	}
}

func TestNewAgentExecutorFromConfig(t *testing.T) {
	lg := &mockTestLogger{}

	cases := []struct {
		name      string
		agentType string
		wantErr   error
	}{
		// D06-08b: gemini returns an executor.
		{name: "gemini", agentType: "gemini", wantErr: nil},
		// D06-08b: claude returns an executor.
		{name: "claude", agentType: "claude", wantErr: nil},
		// D06-09b: unsupported type returns ErrAgentUnsupportedType.
		{name: "unsupported", agentType: "unknown_agent", wantErr: ErrAgentUnsupportedType},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := foundation.Config{}
			cfg.Agent.Type = tc.agentType

			executor, err := NewAgentExecutorFromConfig(cfg, lg)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("expected %v, got %v", tc.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if executor == nil {
				t.Error("executor should not be nil")
			}
		})
	}
}
