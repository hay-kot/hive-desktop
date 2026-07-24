package pipelinedb

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
	err := db.conn.QueryRowContext(ctx, getFeedIDForItem, profileID, itemID).Scan(&feedID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolving feed for inbox item %d: %w", itemID, err)
	}
	return feedID, nil
}
