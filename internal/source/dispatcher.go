package source

import (
	"context"
	"fmt"

	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/logger"
)

// NewTaskSourceFromConfig selects a concrete TaskSource based on cfg.Source.Type.
// Unsupported type returns an error; caller (Daemon.Run startup) treats it as config error (exit 3).
func NewTaskSourceFromConfig(cfg foundation.Config, lg logger.Logger) (TaskSource, error) {
	switch cfg.Source.Type {
	case "github_issue":
		return newGitHubIssueSource(cfg, lg), nil
	case "local":
		return newLocalTicketSource(cfg, lg), nil
	default:
		return nil, fmt.Errorf("unsupported source.type: %q", cfg.Source.Type)
	}
}

// NewSourceProbeFromConfig selects a concrete SourceProbe based on cfg.Source.Type.
// Called by D02 at startup to assemble ToolchainProbe for D01.Preflight.
func NewSourceProbeFromConfig(cfg foundation.Config, lg logger.Logger) (SourceProbe, error) {
	switch cfg.Source.Type {
	case "github_issue":
		return newGitHubIssueProbe(cfg, lg), nil
	case "local":
		return newLocalTicketProbe(cfg, lg), nil
	default:
		return nil, fmt.Errorf("unsupported source.type: %q", cfg.Source.Type)
	}
}

// NewFeedbackReporterFromConfig selects a concrete FeedbackReporter based on cfg.Source.Type.
// Called by D03 Stage 3 execution engine.
func NewFeedbackReporterFromConfig(cfg foundation.Config, lg logger.Logger) (FeedbackReporter, error) {
	switch cfg.Source.Type {
	case "github_issue":
		return newGitHubFeedbackReporter(cfg, lg), nil
	case "local":
		return newLocalTicketReporter(cfg, lg), nil
	default:
		return nil, fmt.Errorf("unsupported source.type: %q", cfg.Source.Type)
	}
}

// NewTaskContentFetcherFromConfig selects a concrete TaskContentFetcher based on cfg.Source.Type.
// Called by CLI startup before composing execution.Deps.
func NewTaskContentFetcherFromConfig(cfg foundation.Config, lg logger.Logger) (TaskContentFetcher, error) {
	switch cfg.Source.Type {
	case "github_issue":
		return newGitHubIssueContentFetcher(cfg, lg), nil
	case "local":
		return newLocalTicketContentFetcher(cfg, lg), nil
	default:
		return nil, fmt.Errorf("unsupported source.type: %q", cfg.Source.Type)
	}
}

// NewTaskVerifierFromConfig selects a concrete TaskVerifier based on cfg.Source.Type.
// github_issue has no ticket-declared verify, so it returns a verifier that
// always reports ok=false (D03 falls back to the result.json verdict).
func NewTaskVerifierFromConfig(cfg foundation.Config, lg logger.Logger) (TaskVerifier, error) {
	switch cfg.Source.Type {
	case "github_issue":
		return nopVerifier{}, nil
	case "local":
		return newLocalTicketVerifier(cfg, lg), nil
	default:
		return nil, fmt.Errorf("unsupported source.type: %q", cfg.Source.Type)
	}
}

// nopVerifier reports no ticket-declared verification; callers fall back to the
// result.json coding verdict.
type nopVerifier struct{}

func (nopVerifier) VerifySpec(_ context.Context, _ string) (VerifySpec, bool, error) {
	return VerifySpec{}, false, nil
}
