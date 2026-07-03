package foundation

// RuntimeState is the daemon's explicit execution phase persisted in state.yaml
// (R04). It is shared here (rather than in the daemon package) so that both the
// daemon and the execution engine can reference it without an import cycle.
//
// "down" is intentionally not part of this enum: it is not a persisted value.
// A dead daemon cannot update its own state, so the observability CLI (R12)
// derives "down" at query time when the recorded PID is no longer alive.
type RuntimeState string

const (
	RuntimeIdle      RuntimeState = "idle"
	RuntimeExecuting RuntimeState = "executing"
	RuntimeVerifying RuntimeState = "verifying"
	RuntimeReporting RuntimeState = "reporting"
	RuntimeFailed    RuntimeState = "failed"
	RuntimeBroken    RuntimeState = "broken"
)
