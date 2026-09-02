package schedule

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// ParseCron parses a 5-field cron expression or one of the @hourly/@daily/
// @weekly/@monthly/@every descriptors.
//
// The parser is left at its default location, which makes robfig evaluate the
// expression in the location of the time handed to Next. Every caller passes a
// time.Local clock reading, so "0 9 * * 5" means 09:00 where the user is,
// including across a daylight-saving shift.
func ParseCron(expr string) (cron.Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("cron is required")
	}
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, fmt.Errorf("cron %q is not valid: %w", expr, err)
	}
	return sched, nil
}

// NextOccurrences returns up to n occurrences strictly after `after`. It stops
// early when the expression has no further occurrence at all (February 30th),
// so the result can be shorter than n.
func NextOccurrences(expr string, after time.Time, n int) ([]time.Time, error) {
	sched, err := ParseCron(expr)
	if err != nil {
		return nil, err
	}
	if n <= 0 {
		return nil, nil
	}
	out := make([]time.Time, 0, n)
	at := after
	for range n {
		next := sched.Next(at)
		if next.IsZero() {
			break
		}
		out = append(out, next)
		at = next
	}
	return out, nil
}
