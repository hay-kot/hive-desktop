package app

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
	datastores "github.com/hay-kot/hive-desktop/internal/app/data/stores"
)

// queriesCommitStore adapts *queries.DB's CommitBatch and ActivateReplay --
// still cross-table operations on *DB until phase 3b moves them onto
// EventLogStore -- to runtime.CommitStore. Deleted along with this type in
// that phase; nothing else should come to depend on it.
type queriesCommitStore struct {
	db *queries.DB
}

func (a queriesCommitStore) Commit(ctx context.Context, batch models.CommitBatch) error {
	return a.db.CommitBatch(ctx, batch)
}

func (a queriesCommitStore) ActivateReplay(ctx context.Context, profileID string, tail int64, claims []datastores.FeedClaim, feedIDs, sourceIDs, kvNodeIDs []string) error {
	converted := make([]queries.FeedMembershipClaim, len(claims))
	for i, c := range claims {
		converted[i] = queries.FeedMembershipClaim{ProfileID: c.ProfileID, FeedID: c.FeedID, ItemID: c.ItemID, SourceID: c.SourceID}
	}
	return a.db.ActivateReplay(ctx, profileID, tail, converted, feedIDs, sourceIDs, kvNodeIDs)
}
