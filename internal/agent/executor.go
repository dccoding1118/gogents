package agent

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrAgentSpawn indicates that the agent process could not be spawned (program/env error).
	ErrAgentSpawn = errors.New("agent process spawn failed")
	// ErrAgentIO indicates that the agent I/O descriptors could not be setup (file existence/permissions).
	ErrAgentIO = errors.New("agent io setup failed")
)

type RunRequest struct {
	TaskID     string // strictly for logging
	WorkDir    string // cwd for the child process
	InputPath  string // path to read stdin from
	OutputPath string // path to write stdout+stderr to (O_TRUNC)
	Model      string // config agent model string
}

type RunResult struct {
	ExitCode   int
	Signaled   bool
	Signal     string
	StartedAt  time.Time
	FinishedAt time.Time
	DurationMs int64
}

// AgentExecutor drives the execution of an agent process.
type AgentExecutor interface {
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}
