package stores

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// Seed writes rows directly through the generated queries, for tests outside
// internal/app/data that need a fixture no store method can produce -- an
// exact timestamp a store's own clock would overwrite, or a row with no
// aggregate-level meaning on its own. It exists for tests only; production
// code reaches a store, never Seed. Every method goes through the same
// s.q.Ctx(ctx) path a store does, so a fixture written inside
// Stores.Tx stays inside that transaction.
type Seed struct {
	q *queries.DB
}

// NewSeed builds a Seed over q.
func NewSeed(q *queries.DB) Seed {
	return Seed{q: q}
}

// InboxItem inserts one inbox_item row with the exact fields p specifies.
func (s Seed) InboxItem(ctx context.Context, p queries.InsertInboxItemParams) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).InsertInboxItem(ctx, p)
	if err != nil {
		return InboxItem{}, wrap("seeding inbox item", err)
	}
	return mapInboxItemFromDB(row), nil
}

// InboxEvent inserts one inbox_event row with the exact fields p specifies.
func (s Seed) InboxEvent(ctx context.Context, p queries.InsertInboxEventParams) (InboxEvent, error) {
	row, err := s.q.Ctx(ctx).InsertInboxEvent(ctx, p)
	if err != nil {
		return InboxEvent{}, wrap("seeding inbox event", err)
	}
	return mapInboxEventFromDB(row), nil
}

// ConsumerOffset sets one consumer's read checkpoint directly. EventLogStore
// only advances an offset as part of Commit, which does far more than a
// fixture setting up a starting point needs.
func (s Seed) ConsumerOffset(ctx context.Context, consumer string, offset int64) error {
	return wrap("seeding consumer offset", s.q.Ctx(ctx).CommitConsumerOffset(ctx, queries.CommitConsumerOffsetParams{
		Consumer: consumer, Offset: offset,
	}))
}

// Job inserts one job row with the exact fields p specifies -- JobStore.Insert
// always stamps CreatedAt and UpdatedAt from its own clock, which cannot
// produce a fixture that needs them to differ.
func (s Seed) Job(ctx context.Context, p queries.InsertJobParams) (Job, error) {
	row, err := s.q.Ctx(ctx).InsertJob(ctx, p)
	if err != nil {
		return Job{}, wrap("seeding job", err)
	}
	return mapJobFromDB(row), nil
}
