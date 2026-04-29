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

type geminiProbe struct {
	cfg foundation.Config
	lg  logger.Logger
}

// NewGeminiProbe returns a foundation.AgentProbe implementation for gemini.
func NewGeminiProbe(cfg foundation.Config, lg logger.Logger) foundation.AgentProbe {
	// Not enforcing cfg.Agent.Type validation here per design limits 4.4 constraints.
	return &geminiProbe{cfg: cfg, lg: lg}
}

func (p *geminiProbe) CheckAgent(spec foundation.AgentSpec) error {
	// 1. check executable look path
	if _, err := exec.LookPath("gemini"); err != nil {
		return &foundation.AgentProbeError{
			Stage:  "lookup",
			Reason: fmt.Sprintf("check gemini CLI installation: %v", err),
		}
	}

	// 2. check gemini version with 10s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gemini", "--version")
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
			Reason: fmt.Sprintf("gemini --version execution failed: %s", reason),
		}
	}

	return nil
}

// NewGeminiExecutor returns an AgentExecutor implementation for gemini.
func NewGeminiExecutor(cfg foundation.Config, lg logger.Logger) AgentExecutor {
	return &geminiExecutor{cfg: cfg, lg: lg}
}

type geminiExecutor struct {
	cfg foundation.Config
	lg  logger.Logger
}

func (e *geminiExecutor) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	t0 := time.Now()

	// 1. Open I/O
	//nolint:gosec // workspace path determined by D03
	stdinFile, err := os.Open(req.InputPath)
	if err != nil {
		e.lg.Error("agent.spawn_failed", req.TaskID, "failed to open input file", map[string]any{
			"reason": "input_missing",
			"error":  err.Error(),
		})
		return RunResult{}, ErrAgentIO
	}
	defer func() { _ = stdinFile.Close() }()

	//nolint:gosec // workspace path determined by D03
	outFile, err := os.OpenFile(req.OutputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		e.lg.Error("agent.spawn_failed", req.TaskID, "failed to open output file", map[string]any{
			"reason": "output_open",
			"error":  err.Error(),
		})
		return RunResult{}, ErrAgentIO
	}
	defer func() { _ = outFile.Close() }()

	// 2. Compose command
	model := req.Model
	if model == "" {
		model = e.cfg.Agent.Model
	}

	//nolint:gosec // Safe execution of gemini inside workspace
	cmd := exec.CommandContext(ctx, "gemini", "-m", model, "--yolo", "-o", "stream-json")
	cmd.Dir = req.WorkDir
	cmd.Stdin = stdinFile
	cmd.Stdout = outFile
	cmd.Stderr = outFile // B3: Merge stderr into stdout

	// 3. Start -> Emit event
	if err := cmd.Start(); err != nil {
		e.lg.Error("agent.spawn_failed", req.TaskID, "failed to start agent process", map[string]any{
			"agent_type": "gemini",
			"reason":     err.Error(),
		})
		return RunResult{}, ErrAgentSpawn
	}

	e.lg.Info("task.agent_invoked", req.TaskID, "agent invoked", map[string]any{
		"agent_type": "gemini",
		"model":      model,
		"pid":        cmd.Process.Pid,
		"work_dir":   req.WorkDir,
	})

	// 4. Wait
	_ = cmd.Wait()

	// 5. Result
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
		"exit_code":   result.ExitCode,
		"duration_ms": result.DurationMs,
		"signaled":    result.Signaled,
		"signal":      result.Signal,
	})

	// 6. Return ctx.Err() if cancelled, else nil
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, nil
}
