package stores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// InboxItemStore owns inbox_item and inbox_event: the durable substrate
// behind every feed item, and the lifecycle events recorded against it.
// IngestObservation -- the write that also touches source_head and
// event_log -- lands here in phase 3b; this store is the leaf half.
type InboxItemStore struct {
	q      *queries.DB
	now    func() time.Time
	mapper MapFunc[queries.InboxItem, InboxItem]
}

func NewInboxItemStore(q *queries.DB, opts Options) *InboxItemStore {
	return &InboxItemStore{q: q, now: opts.Now, mapper: mapInboxItemFromDB}
}

// ListByFeed returns a feed's active items, newest first.
func (s *InboxItemStore) ListByFeed(ctx context.Context, profileID, feedID string, limit int) ([]InboxItem, error) {
	if limit <= 0 {
		return []InboxItem{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListInboxItemsByFeed(ctx, queries.ListInboxItemsByFeedParams{ProfileID: profileID, FeedID: feedID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing inbox items for feed %q: %w", feedID, err)
	}
	return s.mapper.Slice(rows), nil
}

// ListArchivedByFeed returns the feed's archived section, newest archive
// first. It is queried lazily when the archived divider expands.
func (s *InboxItemStore) ListArchivedByFeed(ctx context.Context, profileID, feedID string, limit int) ([]InboxItem, error) {
	if limit <= 0 {
		return []InboxItem{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListArchivedInboxItemsByFeed(ctx, queries.ListArchivedInboxItemsByFeedParams{ProfileID: profileID, FeedID: feedID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing archived inbox items for feed %q: %w", feedID, err)
	}
	return s.mapper.Slice(rows), nil
}

// ListTrash returns unrouted (zero feed claims) and user-ignored items.
// Trash is a utility/debug surface, not a work queue: it carries no unread
// semantics.
func (s *InboxItemStore) ListTrash(ctx context.Context, profileID string, limit int) ([]InboxItem, error) {
	if limit <= 0 {
		return []InboxItem{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListInboxItemsTrash(ctx, queries.ListInboxItemsTrashParams{ProfileID: profileID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing trash inbox items for %q: %w", profileID, err)
	}
	return s.mapper.Slice(rows), nil
}

// ListAll returns every inbox item newest-first, optionally scoped to one
// profile (empty profileID matches all). It is unfiltered by feed membership
// or triage state -- a debug/observation read, not a workspace view.
func (s *InboxItemStore) ListAll(ctx context.Context, profileID string, limit int) ([]InboxItem, error) {
	if limit <= 0 {
		return []InboxItem{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListAllInboxItems(ctx, queries.ListAllInboxItemsParams{ProfileID: profileID, Lim: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing all inbox items: %w", err)
	}
	return s.mapper.Slice(rows), nil
}

// ListUnarchived returns exactly the Wails-safe items eligible for synthetic
// replay. Archived memberships are deliberately frozen and never returned.
func (s *InboxItemStore) ListUnarchived(ctx context.Context, profileID string) ([]InboxItem, error) {
	rows, err := s.q.Ctx(ctx).ListUnarchivedInboxItemsByProfile(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("listing unarchived inbox items for %q: %w", profileID, err)
	}
	return s.mapper.Slice(rows), nil
}

// ListUnarchivedBySource returns the unarchived items behind one connector
// instance, for reconciling a webhook delivery's authoritative snapshot.
func (s *InboxItemStore) ListUnarchivedBySource(ctx context.Context, profileID, sourceKind, sourceScope string) ([]InboxItem, error) {
	rows, err := s.q.Ctx(ctx).ListUnarchivedInboxItemsBySource(ctx, queries.ListUnarchivedInboxItemsBySourceParams{
		ProfileID: profileID, SourceKind: sourceKind, SourceScope: sourceScope,
	})
	if err != nil {
		return nil, fmt.Errorf("listing unarchived inbox items for source %s/%s: %w", sourceKind, sourceScope, err)
	}
	return s.mapper.Slice(rows), nil
}

// FindByExternalID returns every item sharing an external id, optionally
// scoped to one profile (empty profileID matches all). The same id can exist
// across profiles and source scopes, so this returns a slice.
func (s *InboxItemStore) FindByExternalID(ctx context.Context, profileID, externalID string) ([]InboxItem, error) {
	rows, err := s.q.Ctx(ctx).FindInboxItemsByExternalID(ctx, queries.FindInboxItemsByExternalIDParams{
		ExternalID: externalID, ProfileID: profileID,
	})
	if err != nil {
		return nil, fmt.Errorf("finding inbox items for external id %q: %w", externalID, err)
	}
	return s.mapper.Slice(rows), nil
}

// GetByID reads one inbox item by its row id.
func (s *InboxItemStore) GetByID(ctx context.Context, id int64) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).GetInboxItemByID(ctx, id)
	return s.mapper.Err(row, errTransformQueryOne("inbox_item", fmt.Sprint(id), err))
}

// RefByID resolves an inbox row to the ref an association is keyed on.
func (s *InboxItemStore) RefByID(ctx context.Context, itemID int64) (models.ItemRef, error) {
	row, err := s.q.Ctx(ctx).GetInboxItemByID(ctx, itemID)
	if err != nil {
		return models.ItemRef{}, errTransformQueryOne("inbox_item", fmt.Sprint(itemID), err)
	}
	return models.ItemRef{
		ProfileID:   row.ProfileID,
		SourceKind:  row.SourceKind,
		SourceScope: row.SourceScope,
		ExternalID:  row.ExternalID,
	}, nil
}

// ResolveScoped resolves the durable inbox row behind a source identity,
// healing pre-#63 rows on the way.
//
// #63 gave GitHub observations a SourceScope (the account); rows written
// before it carry an empty source_scope and are invisible to the scoped
// lookup every post-#63 read keys on. On a scoped miss this retries under
// the empty scope, and if that hits it rewrites the row to the requested
// scope so the identity -- and the triage decisions the row carries -- are
// consistent for every later read, and so a subsequent ingest updates the
// row in place rather than inserting a scoped duplicate beside it. A genuine
// miss returns sql.ErrNoRows unchanged. See issue #95.
func (s *InboxItemStore) ResolveScoped(ctx context.Context, profileID, sourceKind, sourceScope, externalID string) (InboxItem, error) {
	q := s.q.Ctx(ctx)
	item, err := q.GetInboxItemByExternalID(ctx, queries.GetInboxItemByExternalIDParams{
		ProfileID: profileID, SourceKind: sourceKind, SourceScope: sourceScope, ExternalID: externalID,
	})
	if err == nil {
		return s.mapper(item), nil
	}
	if !errors.Is(err, sql.ErrNoRows) || sourceScope == "" {
		return InboxItem{}, err
	}

	legacy, legacyErr := q.GetInboxItemByExternalID(ctx, queries.GetInboxItemByExternalIDParams{
		ProfileID: profileID, SourceKind: sourceKind, SourceScope: "", ExternalID: externalID,
	})
	if legacyErr != nil {
		if errors.Is(legacyErr, sql.ErrNoRows) {
			return InboxItem{}, err
		}
		return InboxItem{}, legacyErr
	}
	if rescopeErr := q.RescopeInboxItem(ctx, queries.RescopeInboxItemParams{SourceScope: sourceScope, ID: legacy.ID}); rescopeErr != nil {
		return InboxItem{}, rescopeErr
	}
	// item_session is keyed on these same coordinates, so the links have to
	// move with the row or they address a scope nothing reads under again.
	if rescopeErr := q.RescopeItemSessions(ctx, queries.RescopeItemSessionsParams{
		SourceScope: sourceScope, ProfileID: profileID, SourceKind: sourceKind, ExternalID: externalID,
	}); rescopeErr != nil {
		return InboxItem{}, rescopeErr
	}
	legacy.SourceScope = sourceScope
	return s.mapper(legacy), nil
}

// IDByExternalID resolves the durable inbox row behind a source identity. It
// is the read-only half of the identity a commit resolves for feed
// membership, exposed for callers that hold only the source-side identity --
// a delivered notification linking back to the item that triggered it. A
// missing row is (0, nil), not an error: an identity can legitimately have
// no inbox row (a message a function node synthesized), and that only means
// "nothing to link to".
func (s *InboxItemStore) IDByExternalID(ctx context.Context, profileID, sourceKind, sourceScope, externalID string) (int64, error) {
	if externalID == "" {
		return 0, nil
	}
	item, err := s.q.Ctx(ctx).GetInboxItemByExternalID(ctx, queries.GetInboxItemByExternalIDParams{
		ProfileID: profileID, SourceKind: sourceKind, SourceScope: sourceScope, ExternalID: externalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("resolving inbox item %s/%s/%s: %w", sourceKind, sourceScope, externalID, err)
	}
	return item.ID, nil
}

// FeedIDForItem returns the feed that claims an item, or "" when nothing
// does (an unrouted item, which the UI shows in Trash). An item claimed by
// several feeds resolves to the lowest feed id so the answer is stable
// across calls -- any of them reveals the item.
func (s *InboxItemStore) FeedIDForItem(ctx context.Context, profileID string, itemID int64) (string, error) {
	feedID, err := s.q.Ctx(ctx).GetFeedIDForItem(ctx, queries.GetFeedIDForItemParams{ProfileID: profileID, ItemID: itemID})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolving feed for inbox item %d: %w", itemID, err)
	}
	return feedID, nil
}

// FeedIDsForItems resolves the claiming feed of each item in one query,
// using the same lowest-feed-id rule as FeedIDForItem. Keyed by item id (a
// global primary key, so the profile is implied); an item with no claim is
// absent from the map, meaning its feed is "". Built for callers that list
// items flat and need each item's feed without an N+1 of FeedIDForItem.
func (s *InboxItemStore) FeedIDsForItems(ctx context.Context, itemIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := s.q.Ctx(ctx).ListFeedIDsForItems(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("resolving feeds for %d inbox items: %w", len(itemIDs), err)
	}
	for _, row := range rows {
		out[row.ItemID] = row.FeedID
	}
	return out, nil
}

// Events lists one item's lifecycle events, newest first.
func (s *InboxItemStore) Events(ctx context.Context, itemID int64, limit int) ([]InboxEvent, error) {
	if limit <= 0 {
		return []InboxEvent{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListInboxEventsByItem(ctx, queries.ListInboxEventsByItemParams{ItemID: itemID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing inbox events for %d: %w", itemID, err)
	}
	events := make([]InboxEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, mapInboxEventFromDB(row))
	}
	return events, nil
}

// SetUnread sets one item's unread flag under a revision guard. A stale
// revision -- the row has moved since the caller read it -- returns
// ErrStale, not a NotFoundError: it must bypass errTransformQueryOne, or a
// caller checking IsNotFound(err) would see this exact conflict as a missing
// row instead of "re-read and retry".
func (s *InboxItemStore) SetUnread(ctx context.Context, itemID, revision int64, unread bool) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).SetInboxItemUnread(ctx, queries.SetInboxItemUnreadParams{Unread: boolToInt64(unread), ID: itemID, Revision: revision})
	if errors.Is(err, sql.ErrNoRows) {
		return InboxItem{}, ErrStale
	}
	if err != nil {
		return InboxItem{}, fmt.Errorf("setting inbox item %d unread: %w", itemID, err)
	}
	return s.mapper(row), nil
}

// MarkRead clears unread across a whole scope in one statement: feedID names
// a single feed, an empty feedID means every feed in the workspace. It
// returns how many rows it changed so the caller can report the size of
// what it just did.
//
// Callers get no rows back. A bulk clear touches more rows than a UI holds
// and the revisions all move, so the frontend re-reads the affected list and
// the sidebar counts rather than patching what it has.
func (s *InboxItemStore) MarkRead(ctx context.Context, profileID, feedID string) (int64, error) {
	if profileID == "" {
		return 0, fmt.Errorf("marking inbox items read: profile id is required")
	}
	q := s.q.Ctx(ctx)
	if feedID == "" {
		marked, err := q.MarkProfileInboxItemsRead(ctx, profileID)
		if err != nil {
			return 0, fmt.Errorf("marking every feed read for %q: %w", profileID, err)
		}
		return marked, nil
	}
	marked, err := q.MarkFeedInboxItemsRead(ctx, queries.MarkFeedInboxItemsReadParams{ProfileID: profileID, FeedID: feedID})
	if err != nil {
		return 0, fmt.Errorf("marking feed %q read: %w", feedID, err)
	}
	return marked, nil
}

// ToggleArchived flips one item's archived state under a revision guard,
// stamping the store's own clock when archiving. See SetUnread's comment on
// why a stale revision returns ErrStale rather than going through
// errTransformQueryOne.
func (s *InboxItemStore) ToggleArchived(ctx context.Context, itemID, revision int64) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).ToggleInboxItemArchived(ctx, queries.ToggleInboxItemArchivedParams{
		ArchivedAt: sql.NullInt64{Int64: s.now().UnixMilli(), Valid: true}, ID: itemID, Revision: revision,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return InboxItem{}, ErrStale
	}
	if err != nil {
		return InboxItem{}, fmt.Errorf("toggling archive for inbox item %d: %w", itemID, err)
	}
	return s.mapper(row), nil
}

// ToggleIgnored flips one item's ignored state under a revision guard,
// stamping the store's own clock when ignoring. See SetUnread's comment on
// why a stale revision returns ErrStale rather than going through
// errTransformQueryOne.
func (s *InboxItemStore) ToggleIgnored(ctx context.Context, itemID, revision int64) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).ToggleInboxItemIgnored(ctx, queries.ToggleInboxItemIgnoredParams{
		IgnoredAt: sql.NullInt64{Int64: s.now().UnixMilli(), Valid: true}, ID: itemID, Revision: revision,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return InboxItem{}, ErrStale
	}
	if err != nil {
		return InboxItem{}, fmt.Errorf("toggling ignored state for inbox item %d: %w", itemID, err)
	}
	return s.mapper(row), nil
}

// FeedCounts returns total/unread/archived counts per feed for a profile.
func (s *InboxItemStore) FeedCounts(ctx context.Context, profileID string) ([]FeedCount, error) {
	rows, err := s.q.Ctx(ctx).CountInboxItemsByFeed(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("counting inbox items by feed for %q: %w", profileID, err)
	}
	counts := make([]FeedCount, 0, len(rows))
	for _, row := range rows {
		counts = append(counts, FeedCount(row))
	}
	return counts, nil
}

// DeleteByProfile removes every inbox_item row for profileID. Used by
// FlowsService.PurgeProfile (3b) when a workspace is deleted.
func (s *InboxItemStore) DeleteByProfile(ctx context.Context, profileID string) error {
	return wrap("deleting inbox items by profile", s.q.Ctx(ctx).DeleteInboxItemsByProfile(ctx, profileID))
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
