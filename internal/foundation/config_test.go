package foundation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoader_Load(t *testing.T) {
	tmpDir := t.TempDir()
	globalHome := filepath.Join(tmpDir, ".gogents")
	_ = os.MkdirAll(globalHome, 0o750)
	paths := NewWorkspacePaths(tmpDir)
	loader := NewConfigLoader(paths)

	t.Run("D01-02 runtime.yaml missing", func(t *testing.T) {
		_, err := loader.Load(tmpDir)
		if !errors.Is(err, ErrConfigParse) {
			t.Errorf("expect ErrConfigParse, got %v", err)
		}
	})

	t.Run("D01-01 normal load", func(t *testing.T) {
		runtimeYaml := paths.RuntimeYaml()
		_ = os.MkdirAll(filepath.Dir(runtimeYaml), 0o750)
		content := `
runtime_name: "test"
project:
  repo:
    url: "https://github.com/user/repo"
source:
  type: "github_issue"
  filter:
    label: "queued"
agent:
  type: "gemini"
  model: "pro"
`
		_ = os.WriteFile(runtimeYaml, []byte(content), 0o600)
		cfg, err := loader.Load(tmpDir)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.RuntimeName != "test" {
			t.Error("load error")
		}
	})

	t.Run("validateConfigSchema: fail", func(t *testing.T) {
		err := validateConfigSchema(&Config{})
		if !errors.Is(err, ErrConfigSchema) {
			t.Errorf("expect ErrConfigSchema, got %v", err)
		}
	})
}

type mockProbe struct {
	checkSourceErr error
	checkAgentErr  error
	checkGitErr    error
}

func (m *mockProbe) CheckSource(src SourceSpec) error { return m.checkSourceErr }
func (m *mockProbe) CheckAgent(agt AgentSpec) error   { return m.checkAgentErr }
func (m *mockProbe) CheckGit() error                  { return m.checkGitErr }

func TestConfigLoader_Preflight(t *testing.T) {
	paths := NewWorkspacePaths("/tmp")
	loader := NewConfigLoader(paths)
	t.Run("Preflight: success", func(t *testing.T) {
		err := loader.Preflight(Config{}, &mockProbe{})
		if err != nil {
			t.Error(err)
		}
	})
}

func TestBuiltinProbes(t *testing.T) {
	probe := NewGitProbe("/tmp")
	_ = probe.CheckSource(SourceSpec{})
	_ = probe.CheckAgent(AgentSpec{})
	_ = probe.CheckGit()
}

func TestAgentProbeError(t *testing.T) {
	err := &AgentProbeError{Stage: "S", Reason: "R"}
	if err.Error() == "" {
		t.Error("error empty")
	}
}
