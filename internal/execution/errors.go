package execution

import "errors"

// Internal sentinel errors used by judge / finalize. They never escape Run
// (D03 §2 B1: task-level failures land in task.yaml, not the Run return value).
var (
	ErrResultMissing      = errors.New("result.json missing")
	ErrResultParse        = errors.New("result.json parse failed")
	ErrResultFieldMissing = errors.New("result.json required field missing")
	ErrResultBadStatus    = errors.New("result.json status not recognized")
	ErrGitPrepare         = errors.New("git prepare failed")
	ErrInputWrite         = errors.New("input.md write failed")
	ErrContentFetch       = errors.New("task content fetch failed")
)
