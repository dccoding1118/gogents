package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dccoding/gogents/internal/foundation"
)

type mockConfigLoader struct {
	loadRes foundation.Config
	loadErr error
	prefErr error
}

func (m *mockConfigLoader) Load(dir string) (foundation.Config, error) {
	return m.loadRes, m.loadErr
}

func (m *mockConfigLoader) Preflight(cfg foundation.Config, probe foundation.ToolchainProbe) error {
	return m.prefErr
}

type mockPaths struct {
	foundation.WorkspacePaths
	ws string
}

func (m *mockPaths) Init() error         { return nil }
func (m *mockPaths) StateYaml() string   { return filepath.Join(m.ws, "state.yaml") }
func (m *mockPaths) RuntimeYaml() string { return filepath.Join(m.ws, "runtime.yaml") }
func (m *mockPaths) TasksDir() string    { return filepath.Join(m.ws, "tasks") }
func (m *mockPaths) Gogents() string     { return filepath.Join(m.ws, ".gogents") }

type mockGit struct {
	cloneErr error
	fetchErr error
}

func (m *mockGit) Clone(ctx context.Context, url, dir string) error { return m.cloneErr }
func (m *mockGit) Fetch(ctx context.Context, dir string) error      { return m.fetchErr }

type mockExecutor struct {
	runErr error
}

func (m *mockExecutor) Run(ctx context.Context, t foundation.Task) error { return m.runErr }

type mockAgentProbe struct {
	err error
}

func (m *mockAgentProbe) CheckAgent(spec foundation.AgentSpec) error { return m.err }

func TestDaemon_Run_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	_ = os.MkdirAll(repoDir, 0o750)
	_ = os.MkdirAll(filepath.Join(repoDir, ".git"), 0o750)

	lg := &mockLogger{}
	cfg := foundation.Config{
		RuntimeName: "test",
	}
	cfg.Project.Repo.URL = "https://gh.com/u/repo"
	cfg.Source.SyncInterval = 10 * time.Millisecond

	deps := Deps{
		ConfigLoader: &mockConfigLoader{loadRes: cfg},
		NewPaths:     func(ws string) foundation.WorkspacePaths { return &mockPaths{ws: ws} },
		NewStore:     func(p foundation.WorkspacePaths) foundation.TaskStore { return &mockStore{} },
		Logger:       lg,
		Source:       &mockSource{},
		SourceProbe:  &mockSourceProbe{},
		Git:          &mockGit{},
		Executor:     &mockExecutor{},
		AgentProbe:   &mockAgentProbe{},
	}

	d := New(deps)

	t.Run("D02-08 Startup sequence", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- d.Run(ctx, tmpDir)
		}()

		select {
		case err := <-errCh:
			if err != nil && !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "deadline") {
				t.Logf("daemon exited with: %v", err)
			}
		case <-time.After(200 * time.Millisecond):
			cancel()
		}
	})
}

type mockSourceProbe struct{ err error }

func (m *mockSourceProbe) CheckSource() error { return m.err }

type mockGlobalConfig struct {
	upserts []foundation.RuntimeRegistration
}

func (m *mockGlobalConfig) Load() (foundation.GlobalConfig, error) {
	return foundation.GlobalConfig{}, nil
}

func (m *mockGlobalConfig) Upsert(r foundation.RuntimeRegistration) error {
	m.upserts = append(m.upserts, r)
	return nil
}

func (m *mockGlobalConfig) Resolve(string) (foundation.RuntimeRegistration, error) {
	return foundation.RuntimeRegistration{}, nil
}

// TestDaemon_RegistrationAndRuntimeState covers D02-15 (startup registers the
// runtime) and D02-16 (runtime_state is initialised to idle).
func TestDaemon_RegistrationAndRuntimeState(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	_ = os.MkdirAll(filepath.Join(repoDir, ".git"), 0o750)

	cfg := foundation.Config{RuntimeName: "worker-a"}
	cfg.Project.Repo.URL = "https://gh.com/u/repo"
	cfg.Source.SyncInterval = 10 * time.Millisecond

	mgc := &mockGlobalConfig{}
	stateStore := NewStateStore(filepath.Join(tmpDir, "state.yaml"))

	deps := Deps{
		ConfigLoader: &mockConfigLoader{loadRes: cfg},
		NewPaths:     func(ws string) foundation.WorkspacePaths { return &mockPaths{ws: ws} },
		NewStore:     func(p foundation.WorkspacePaths) foundation.TaskStore { return &mockStore{} },
		Logger:       &mockLogger{},
		Source:       &mockSource{},
		SourceProbe:  &mockSourceProbe{},
		Git:          &mockGit{},
		Executor:     &mockExecutor{},
		AgentProbe:   &mockAgentProbe{},
		StateStore:   stateStore,
		GlobalConfig: mgc,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- New(deps).Run(ctx, tmpDir) }()
	<-errCh

	if len(mgc.upserts) == 0 {
		t.Fatal("D02-15 expected runtime registration upsert")
	}
	if mgc.upserts[0].Workspace != tmpDir || mgc.upserts[0].Name != "worker-a" {
		t.Errorf("registration mismatch: %+v", mgc.upserts[0])
	}

	st, _ := stateStore.Load()
	if st.RuntimeState != foundation.RuntimeIdle {
		t.Errorf("D02-16 expected runtime_state idle, got %q", st.RuntimeState)
	}
}

func TestResolveRepoDir(t *testing.T) {
	dir, err := resolveRepoDir("/tmp", "https://github.com/user/gogents.git")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "gogents" {
		t.Errorf("expected gogents, got %s", dir)
	}
}

func TestExitError(t *testing.T) {
	err := &ExitError{Code: 5, Msg: "fail"}
	if !strings.Contains(err.Error(), "exit 5") {
		t.Error("error format")
	}
}
