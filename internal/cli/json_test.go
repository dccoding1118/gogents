package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dccoding/gogents/internal/foundation"
)

// D08-11: `runtime list --json` — array of runtimes; empty registry marshals to
// `[]` (not the human text), preserving the stable contract.
func TestJSON_List(t *testing.T) {
	views := []RuntimeView{
		{Name: "coding", Workspace: "/home/u/.am/workers/coding", RegisteredAt: "2026-07-04T12:00:00+08:00"},
	}
	b, err := json.Marshal(views)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("D08-11 expected 1 element, got %d", len(got))
	}
	for _, k := range []string{"name", "workspace", "registered_at"} {
		if _, ok := got[0][k]; !ok {
			t.Errorf("D08-11 missing key %q in %v", k, got[0])
		}
	}

	// Empty registry: List returns a non-nil empty slice → "[]".
	empty := make([]RuntimeView, 0)
	if b, _ := json.Marshal(empty); string(b) != "[]" {
		t.Errorf("D08-11 empty list expected `[]`, got %s", b)
	}
}

// D08-12: `runtime status --json` — flat object; a down runtime with no sync and
// no current task renders alive=false, runtime_state=down, and empty strings for
// last_sync_at / current_task (not the time zero value).
func TestJSON_Status(t *testing.T) {
	view := StatusView{
		RuntimeView:  RuntimeView{Name: "coding", Workspace: "/ws", RegisteredAt: "2026-07-04T12:00:00+08:00"},
		RuntimeState: "down",
		Pid:          12345,
		Alive:        false,
		CurrentTask:  "",
	} // LastSyncAt zero, LastSyncStatus ""

	b, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := map[string]any{
		"name":             "coding",
		"workspace":        "/ws",
		"registered_at":    "2026-07-04T12:00:00+08:00",
		"runtime_state":    "down",
		"alive":            false,
		"current_task":     "",
		"last_sync_at":     "", // zero time → empty string, not 0001-01-01…
		"last_sync_status": "",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("D08-12 field %q = %#v, want %#v", k, got[k], v)
		}
	}
	if got["pid"] != float64(12345) {
		t.Errorf("D08-12 pid = %#v, want 12345", got["pid"])
	}
}

// D08-12: a non-zero LastSyncAt marshals as RFC3339.
func TestJSON_Status_LastSyncRFC3339(t *testing.T) {
	ts := time.Date(2026, 7, 4, 12, 3, 0, 0, time.FixedZone("CST", 8*3600))
	view := StatusView{LastSyncAt: ts, RuntimeState: "idle", LastSyncStatus: "ok"}
	b, _ := json.Marshal(view)
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	if got["last_sync_at"] != "2026-07-04T12:03:00+08:00" {
		t.Errorf("D08-12 last_sync_at = %v, want RFC3339", got["last_sync_at"])
	}
}

// D08-13: `--json` failures classify into stable codes.
func TestJSON_ErrorCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"runtime_not_found", foundation.ErrRuntimeNotRegistered, "runtime_not_found"},
		{"state_not_found", fmt.Errorf("wrap: %w", ErrStateNotFound), "state_not_found"},
		{"internal", errors.New("disk exploded"), "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newRuntimeErrorEnvelope(tc.err)
			if env.Error.Code != tc.code {
				t.Errorf("D08-13 code = %q, want %q", env.Error.Code, tc.code)
			}
			if env.Error.Message == "" {
				t.Errorf("D08-13 message must not be empty")
			}
			b, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Error.Code != tc.code {
				t.Errorf("D08-13 marshaled code = %q, want %q", got.Error.Code, tc.code)
			}
		})
	}
}

// D08 flag parsing: --json is tolerated before or after the positional target.
func TestExtractBoolFlag_PositionTolerant(t *testing.T) {
	cases := [][]string{
		{"coding", "--json"},
		{"--json", "coding"},
	}
	for _, args := range cases {
		rest, present := extractBoolFlag(args, "json")
		if !present {
			t.Errorf("expected --json present in %v", args)
		}
		if len(rest) != 1 || rest[0] != "coding" {
			t.Errorf("expected target [coding] from %v, got %v", args, rest)
		}
	}

	rest, present := extractBoolFlag([]string{"coding"}, "json")
	if present || len(rest) != 1 {
		t.Errorf("expected no flag and target retained, got present=%v rest=%v", present, rest)
	}
}
