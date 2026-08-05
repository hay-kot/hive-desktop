package store

import (
	"context"
	"time"
)

// ItemRef identifies an inbox item by inbox_item's own UNIQUE key rather than
// by its row id, which ActivateReplay does not preserve (ADR 0060).
type ItemRef struct {
	ProfileID   string `json:"profileId"`
	SourceKind  string `json:"sourceKind"`
	SourceScope string `json:"sourceScope"`
	ExternalID  string `json:"externalId"`
}

// Known reports whether the ref names an item at all. A profile and an
// external id are the parts that cannot be empty for a real inbox row; source
// scope legitimately is (a connector that fetches as no account).
func (r ItemRef) Known() bool { return r.ProfileID != "" && r.ExternalID != "" }

// ItemRefByID resolves an inbox row to the ref an association is keyed on.
func (db *DB) ItemRefByID(ctx context.Context, itemID int64) (ItemRef, error) {
	row, err := db.Ctx(ctx).queries.GetInboxItemByID(ctx, itemID)
	if err != nil {
		return ItemRef{}, wrap("reading inbox item for its ref", err)
	}
	return ItemRef{
		ProfileID:   row.ProfileID,
		SourceKind:  row.SourceKind,
		SourceScope: row.SourceScope,
		ExternalID:  row.ExternalID,
	}, nil
}

// LinkItemSession records that a hive session was created on ref's behalf.
// Nothing about the session is copied: hive owns its name, state and checkout,
// and a mirror here could only go stale.
func (db *DB) LinkItemSession(ctx context.Context, sessionID string, ref ItemRef) error {
	if sessionID == "" || !ref.Known() {
		return nil
	}
	err := db.Ctx(ctx).queries.LinkItemSession(ctx, LinkItemSessionParams{
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
func (db *DB) ItemSessions(ctx context.Context, ref ItemRef) ([]ItemSession, error) {
	if !ref.Known() {
		return []ItemSession{}, nil
	}
	rows, err := db.Ctx(ctx).queries.ListItemSessions(ctx, ListItemSessionsParams(ref))
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
		if err := bound.queries.DeleteItemSession(ctx, id); err != nil {
			return wrap("unlinking a session from its inbox item", err)
		}
	}
	return nil
}
