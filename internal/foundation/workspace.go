package foundation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// WorkspacePaths 定義了各路徑資源的取得機制
type WorkspacePaths interface {
	Init() error  // 建立缺少的目錄（idempotent）
	Hash() string // workspace-hash (sha1 前 8 碼)

	// workspace 層級
	Root() string
	Gogents() string     // <workspace>/.gogents/
	RuntimeYaml() string // <workspace>/.gogents/runtime.yaml
	StateYaml() string   // <workspace>/.gogents/state.yaml
	TasksDir() string    // <workspace>/.gogents/tasks/
	TaskDir(id string) string
	TaskYaml(id string) string
	InputMd(id string) string
	OutputJsonl(id string) string
	ResultJson(id string) string

	// 全域（~/.gogents/）
	GlobalConfigYaml() string // ~/.gogents/config.yaml
	LogsDir() string          // ~/.gogents/logs/<hash>/
	DaemonLog() string        // ~/.gogents/logs/<hash>/daemon.jsonl
	ErrorLog() string         // ~/.gogents/logs/<hash>/error.jsonl
}

type defaultWorkspacePaths struct {
	workspaceDir string
	hash         string
	globalHome   string
}

func NewWorkspacePaths(workspaceDir string) WorkspacePaths {
	absDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		absDir = workspaceDir
	}

	h := sha256.New()
	h.Write([]byte(absDir))
	hashStr := hex.EncodeToString(h.Sum(nil))[:8]

	home, _ := os.UserHomeDir()
	if home == "" {
		// Fallback
		home = "/tmp"
	}

	return &defaultWorkspacePaths{
		workspaceDir: absDir,
		hash:         hashStr,
		globalHome:   filepath.Join(home, ".gogents"),
	}
}

func (p *defaultWorkspacePaths) Init() error {
	dirs := []string{
		p.Gogents(),
		p.TasksDir(),
		p.LogsDir(),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", d, err)
		}
	}
	return nil
}

func (p *defaultWorkspacePaths) Hash() string    { return p.hash }
func (p *defaultWorkspacePaths) Root() string    { return p.workspaceDir }
func (p *defaultWorkspacePaths) Gogents() string { return filepath.Join(p.workspaceDir, ".gogents") }
func (p *defaultWorkspacePaths) RuntimeYaml() string {
	return filepath.Join(p.Gogents(), "runtime.yaml")
}
func (p *defaultWorkspacePaths) StateYaml() string { return filepath.Join(p.Gogents(), "state.yaml") }
func (p *defaultWorkspacePaths) TasksDir() string  { return filepath.Join(p.Gogents(), "tasks") }

func (p *defaultWorkspacePaths) TaskDir(id string) string { return filepath.Join(p.TasksDir(), id) }
func (p *defaultWorkspacePaths) TaskYaml(id string) string {
	return filepath.Join(p.TaskDir(id), "task.yaml")
}

func (p *defaultWorkspacePaths) InputMd(id string) string {
	return filepath.Join(p.TaskDir(id), "input.md")
}

func (p *defaultWorkspacePaths) OutputJsonl(id string) string {
	return filepath.Join(p.TaskDir(id), "output.jsonl")
}

func (p *defaultWorkspacePaths) ResultJson(id string) string {
	return filepath.Join(p.TaskDir(id), "result.json")
}

func (p *defaultWorkspacePaths) GlobalConfigYaml() string {
	return filepath.Join(p.globalHome, "config.yaml")
}
func (p *defaultWorkspacePaths) LogsDir() string   { return filepath.Join(p.globalHome, "logs", p.hash) }
func (p *defaultWorkspacePaths) DaemonLog() string { return filepath.Join(p.LogsDir(), "daemon.jsonl") }
func (p *defaultWorkspacePaths) ErrorLog() string  { return filepath.Join(p.LogsDir(), "error.jsonl") }
