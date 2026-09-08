package stores

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// FeedClaimStore owns feed_membership_claim: which items each feed claims,
// and from which source. EventLogStore.Commit and .ActivateReplay call
// these methods as siblings inside their own transaction; this store is the
// leaf half.
type FeedClaimStore struct {
	q *queries.DB
}

func NewFeedClaimStore(q *queries.DB, _ Options) *FeedClaimStore {
	return &FeedClaimStore{q: q}
}

// Upsert claims itemID for feedID under profileID, attributed to sourceID.
// A claim already recorded is a no-op.
func (s *FeedClaimStore) Upsert(ctx context.Context, claim models.FeedClaim) error {
	return wrap("claiming feed membership", s.q.Ctx(ctx).UpsertFeedMembershipClaim(ctx, queries.UpsertFeedMembershipClaimParams{
		ProfileID: claim.ProfileID, FeedID: claim.FeedID, ItemID: claim.ItemID, SourceID: claim.SourceID,
	}))
}

// DeleteNotInSnapshot reconciles one (feed, source) scope down to exactly
// itemIDs, dropping every other claim that scope holds.
func (s *FeedClaimStore) DeleteNotInSnapshot(ctx context.Context, feedID, sourceID string, itemIDs []int64) error {
	return wrap("reconciling feed snapshot", s.q.Ctx(ctx).DeleteFeedMembershipClaimsNotInSnapshot(ctx, queries.DeleteFeedMembershipClaimsNotInSnapshotParams{
		FeedID: feedID, SourceID: sourceID, ItemIds: itemIDs,
	}))
}

// DeleteForSourceAll clears every claim one (feed, source) scope holds --
// the empty-snapshot case DeleteNotInSnapshot cannot express with an empty
// item list.
func (s *FeedClaimStore) DeleteForSourceAll(ctx context.Context, feedID, sourceID string) error {
	return wrap("clearing empty feed snapshot", s.q.Ctx(ctx).DeleteFeedMembershipClaimsForSourceAll(ctx, queries.DeleteFeedMembershipClaimsForSourceAllParams{
		FeedID: feedID, SourceID: sourceID,
	}))
}

// DeleteForFeeds removes profileID's claims against feeds no longer in
// keepFeedIDs. An empty keepFeedIDs clears every feed the profile holds --
// the shape ActivateReplay needs when a flow has no snapshot-reconciled
// nodes left.
func (s *FeedClaimStore) DeleteForFeeds(ctx context.Context, profileID string, keepFeedIDs []string) error {
	q := s.q.Ctx(ctx)
	if len(keepFeedIDs) == 0 {
		return wrap("clearing flow feeds", q.DeleteFeedMembershipClaimsForFeedsAll(ctx, profileID))
	}
	return wrap("removing obsolete feeds", q.DeleteFeedMembershipClaimsForFeeds(ctx, queries.DeleteFeedMembershipClaimsForFeedsParams{
		ProfileID: profileID, FeedIds: keepFeedIDs,
	}))
}

// DeleteForRemovedSources removes profileID's unarchived-item claims against
// sources no longer in keepSourceIDs. An empty keepSourceIDs clears every
// source the profile holds.
func (s *FeedClaimStore) DeleteForRemovedSources(ctx context.Context, profileID string, keepSourceIDs []string) error {
	q := s.q.Ctx(ctx)
	if len(keepSourceIDs) == 0 {
		return wrap("clearing removed sources", q.DeleteFeedMembershipClaimsForRemovedSourcesAll(ctx, profileID))
	}
	return wrap("removing obsolete sources", q.DeleteFeedMembershipClaimsForRemovedSources(ctx, queries.DeleteFeedMembershipClaimsForRemovedSourcesParams{
		ProfileID: profileID, SourceIds: keepSourceIDs,
	}))
}

// DeleteUnarchivedByProfile clears every unarchived-item claim profileID
// holds, the first step of an ActivateReplay activation.
func (s *FeedClaimStore) DeleteUnarchivedByProfile(ctx context.Context, profileID string) error {
	return wrap("clearing replayable memberships", s.q.Ctx(ctx).DeleteUnarchivedFeedMembershipClaimsByProfile(ctx, profileID))
}

// DeleteByProfile clears every claim profileID holds, archived items
// included.
func (s *FeedClaimStore) DeleteByProfile(ctx context.Context, profileID string) error {
	return wrap("clearing profile memberships", s.q.Ctx(ctx).DeleteFeedMembershipClaimsForFeedsAll(ctx, profileID))
}
