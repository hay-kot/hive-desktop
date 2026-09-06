package activity

import "context"

// Recorder is the write side handed to backend subsystems. It is deliberately
// fire-and-forget: an emit site should never fail or block because the audit
// log couldn't be written, so errors are logged, not returned.
// *app.ActivityService satisfies it.
type Recorder interface {
	Record(ctx context.Context, e Event)
}
