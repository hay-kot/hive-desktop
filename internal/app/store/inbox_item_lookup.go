package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

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

const getFeedIDForItem = `
SELECT feed_id FROM feed_membership_claim
WHERE profile_id = ? AND item_id = ?
ORDER BY feed_id
LIMIT 1
`

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

// InboxItemNotifiable reports whether an observation is worth interrupting the
// user for, reusing ingestion's own verdict rather than forming a second
// opinion from the raw source emission. Two conditions:
//
//   - An inbox_event exists for this occurrence. Ingestion records one only
//     when the observation carried a state transition or real activity (see
//     applyTransition and the classifiers); a trivial re-observation gets no
//     row, and its occurrence key is a backfilled event-log offset that
//     matches nothing. This is the "genuinely new-to-me, not the poller
//     re-observed it" line.
//   - The item is unread. The same triage step that recorded the event decides
//     this, so it excludes an item the observation archived — a PR closing is
//     activity, but it is leaving the feed, not asking for attention.
//
// A missing item or occurrence key is not notifiable rather than an error:
// like InboxItemID, an identity can legitimately have no inbox row.
func (db *DB) InboxItemNotifiable(ctx context.Context, profileID, sourceKind, sourceScope, externalID, occurrenceKey string) (bool, error) {
	if externalID == "" || occurrenceKey == "" {
		return false, nil
	}
	item, err := db.queries.GetInboxItemByExternalID(ctx, GetInboxItemByExternalIDParams{
		ProfileID: profileID, SourceKind: sourceKind, SourceScope: sourceScope, ExternalID: externalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("resolving inbox item %s/%s/%s: %w", sourceKind, sourceScope, externalID, err)
	}
	if item.Unread == 0 {
		return false, nil
	}
	_, err = db.queries.GetInboxEventByOccurrence(ctx, GetInboxEventByOccurrenceParams{
		ItemID:        item.ID,
		OccurrenceKey: sql.NullString{String: occurrenceKey, Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading inbox event for item %d: %w", item.ID, err)
	}
	return true, nil
}
