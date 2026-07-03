package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/logger"
)

const claudeAgentType = "claude"

type claudeProbe struct {
	cfg foundation.Config
	lg  logger.Logger
}

// NewClaudeProbe returns a foundation.AgentProbe implementation for Claude Code.
func NewClaudeProbe(cfg foundation.Config, lg logger.Logger) foundation.AgentProbe {
	return &claudeProbe{cfg: cfg, lg: lg}
}

func (p *claudeProbe) CheckAgent(spec foundation.AgentSpec) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return &foundation.AgentProbeError{
			Stage:  "lookup",
			Reason: fmt.Sprintf("check claude CLI installation: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		reason := err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			reason = "timeout"
		} else if len(output) > 0 {
			outStr := string(output)
			if len(outStr) > 512 {
				outStr = outStr[:512]
			}
			reason = fmt.Sprintf("%v: %s", err, outStr)
		}

		return &foundation.AgentProbeError{
			Stage:  "version",
			Reason: fmt.Sprintf("claude --version execution failed: %s", reason),
		}
	}

	return nil
}

// NewClaudeExecutor returns an AgentExecutor implementation for Claude Code.
func NewClaudeExecutor(cfg foundation.Config, lg logger.Logger) AgentExecutor {
	return &claudeExecutor{cfg: cfg, lg: lg}
}

type claudeExecutor struct {
	cfg foundation.Config
	lg  logger.Logger
}

func (e *claudeExecutor) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	t0 := time.Now()

	//nolint:gosec // workspace path determined by D03
	stdinFile, err := os.Open(req.InputPath)
	if err != nil {
		e.lg.Error("agent.spawn_failed", req.TaskID, "failed to open input file", map[string]any{
			"agent_type": claudeAgentType,
			"reason":     "input_missing",
			"error":      err.Error(),
		})
		return RunResult{}, ErrAgentIO
	}
	defer func() { _ = stdinFile.Close() }()

	//nolint:gosec // workspace path determined by D03
	outFile, err := os.OpenFile(req.OutputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		e.lg.Error("agent.spawn_failed", req.TaskID, "failed to open output file", map[string]any{
			"agent_type": claudeAgentType,
			"reason":     "output_open",
			"error":      err.Error(),
		})
		return RunResult{}, ErrAgentIO
	}
	defer func() { _ = outFile.Close() }()

	model := req.Model
	if model == "" {
		model = e.cfg.Agent.Model
	}

	// Prompt is fed via stdin (B3); -p without an inline prompt makes claude
	// read the prompt from stdin in non-interactive mode.
	// --verbose is required by the claude CLI when --output-format=stream-json
	// is combined with -p.
	args := []string{"-p"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(
		args,
		"--dangerously-skip-permissions",
		"--output-format=stream-json",
		"--verbose",
	)

	//nolint:gosec // Safe execution of claude inside workspace
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = stdinFile
	cmd.Stdout = outFile
	cmd.Stderr = outFile // B3: merge stderr into stdout

	if err := cmd.Start(); err != nil {
		e.lg.Error("agent.spawn_failed", req.TaskID, "failed to start agent process", map[string]any{
			"agent_type": claudeAgentType,
			"reason":     err.Error(),
		})
		return RunResult{}, ErrAgentSpawn
	}

	e.lg.Info("task.agent_invoked", req.TaskID, "agent invoked", map[string]any{
		"agent_type": claudeAgentType,
		"model":      model,
		"pid":        cmd.Process.Pid,
		"work_dir":   req.WorkDir,
	})

	_ = cmd.Wait()

	result := RunResult{
		StartedAt:  t0,
		FinishedAt: time.Now(),
		DurationMs: time.Since(t0).Milliseconds(),
		ExitCode:   cmd.ProcessState.ExitCode(),
	}

	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		result.Signaled = true
		result.Signal = ws.Signal().String()
	}

	level := logger.LevelInfo
	if result.ExitCode != 0 || result.Signaled {
		level = logger.LevelWarning
	}

	e.lg.Log(level, "task.agent_exited", req.TaskID, "agent exited", map[string]any{
		"agent_type":  claudeAgentType,
		"exit_code":   result.ExitCode,
		"duration_ms": result.DurationMs,
		"signaled":    result.Signaled,
		"signal":      result.Signal,
	})

	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, nil
}
