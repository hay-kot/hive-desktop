-- name: UpsertFeedMembershipClaim :exec
INSERT INTO feed_membership_claim (profile_id, feed_id, item_id, source_id)
VALUES (?, ?, ?, ?)
ON CONFLICT (profile_id, feed_id, item_id, source_id) DO NOTHING;

-- name: DeleteFeedMembershipClaimsNotInSnapshot :exec
DELETE FROM feed_membership_claim
WHERE feed_id = ? AND source_id = ? AND item_id NOT IN (sqlc.slice(item_ids));

-- name: DeleteFeedMembershipClaimsForSourceAll :exec
DELETE FROM feed_membership_claim WHERE feed_id = ? AND source_id = ?;

-- name: DeleteFeedMembershipClaimsForFeeds :exec
DELETE FROM feed_membership_claim
WHERE feed_membership_claim.profile_id = ? AND feed_id NOT IN (sqlc.slice(feed_ids));

-- name: DeleteFeedMembershipClaimsForFeedsAll :exec
DELETE FROM feed_membership_claim WHERE profile_id = ?;

-- name: DeleteFeedMembershipClaimsForRemovedSources :exec
DELETE FROM feed_membership_claim
WHERE feed_membership_claim.profile_id = ?
  AND source_id NOT IN (sqlc.slice(source_ids))
  AND item_id IN (SELECT id FROM inbox_item WHERE archived_at IS NULL);

-- name: DeleteFeedMembershipClaimsForRemovedSourcesAll :exec
DELETE FROM feed_membership_claim
WHERE feed_membership_claim.profile_id = ?
  AND item_id IN (SELECT id FROM inbox_item WHERE archived_at IS NULL);

-- name: DeleteUnarchivedFeedMembershipClaimsByProfile :exec
DELETE FROM feed_membership_claim
WHERE feed_membership_claim.profile_id = ? AND item_id IN (SELECT id FROM inbox_item WHERE archived_at IS NULL);
