package foundation

import (
	"fmt"
	"regexp"
	"strconv"
	"sync/atomic"
	"time"
)

// defaultTimezone is the timezone used until SetGlobalTimezone is called
// (e.g. before runtime.yaml is loaded, or when the field is omitted).
const defaultTimezone = "Asia/Taipei"

var globalTZ atomic.Pointer[time.Location]

func init() {
	loc, err := time.LoadLocation(defaultTimezone)
	if err != nil {
		loc = time.FixedZone("UTC+08:00", 8*3600)
	}
	globalTZ.Store(loc)
}

// SetGlobalTimezone replaces the process-wide timezone used by Now() and
// Timezone(). nil is a no-op so callers can chain LoadTimezone safely.
func SetGlobalTimezone(loc *time.Location) {
	if loc == nil {
		return
	}
	globalTZ.Store(loc)
}

// Timezone returns the active process-wide *time.Location.
func Timezone() *time.Location {
	return globalTZ.Load()
}

// Now returns the current wall clock time projected into the configured timezone.
// All persisted timestamps (state.yaml, task.yaml, log lines, comment bodies)
// should derive from this helper instead of time.Now() so the operator's
// chosen timezone wins consistently.
func Now() time.Time {
	return time.Now().In(globalTZ.Load())
}

// offsetRe matches "+08:00", "-05:30", "UTC+08:00", "+08", etc.
var offsetRe = regexp.MustCompile(`^(?:UTC)?([+-])(\d{1,2})(?::?(\d{2}))?$`)

// LoadTimezone parses a timezone string. Accepted forms:
//   - IANA names: "Asia/Taipei", "UTC", "America/New_York"
//   - Fixed offsets: "+08:00", "-05:00", "UTC+08:00", "+08"
func LoadTimezone(s string) (*time.Location, error) {
	if s == "" {
		return nil, fmt.Errorf("empty timezone")
	}
	if loc, err := time.LoadLocation(s); err == nil {
		return loc, nil
	}
	m := offsetRe.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("invalid timezone %q", s)
	}
	sign := 1
	if m[1] == "-" {
		sign = -1
	}
	h, err := strconv.Atoi(m[2])
	if err != nil || h > 23 {
		return nil, fmt.Errorf("invalid hours in timezone %q", s)
	}
	var mn int
	if m[3] != "" {
		if mn, err = strconv.Atoi(m[3]); err != nil || mn > 59 {
			return nil, fmt.Errorf("invalid minutes in timezone %q", s)
		}
	}
	secs := sign * (h*3600 + mn*60)
	name := fmt.Sprintf("UTC%s%02d:%02d", m[1], h, mn)
	return time.FixedZone(name, secs), nil
}
