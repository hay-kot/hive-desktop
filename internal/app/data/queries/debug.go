package queries

import (
	"context"
	"time"
)

// DebugPauseCommit widens the pre-commit crash window using the development
// duration injected when the database was opened. Exported so
// stores.EventLogStore.Commit -- the write it guards -- can call it from
// outside this package.
func (db *DB) DebugPauseCommit(ctx context.Context) { debugPause(ctx, db.pauseCommit) }

func debugPause(ctx context.Context, duration time.Duration) {
	if duration <= 0 {
		return
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
