package agent

import (
	"errors"
	"fmt"

	"github.com/dccoding1118/gogents/internal/foundation"
	"github.com/dccoding1118/gogents/internal/logger"
)

// ErrAgentUnsupportedType is returned when the agent type is unknown.
var ErrAgentUnsupportedType = errors.New("unsupported agent.type")

// NewAgentProbeFromConfig selects a concrete AgentProbe based on cfg.Agent.Type.
func NewAgentProbeFromConfig(cfg foundation.Config, lg logger.Logger) (foundation.AgentProbe, error) {
	switch cfg.Agent.Type {
	case "claude":
		return NewClaudeProbe(cfg, lg), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrAgentUnsupportedType, cfg.Agent.Type)
	}
}

// NewAgentExecutorFromConfig selects a concrete AgentExecutor based on cfg.Agent.Type.
func NewAgentExecutorFromConfig(cfg foundation.Config, lg logger.Logger) (AgentExecutor, error) {
	switch cfg.Agent.Type {
	case "claude":
		return NewClaudeExecutor(cfg, lg), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrAgentUnsupportedType, cfg.Agent.Type)
	}
}
