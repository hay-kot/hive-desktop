package queries

import (
	"context"
	"database/sql"
	"errors"
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
//
// stores.InboxItemStore.ResolveScoped is the same logic for stores; the two
// coexist while IngestObservation and CommitBatch stay on *DB (phase 3b moves
// both onto the store and this function goes with them).
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
