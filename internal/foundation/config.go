package foundation

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	ErrConfigParse     = errors.New("config parse error")
	ErrConfigSchema    = errors.New("config schema error")
	ErrGitNotInstalled = errors.New("git CLI is not installed")
	ErrNotGitRepo      = errors.New("workspace is not a git repository")
)

// GlobalConfig represents ~/.gogents/config.yaml — the runtime registry
// maintained by `gogents start`. It is keyed by workspace path and consumed by
// the observability CLI (`gogents runtime list/status`, `gogents logs`) and by
// agent-manager to locate each worker.
type GlobalConfig struct {
	Runtimes []RuntimeRegistration `yaml:"runtimes"`
}

// RuntimeRegistration is an entry in the global runtimes registry.
// The workspace path is the primary key for upsert.
type RuntimeRegistration struct {
	Name         string `yaml:"name"`
	Workspace    string `yaml:"workspace"`
	RegisteredAt string `yaml:"registered_at"`
}

// Config represents <workspace>/.gogents/runtime.yaml — the single
// source of truth for one workspace's runtime configuration.
type Config struct {
	RuntimeName string `yaml:"runtime_name"`
	// Timezone applies to every persisted timestamp (state.yaml, task.yaml,
	// log lines, feedback comment bodies). Accepts IANA names ("Asia/Taipei")
	// or fixed offsets ("+08:00", "UTC+08:00"). Defaults to "Asia/Taipei".
	Timezone string `yaml:"timezone,omitempty"`
	Project  struct {
		Repo struct {
			URL           string `yaml:"url"`
			DefaultBranch string `yaml:"default_branch"`
		} `yaml:"repo"`
	} `yaml:"project"`
	Source struct {
		Type     string `yaml:"type"`
		Instance string `yaml:"instance,omitempty"` // github_issue: owner/repo
		Path     string `yaml:"path,omitempty"`     // local: ticket inbox dir (default ~/.am/tickets)
		Filter   struct {
			Label string `yaml:"label"`
		} `yaml:"filter"`
		SyncInterval time.Duration `yaml:"sync_interval"`
	} `yaml:"source"`
	Agent struct {
		Type  string `yaml:"type"`
		Model string `yaml:"model"`
	} `yaml:"agent"`
	TaskPolicy struct {
		MaxFailures int `yaml:"max_failures"`
	} `yaml:"task_policy"`
	Assignee string `yaml:"assignee"`
}

type SourceSpec struct {
	// 定義將交給 D05 使用的必要參數
}

type AgentSpec struct {
	// 定義將交給 D06 使用的必要參數
}

type ToolchainProbe interface {
	CheckSource(src SourceSpec) error // 由 D05 實作
	CheckAgent(agt AgentSpec) error   // 由 D06 實作
	CheckGit() error                  // 由 D01 內建實作
}

type ConfigLoader interface {
	Load(workspaceDir string) (Config, error)
	Preflight(cfg Config, probe ToolchainProbe) error
}

type defaultLoader struct {
	paths WorkspacePaths
}

func NewConfigLoader(paths WorkspacePaths) ConfigLoader {
	return &defaultLoader{
		paths: paths,
	}
}

func (l *defaultLoader) Load(workspaceDir string) (Config, error) {
	var cfg Config

	// Read global config (registry) — currently for information only;
	// daemon startup uses --workspace flag directly, not active_runtime.
	globalData, err := os.ReadFile(l.paths.GlobalConfigYaml())
	if err == nil {
		var gc GlobalConfig
		if uerr := yaml.Unmarshal(globalData, &gc); uerr != nil {
			return cfg, fmt.Errorf("%w: failed to parse global config %s: %v",
				ErrConfigParse, l.paths.GlobalConfigYaml(), uerr)
		}
		// GlobalConfig is read but not merged into Config.
		// Reserved for future admin channel usage (see R02).
	}

	// Read runtime config — the single source of truth for this workspace.
	runtimePath := l.paths.RuntimeYaml()
	//nolint:gosec // safe: path is from internal workspace paths generator
	runtimeData, err := os.ReadFile(runtimePath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, fmt.Errorf("%w: runtime config not found: %s", ErrConfigParse, runtimePath)
		}
		return cfg, fmt.Errorf("%w: failed to read runtime config: %v", ErrConfigParse, err)
	}
	if uerr := yaml.Unmarshal(runtimeData, &cfg); uerr != nil {
		return cfg, fmt.Errorf("%w: failed to parse runtime config %s: %v",
			ErrConfigParse, runtimePath, uerr)
	}

	// Schema validation — required fields check.
	if err := validateConfigSchema(&cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// validateConfigSchema checks that all required Config fields are present.
func validateConfigSchema(cfg *Config) error {
	var missing []string
	if cfg.RuntimeName == "" {
		missing = append(missing, "runtime_name")
	}
	if cfg.Project.Repo.URL == "" {
		missing = append(missing, "project.repo.url")
	}
	if cfg.Source.Type == "" {
		missing = append(missing, "source.type")
	}
	if cfg.Source.Filter.Label == "" {
		missing = append(missing, "source.filter.label")
	}
	if cfg.Agent.Type == "" {
		missing = append(missing, "agent.type")
	}
	if cfg.Agent.Model == "" {
		missing = append(missing, "agent.model")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: missing required fields: %v", ErrConfigSchema, missing)
	}

	// source.type enum + per-type required fields.
	switch cfg.Source.Type {
	case "github_issue":
		if cfg.Source.Instance == "" {
			return fmt.Errorf("%w: source.instance is required for source.type=github_issue", ErrConfigSchema)
		}
	case "local":
		// source.path is optional (defaults to ~/.am/tickets); filter.label is
		// already required above.
	default:
		return fmt.Errorf("%w: unsupported source.type: %q", ErrConfigSchema, cfg.Source.Type)
	}

	// agent.type enum.
	switch cfg.Agent.Type {
	case "gemini", "claude":
	default:
		return fmt.Errorf("%w: unsupported agent.type: %q", ErrConfigSchema, cfg.Agent.Type)
	}

	// Apply defaults.
	if cfg.TaskPolicy.MaxFailures <= 0 {
		cfg.TaskPolicy.MaxFailures = 3
	}
	if cfg.Timezone == "" {
		cfg.Timezone = defaultTimezone
	}
	if _, err := LoadTimezone(cfg.Timezone); err != nil {
		return fmt.Errorf("%w: invalid timezone %q: %v", ErrConfigSchema, cfg.Timezone, err)
	}

	return nil
}

func (l *defaultLoader) Preflight(cfg Config, probe ToolchainProbe) error {
	// 1. schema validate 已經在 Load 中初步檢查
	// 2. source
	if err := probe.CheckSource(SourceSpec{}); err != nil {
		return err
	}
	// 3. agent
	if err := probe.CheckAgent(AgentSpec{}); err != nil {
		return err
	}
	// 4. git
	if err := probe.CheckGit(); err != nil {
		return err
	}
	return nil
}

type builtinGitProbe struct {
	workspaceDir string
}

func NewGitProbe(workspaceDir string) ToolchainProbe {
	// 僅做為 git probe 實作包裝，其他 Check 回傳 nil (被覆寫的情境)
	// 在實際系統中該 ToolchainProbe 將會由 D02 組合多個 Probe 而成
	return &builtinGitProbe{workspaceDir: workspaceDir}
}

func (p *builtinGitProbe) CheckSource(src SourceSpec) error {
	return nil
}

func (p *builtinGitProbe) CheckAgent(agt AgentSpec) error {
	return nil
}

func (p *builtinGitProbe) CheckGit() error {
	// 檢查 git 是否安裝
	if err := exec.Command("git", "--version").Run(); err != nil {
		return fmt.Errorf("%w: %v", ErrGitNotInstalled, err)
	}

	return nil
}

// AgentProbe checks the availability of an agent.
type AgentProbe interface {
	CheckAgent(spec AgentSpec) error
}

// AgentProbeError contains structured errors during agent startup.
type AgentProbeError struct {
	Stage  string
	Reason string
}

func (e *AgentProbeError) Error() string {
	return fmt.Sprintf("agent probe failed at stage %s: %s", e.Stage, e.Reason)
}
