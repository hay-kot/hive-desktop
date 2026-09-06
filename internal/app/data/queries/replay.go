package queries

import (
	"context"
	"fmt"
	"strings"
)

// ActivateReplay atomically installs a prepared synthetic replay: it advances
// the consumer past stale action-bound events, replaces unarchived feed
// memberships, removes claims for deleted flow structure, and reconciles the
// flow's node KV against kvNodeIDs — the ids still capable of owning KV. A
// failed activation leaves the last-known-good runtime's offset, claims and
// KV intact.
func (db *DB) ActivateReplay(ctx context.Context, profileID string, tail int64, claims []FeedMembershipClaim, feedIDs, sourceIDs, kvNodeIDs []string) error {
	if tail < 0 {
		return fmt.Errorf("activating replay for %q: negative tail", profileID)
	}
	return db.WithinTx(ctx, func(ctx context.Context, tx *DB) error {
		currentTail, err := tx.GetEventLogTailOffset(ctx)
		if err != nil {
			return fmt.Errorf("reading event log tail: %w", err)
		}
		if tail > currentTail {
			return fmt.Errorf("activating replay for %q: supplied tail %d exceeds current event log tail %d", profileID, tail, currentTail)
		}

		if err := tx.DeleteUnarchivedFeedMembershipClaimsByProfile(ctx, profileID); err != nil {
			return fmt.Errorf("clearing replayable memberships: %w", err)
		}
		for _, claim := range claims {
			if claim.ProfileID != "" && claim.ProfileID != profileID {
				return fmt.Errorf("activating replay: claim profile %q does not match %q", claim.ProfileID, profileID)
			}
			if _, err := tx.GetUnarchivedInboxItemByID(ctx, GetUnarchivedInboxItemByIDParams{ID: claim.ItemID, ProfileID: profileID}); err != nil {
				return fmt.Errorf("activating replay: item %d is not an unarchived item in %q: %w", claim.ItemID, profileID, err)
			}
			if err := tx.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{
				ProfileID: profileID,
				FeedID:    claim.FeedID,
				ItemID:    claim.ItemID,
				SourceID:  claim.SourceID,
			}); err != nil {
				return fmt.Errorf("activating replay membership %s/%d: %w", claim.FeedID, claim.ItemID, err)
			}
		}

		if len(feedIDs) == 0 {
			if err := tx.DeleteFeedMembershipClaimsForFeedsAll(ctx, profileID); err != nil {
				return fmt.Errorf("clearing flow feeds: %w", err)
			}
		} else if err := tx.DeleteFeedMembershipClaimsForFeeds(ctx, DeleteFeedMembershipClaimsForFeedsParams{ProfileID: profileID, FeedIds: feedIDs}); err != nil {
			return fmt.Errorf("removing obsolete feeds: %w", err)
		}
		if len(sourceIDs) == 0 {
			if err := tx.DeleteFeedMembershipClaimsForRemovedSourcesAll(ctx, profileID); err != nil {
				return fmt.Errorf("clearing removed sources: %w", err)
			}
		} else if err := tx.DeleteFeedMembershipClaimsForRemovedSources(ctx, DeleteFeedMembershipClaimsForRemovedSourcesParams{ProfileID: profileID, SourceIds: sourceIDs}); err != nil {
			return fmt.Errorf("removing obsolete sources: %w", err)
		}

		if err := tx.CommitConsumerOffset(ctx, CommitConsumerOffsetParams{Consumer: profileID, Offset: tail}); err != nil {
			return fmt.Errorf("advancing replay consumer offset: %w", err)
		}

		if len(kvNodeIDs) == 0 {
			if err := tx.DeleteNodeKVByFlow(ctx, profileID); err != nil {
				return fmt.Errorf("clearing flow node kv: %w", err)
			}
		} else if err := tx.DeleteNodeKVForFlowExceptNodes(ctx, DeleteNodeKVForFlowExceptNodesParams{FlowID: profileID, NodeIds: kvNodeIDs}); err != nil {
			return fmt.Errorf("removing obsolete node kv: %w", err)
		}
		return nil
	})
}

// PurgeProfile removes all durable state owned by a deleted flow. The topic
// prefix is escaped for LIKE so profile IDs cannot accidentally widen a purge.
func (db *DB) PurgeProfile(ctx context.Context, profileID string) error {
	prefix := "source:" + escapeLike(profileID) + "/%"
	return db.WithinTx(ctx, func(ctx context.Context, tx *DB) error {
		if err := tx.DeleteInboxItemsByProfile(ctx, profileID); err != nil {
			return fmt.Errorf("purging inbox items: %w", err)
		}
		// The sessions themselves are hive's and survive; only the links go,
		// because there is no longer an item for them to hang off.
		if err := tx.DeleteItemSessionsByProfile(ctx, profileID); err != nil {
			return fmt.Errorf("purging item session links: %w", err)
		}
		if err := tx.DeleteConsumerOffsetByConsumer(ctx, profileID); err != nil {
			return fmt.Errorf("purging consumer offset: %w", err)
		}
		if err := tx.DeleteEventLogByTopicPrefix(ctx, prefix); err != nil {
			return fmt.Errorf("purging event log: %w", err)
		}
		if err := tx.DeleteSourceHeadByTopicPrefix(ctx, prefix); err != nil {
			return fmt.Errorf("purging source head: %w", err)
		}
		if err := tx.DeleteNodeKVByFlow(ctx, profileID); err != nil {
			return fmt.Errorf("purging node kv: %w", err)
		}
		return nil
	})
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
