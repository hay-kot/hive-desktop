package stores

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// Seed writes rows directly through the generated queries, for tests outside
// internal/app/data that need a fixture no store method can produce -- an
// exact timestamp a store's own clock would overwrite, or a row with no
// aggregate-level meaning on its own. It takes the store's own types, so a
// test never imports the queries package for a fixture. It exists for tests
// only; production code reaches a store, never Seed. Every method goes
// through the same s.q.Ctx(ctx) path a store does, so a fixture written
// inside Stores.WithinTx stays inside that transaction.
type Seed struct {
	q *queries.DB
}

func NewSeed(q *queries.DB) Seed {
	return Seed{q: q}
}

// InboxItem inserts one inbox_item row from item's fields. The columns the
// insert does not take -- id, revision, and the triage state -- are ignored.
func (s Seed) InboxItem(ctx context.Context, item InboxItem) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).InsertInboxItem(ctx, queries.InsertInboxItemParams{
		ProfileID: item.ProfileID, SourceKind: item.SourceKind, SourceScope: item.SourceScope, ExternalID: item.ExternalID,
		Title: item.Title, Url: item.URL, Payload: item.Payload, Unread: boolToInt64(item.Unread), Lifecycle: item.Lifecycle,
		FirstSeenAt: item.FirstSeenAt, LastEventAt: item.LastEventAt,
	})
	if err != nil {
		return InboxItem{}, wrap("seeding inbox item", err)
	}
	return mapInboxItemFromDB(row), nil
}

// InboxEvent inserts one inbox_event row from event's fields; the id is
// ignored.
func (s Seed) InboxEvent(ctx context.Context, event InboxEvent) (InboxEvent, error) {
	row, err := s.q.Ctx(ctx).InsertInboxEvent(ctx, queries.InsertInboxEventParams{
		ItemID: event.ItemID, Kind: event.Kind, Transition: event.Transition, Attention: event.Attention,
		Summary: null(event.Summary), Detail: event.Detail, CreatedAt: event.CreatedAt,
	})
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
