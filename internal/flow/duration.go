package flow

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDuration parses a duration string with the same units as Go's
// time.ParseDuration plus two extensions commonly used in HCL TTL config:
//
//   - "d" for days (e.g. "30d" → 720h)
//   - "w" for weeks (e.g. "2w" → 336h)
//
// Multi-unit composites like "1d12h" are NOT supported — the day or week
// suffix must apply to a single integer count. This matches operator
// intuition ("30 day TTL") and avoids the ambiguity of compound forms.
//
// Returns an error for malformed input. Callers should validate config
// values at parse time so a misconfigured TTL fails the deploy instead
// of silently degrading to "no expiry" (the symptom of catching the
// error and using a zero default).
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	// Try the extended single-unit suffixes first so "30d" does not fall
	// through to time.ParseDuration (which would error with "unknown unit
	// d in duration 30d").
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		if days < 0 {
			return 0, fmt.Errorf("invalid day duration %q: must be non-negative", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	if n, ok := strings.CutSuffix(s, "w"); ok {
		weeks, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("invalid week duration %q: %w", s, err)
		}
		if weeks < 0 {
			return 0, fmt.Errorf("invalid week duration %q: must be non-negative", s)
		}
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}

	return time.ParseDuration(s)
}
