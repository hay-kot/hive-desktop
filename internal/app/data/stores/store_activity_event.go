package stores

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// ActivityEventStore owns activity_event: the audit log the Activity view
// reads and backend subsystems append to. It is the persistence half of
// activity.Store; phase 3c rewires that package onto this store and deletes
// its own database access.
type ActivityEventStore struct {
	q      *queries.DB
	now    func() time.Time
	mapper MapFunc[queries.ActivityEvent, ActivityEvent]
}

func NewActivityEventStore(q *queries.DB, opts Options) *ActivityEventStore {
	return &ActivityEventStore{q: q, now: opts.Now, mapper: mapActivityEventFromDB}
}

// Append persists one activity event and returns the stored row with its
// assigned id and durable created_at.
func (s *ActivityEventStore) Append(ctx context.Context, in ActivityEventCreate) (ActivityEvent, error) {
	// *DB.AppendActivityEvent (activity_event.go) shadows the generated
	// method of the same name until phase 3c deletes it; .Queries reaches
	// the promoted one, typed on the params struct this store wants.
	row, err := s.q.Ctx(ctx).Queries.AppendActivityEvent(ctx, queries.AppendActivityEventParams{
		CreatedAt: s.now().UnixMilli(),
		Category:  in.Category,
		Severity:  in.Severity,
		Title:     in.Title,
		Body:      in.Body,
		Source:    in.Source,
		Metadata:  in.Metadata,
	})
	return s.mapper.Err(row, wrap(fmt.Sprintf("appending activity event %q", in.Title), err))
}

// List returns up to limit events with id < before, newest first. Pass
// before <= 0 to start from the most recent event.
func (s *ActivityEventStore) List(ctx context.Context, before int64, limit int) ([]ActivityEvent, error) {
	if before <= 0 {
		before = math.MaxInt64
	}
	rows, err := s.q.Ctx(ctx).Queries.ListActivityEvents(ctx, queries.ListActivityEventsParams{
		ID:    before,
		Limit: int64(limit),
	})
	return s.mapper.SliceErr(rows, wrap("listing activity events", err))
}
