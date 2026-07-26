package store

import (
	"context"
	"time"
)

// DebugPauseIngest widens the post-hydration/pre-ingest crash window using
// the development duration injected when the database was opened.
func (db *DB) DebugPauseIngest(ctx context.Context) { debugPause(ctx, db.pauseIngest) }

func (db *DB) debugPauseCommit(ctx context.Context) { debugPause(ctx, db.pauseCommit) }

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
