package execution

import (
	"context"

	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/internal/logger"
)

// TaskExecutor runs a single task. Stage 2 uses the no-op implementation;
// Stage 3 will provide the full implementation in D03.
type TaskExecutor interface {
	Run(ctx context.Context, task foundation.Task) error
}

// noopExecutor is the Stage 2 placeholder. It logs the invocation and returns
// immediately without changing task status, calling agents, or touching git.
type noopExecutor struct {
	lg logger.Logger
}

// NewNoopExecutor creates a TaskExecutor that only logs task.noop_executed.
func NewNoopExecutor(lg logger.Logger) TaskExecutor {
	return &noopExecutor{lg: lg}
}

func (e *noopExecutor) Run(_ context.Context, task foundation.Task) error {
	e.lg.Info("task.noop_executed", task.ID, "no-op executor invoked (Stage 2)", map[string]any{
		"task_id": task.ID,
		"status":  string(task.Status),
	})
	return nil
}
