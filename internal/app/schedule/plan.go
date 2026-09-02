package schedule

import "time"

// maxOccurrences bounds one evaluation window. A schedule of "* * * * *" left
// unevaluated for two months yields ~87k occurrences; anything past this is a
// clock that jumped, not a backlog worth walking, so the loop stops and the
// window closes at now.
const maxOccurrences = 100_000

// Cursor is how far one schedule has been evaluated. Cron is stored with it so
// a re-timed schedule is recognized as new rather than back-firing every
// occurrence its old expression would have had.
type Cursor struct {
	Workspace        string
	ID               string
	EvaluatedThrough time.Time
	Cron             string
}

// Decision is one occurrence the evaluation chose to honor.
type Decision struct {
	// ScheduledFor is the latest occurrence in the window. Earlier ones are
	// counted in Missed rather than each getting a run: firing a week of
	// backlog at once is worse than firing once and saying how much was
	// skipped.
	ScheduledFor time.Time
	Reason       Reason
	Missed       int
	// Skip means the run is late and the schedule says not to catch up.
	Skip bool
}

// Evaluation is one spec's outcome: the cursor to persist, and the decision to
// execute when there is one.
type Evaluation struct {
	Cursor   Cursor
	Decision *Decision
}

// Evaluate plans one spec against its stored cursor. It is pure: no clock, no
// I/O, so the whole missed-run policy is testable from a table.
//
// grace is how late an occurrence may be and still count as due rather than a
// catch-up. It absorbs the gap between an occurrence and the pass that notices
// it, so a schedule that fires on time is not reported as a missed one.
func Evaluate(spec Spec, cursor *Cursor, now time.Time, grace time.Duration) Evaluation {
	closed := Cursor{Workspace: spec.Workspace, ID: spec.ID, EvaluatedThrough: now, Cron: spec.Cron}

	// A schedule seen for the first time, or one whose expression changed,
	// starts from now: back-firing every occurrence since the epoch is never
	// what an edit meant.
	if cursor == nil || cursor.Cron != spec.Cron {
		return Evaluation{Cursor: closed}
	}
	if spec.Disabled {
		return Evaluation{Cursor: closed}
	}

	sched, err := ParseCron(spec.Cron)
	if err != nil {
		return Evaluation{Cursor: closed}
	}

	var latest time.Time
	count := 0
	for at := cursor.EvaluatedThrough; count < maxOccurrences; {
		next := sched.Next(at)
		if next.IsZero() || next.After(now) {
			break
		}
		latest, at = next, next
		count++
	}
	if count == 0 {
		return Evaluation{Cursor: closed}
	}

	decision := Decision{ScheduledFor: latest, Reason: ReasonDue, Missed: count - 1}
	if now.Sub(latest) > grace {
		decision.Reason = ReasonCatchUp
		decision.Skip = spec.OnMissed == OnMissedSkip
	}
	return Evaluation{Cursor: closed, Decision: &decision}
}

// NextDue is the soonest occurrence across every enabled spec, and false when
// nothing is scheduled. It is what bounds the loop's sleep.
func NextDue(specs []Spec, now time.Time) (time.Time, bool) {
	var soonest time.Time
	for _, spec := range specs {
		if spec.Disabled {
			continue
		}
		sched, err := ParseCron(spec.Cron)
		if err != nil {
			continue
		}
		next := sched.Next(now)
		if next.IsZero() {
			continue
		}
		if soonest.IsZero() || next.Before(soonest) {
			soonest = next
		}
	}
	return soonest, !soonest.IsZero()
}
