package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// resolveInboxItemScoped resolves the durable inbox row behind a source
// identity, healing pre-#63 rows on the way.
//
// #63 gave GitHub observations a SourceScope (the account); rows written
// before it carry an empty source_scope and are invisible to the scoped lookup
// every post-#63 read keys on. On a scoped miss this retries under the empty
// scope, and if that hits it rewrites the row to the requested scope so the
// identity — and the triage decisions the row carries — are consistent for
// every later read, and so a subsequent ingest updates the row in place rather
// than inserting a scoped duplicate beside it. A genuine miss returns
// sql.ErrNoRows unchanged. See issue #95.
func resolveInboxItemScoped(ctx context.Context, q *Queries, profileID, sourceKind, sourceScope, externalID string) (InboxItem, error) {
	item, err := q.GetInboxItemByExternalID(ctx, GetInboxItemByExternalIDParams{
		ProfileID: profileID, SourceKind: sourceKind, SourceScope: sourceScope, ExternalID: externalID,
	})
	if err == nil || !errors.Is(err, sql.ErrNoRows) || sourceScope == "" {
		return item, err
	}

	legacy, legacyErr := q.GetInboxItemByExternalID(ctx, GetInboxItemByExternalIDParams{
		ProfileID: profileID, SourceKind: sourceKind, SourceScope: "", ExternalID: externalID,
	})
	if legacyErr != nil {
		if errors.Is(legacyErr, sql.ErrNoRows) {
			return item, err
		}
		return legacy, legacyErr
	}
	if rescopeErr := q.RescopeInboxItem(ctx, RescopeInboxItemParams{SourceScope: sourceScope, ID: legacy.ID}); rescopeErr != nil {
		return legacy, rescopeErr
	}
	// item_session is keyed on these same coordinates, so the links have to
	// move with the row or they address a scope nothing reads under again.
	if rescopeErr := q.RescopeItemSessions(ctx, RescopeItemSessionsParams{
		SourceScope: sourceScope, ProfileID: profileID, SourceKind: sourceKind, ExternalID: externalID,
	}); rescopeErr != nil {
		return legacy, rescopeErr
	}
	legacy.SourceScope = sourceScope
	return legacy, nil
}

// InboxItemID resolves the durable inbox row behind a source identity. It is
// the read-only half of the identity CommitBatch resolves for feed
// membership, exposed for callers that hold only the source-side identity —
// a delivered notification linking back to the item that triggered it. A
// missing row is (0, nil), not an error: an identity can legitimately have no
// inbox row (a message a function node synthesized), and that only means
// "nothing to link to".
func (db *DB) InboxItemID(ctx context.Context, profileID, sourceKind, sourceScope, externalID string) (int64, error) {
	if externalID == "" {
		return 0, nil
	}
	item, err := db.queries.GetInboxItemByExternalID(ctx, GetInboxItemByExternalIDParams{
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

// FindInboxItemsByExternalID returns every item sharing an external id,
// optionally scoped to one profile (empty profileID matches all). The same id
// can exist across profiles and source scopes, so this returns a slice.
func (db *DB) FindInboxItemsByExternalID(ctx context.Context, profileID, externalID string) ([]InboxItemView, error) {
	rows, err := db.queries.FindInboxItemsByExternalID(ctx, FindInboxItemsByExternalIDParams{
		ExternalID: externalID, ProfileID: profileID,
	})
	if err != nil {
		return nil, fmt.Errorf("finding inbox items for external id %q: %w", externalID, err)
	}
	return inboxItemViews(rows), nil
}

// ListAllInboxItems returns every inbox item newest-first, optionally scoped to
// one profile (empty profileID matches all). It is unfiltered by feed
// membership or triage state — a debug/observation read, not a workspace view.
func (db *DB) ListAllInboxItems(ctx context.Context, profileID string, limit int) ([]InboxItemView, error) {
	if limit <= 0 {
		return []InboxItemView{}, nil
	}
	rows, err := db.queries.ListAllInboxItems(ctx, ListAllInboxItemsParams{ProfileID: profileID, Lim: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing all inbox items: %w", err)
	}
	return inboxItemViews(rows), nil
}

const getFeedIDForItem = `
SELECT feed_id FROM feed_membership_claim
WHERE profile_id = ? AND item_id = ?
ORDER BY feed_id
LIMIT 1
`

// InboxItemFeedIDs resolves the claiming feed of each item in one query, using
// the same lowest-feed-id rule as InboxItemFeedID. Keyed by item id (a global
// primary key, so the profile is implied); an item with no claim is absent from
// the map, meaning its feed is "". Built for callers that list items flat and
// need each item's feed without an N+1 of InboxItemFeedID.
func (db *DB) InboxItemFeedIDs(ctx context.Context, itemIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(itemIDs)), ",")
	query := "SELECT item_id, MIN(feed_id) FROM feed_membership_claim WHERE item_id IN (" + placeholders + ") GROUP BY item_id"
	args := make([]any, len(itemIDs))
	for i, id := range itemIDs {
		args[i] = id
	}
	rows, err := db.querier().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("resolving feeds for %d inbox items: %w", len(itemIDs), err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			id   int64
			feed string
		)
		if err := rows.Scan(&id, &feed); err != nil {
			return nil, fmt.Errorf("scanning inbox item feed: %w", err)
		}
		out[id] = feed
	}
	return out, rows.Err()
}

// InboxItemFeedID returns the feed that claims an item, or "" when nothing
// does (an unrouted item, which the UI shows in Trash). An item claimed by
// several feeds resolves to the lowest feed id so the answer is stable across
// calls — any of them reveals the item, and the sidebar's own ordering is a
// frontend concern this query deliberately does not reach into.
func (db *DB) InboxItemFeedID(ctx context.Context, profileID string, itemID int64) (string, error) {
	var feedID string
	err := db.querier().QueryRowContext(ctx, getFeedIDForItem, profileID, itemID).Scan(&feedID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolving feed for inbox item %d: %w", itemID, err)
	}
	return feedID, nil
}
