package execution

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// VerdictCase classifies the outcome derived from result.json (D03 §5.2).
type VerdictCase int

const (
	VerdictUnknown       VerdictCase = iota
	VerdictCodingSuccess             // R07 Case A
	VerdictCodingFailed              // R07 Case B
	VerdictMissing                   // R07 Case C — result.json absent
	VerdictParseFailed               // R07 Case C — file present but unparseable
	VerdictInvalid                   // R07 Case C — fields missing or unrecognised
)

// Verdict is the structured outcome of judging result.json.
type Verdict struct {
	Case    VerdictCase
	PrURL   string
	Message string
	Reason  string
}

// resultJSON mirrors the result.json schema (D03 §3.3).
type resultJSON struct {
	Status  string `json:"status"`
	PrURL   string `json:"pr_url"`
	Message string `json:"message"`
}

// judge inspects result.json and returns the verdict. It never logs or mutates
// task.yaml — that is the caller's responsibility (finalize).
func judge(resultPath string) Verdict {
	b, err := os.ReadFile(resultPath) //nolint:gosec // path comes from trusted workspace paths
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Verdict{Case: VerdictMissing}
		}
		return Verdict{Case: VerdictMissing, Reason: err.Error()}
	}

	var r resultJSON
	if uErr := json.Unmarshal(b, &r); uErr != nil {
		return Verdict{Case: VerdictParseFailed, Reason: uErr.Error()}
	}

	switch r.Status {
	case "success":
		if strings.TrimSpace(r.PrURL) == "" {
			return Verdict{Case: VerdictInvalid, Reason: "status=success but pr_url missing"}
		}
		return Verdict{Case: VerdictCodingSuccess, PrURL: r.PrURL, Message: r.Message}
	case "failed":
		return Verdict{Case: VerdictCodingFailed, Message: r.Message}
	case "":
		return Verdict{Case: VerdictInvalid, Reason: "status field missing"}
	default:
		return Verdict{Case: VerdictInvalid, Reason: "unexpected status: " + r.Status}
	}
}
