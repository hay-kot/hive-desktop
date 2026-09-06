package queries

import (
	"context"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
)

// ItemRefByID resolves an inbox row to the ref an association is keyed on.
func (db *DB) ItemRefByID(ctx context.Context, itemID int64) (models.ItemRef, error) {
	row, err := db.Ctx(ctx).GetInboxItemByID(ctx, itemID)
	if err != nil {
		return models.ItemRef{}, wrap("reading inbox item for its ref", err)
	}
	return models.ItemRef{
		ProfileID:   row.ProfileID,
		SourceKind:  row.SourceKind,
		SourceScope: row.SourceScope,
		ExternalID:  row.ExternalID,
	}, nil
}

// LinkItemSession records that a hive session was created on ref's behalf.
// Nothing about the session is copied: hive owns its name, state and checkout,
// and a mirror here could only go stale.
func (db *DB) LinkItemSession(ctx context.Context, sessionID string, ref models.ItemRef) error {
	if sessionID == "" || !ref.Known() {
		return nil
	}
	err := db.Ctx(ctx).Queries.LinkItemSession(ctx, LinkItemSessionParams{
		SessionID:   sessionID,
		ProfileID:   ref.ProfileID,
		SourceKind:  ref.SourceKind,
		SourceScope: ref.SourceScope,
		ExternalID:  ref.ExternalID,
		CreatedAt:   time.Now().UnixMilli(),
	})
	return wrap("linking session to inbox item", err)
}

// ItemSessions returns the links recorded for ref, newest first. A link is a
// claim, not a guarantee: hive may no longer have the session, which is what
// UnlinkItemSessions resolves.
func (db *DB) ItemSessions(ctx context.Context, ref models.ItemRef) ([]ItemSession, error) {
	if !ref.Known() {
		return []ItemSession{}, nil
	}
	rows, err := db.Ctx(ctx).ListItemSessions(ctx, ListItemSessionsParams(ref))
	if err != nil {
		return nil, wrap("listing sessions for an inbox item", err)
	}
	return rows, nil
}

// UnlinkItemSessions drops links to sessions hive no longer has. Callers must
// only pass ids a *successful* session listing failed to account for — a
// listing that errored proves nothing about what still exists.
func (db *DB) UnlinkItemSessions(ctx context.Context, sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	bound := db.Ctx(ctx)
	for _, id := range sessionIDs {
		if err := bound.DeleteItemSession(ctx, id); err != nil {
			return wrap("unlinking a session from its inbox item", err)
		}
	}
	return nil
}
