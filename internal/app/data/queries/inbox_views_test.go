package queries

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func itemIDs(items []InboxItemView) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestMarkInboxItemsRead(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	insert := func(externalID string, unread int64) InboxItem {
		row, err := db.InsertInboxItem(ctx, InsertInboxItemParams{ProfileID: "p", SourceKind: "github", ExternalID: externalID, Title: externalID, Payload: []byte(`{}`), Unread: unread, Lifecycle: "active", FirstSeenAt: 1, LastEventAt: 1})
		require.NoError(t, err)
		return row
	}
	claim := func(feedID string, itemID int64) {
		require.NoError(t, db.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{ProfileID: "p", FeedID: feedID, ItemID: itemID, SourceID: "source-a"}))
	}

	active := insert("active", 1)
	sameFeed := insert("same-feed", 1)
	alreadyRead := insert("already-read", 0)
	archived := insert("archived", 1)
	ignored := insert("ignored", 1)
	otherFeed := insert("other-feed", 1)
	unrouted := insert("unrouted", 1)
	otherProfileItem, err := db.InsertInboxItem(ctx, InsertInboxItemParams{ProfileID: "other", SourceKind: "github", ExternalID: "other", Title: "other", Payload: []byte(`{}`), Unread: 1, Lifecycle: "active", FirstSeenAt: 1, LastEventAt: 1})
	require.NoError(t, err)

	for _, id := range []int64{active.ID, sameFeed.ID, alreadyRead.ID, archived.ID, ignored.ID} {
		claim("feed-a", id)
	}
	claim("feed-b", otherFeed.ID)
	require.NoError(t, db.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{ProfileID: "other", FeedID: "feed-c", ItemID: otherProfileItem.ID, SourceID: "source-a"}))
	_, err = db.ToggleInboxItemArchived(ctx, archived.ID, archived.Revision, 99)
	require.NoError(t, err)
	// Ignoring already clears unread, so force it back on: the query must skip
	// ignored rows on their own merit, not because they happen to be read.
	ignoredRow, err := db.ToggleInboxItemIgnored(ctx, ignored.ID, ignored.Revision, 99)
	require.NoError(t, err)
	_, err = db.SetInboxItemUnread(ctx, ignored.ID, ignoredRow.Revision, true)
	require.NoError(t, err)

	unreadByID := func() map[int64]bool {
		state := map[int64]bool{}
		for _, id := range []int64{active.ID, sameFeed.ID, alreadyRead.ID, archived.ID, ignored.ID, otherFeed.ID, unrouted.ID, otherProfileItem.ID} {
			row, getErr := db.GetInboxItemByID(ctx, id)
			require.NoError(t, getErr)
			state[id] = row.Unread != 0
		}
		return state
	}

	_, err = db.MarkInboxItemsRead(ctx, "", "feed-a")
	require.Error(t, err, "a bulk clear always names its profile")

	marked, err := db.MarkInboxItemsRead(ctx, "p", "feed-a")
	require.NoError(t, err)
	assert.Equal(t, int64(2), marked, "only the feed's unread, unarchived, unignored rows count")
	assert.Equal(t, map[int64]bool{
		active.ID: false, sameFeed.ID: false, alreadyRead.ID: false,
		archived.ID: true, ignored.ID: true, otherFeed.ID: true, unrouted.ID: true, otherProfileItem.ID: true,
	}, unreadByID())

	// The clear advances revisions, so a per-item write holding a pre-clear
	// copy is rejected rather than resurrecting the unread flag.
	_, err = db.SetInboxItemUnread(ctx, active.ID, active.Revision, true)
	require.ErrorIs(t, err, ErrStaleInboxItem)

	repeat, err := db.MarkInboxItemsRead(ctx, "p", "feed-a")
	require.NoError(t, err)
	assert.Zero(t, repeat, "a second pass has nothing left to clear")

	all, err := db.MarkInboxItemsRead(ctx, "p", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), all, "the workspace variant reaches every feed, and nothing else")
	assert.Equal(t, map[int64]bool{
		active.ID: false, sameFeed.ID: false, alreadyRead.ID: false,
		archived.ID: true, ignored.ID: true, otherFeed.ID: false, unrouted.ID: true, otherProfileItem.ID: true,
	}, unreadByID())

	counts, err := db.FeedCounts(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, []FeedInboxCount{{FeedID: "feed-a", Total: 3, Unread: 0, Archived: 1}, {FeedID: "feed-b", Total: 1, Unread: 0}}, counts)
}

func TestFeedViewsTriageAndCounts(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	first, err := db.InsertInboxItem(ctx, InsertInboxItemParams{ProfileID: "p", SourceKind: "github", ExternalID: "one", Title: "one", Payload: []byte(`{}`), Unread: 1, Lifecycle: "active", FirstSeenAt: 1, LastEventAt: 2})
	require.NoError(t, err)
	second, err := db.InsertInboxItem(ctx, InsertInboxItemParams{ProfileID: "p", SourceKind: "github", ExternalID: "two", Title: "two", Payload: []byte(`{}`), Lifecycle: "active", FirstSeenAt: 1, LastEventAt: 2})
	require.NoError(t, err)
	unmatched, err := db.InsertInboxItem(ctx, InsertInboxItemParams{ProfileID: "p", SourceKind: "github", ExternalID: "outside", Title: "outside", Payload: []byte(`{}`), Unread: 1, Lifecycle: "active", FirstSeenAt: 1, LastEventAt: 3})
	require.NoError(t, err)
	require.NoError(t, db.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{ProfileID: "p", FeedID: "feed-a", ItemID: first.ID, SourceID: "source-a"}))
	// A feed is a set of inbox items, not source claims: two sources may claim
	// the same item into one feed without duplicating its row or its unread count.
	require.NoError(t, db.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{ProfileID: "p", FeedID: "feed-a", ItemID: first.ID, SourceID: "source-b"}))
	require.NoError(t, db.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{ProfileID: "p", FeedID: "feed-b", ItemID: second.ID, SourceID: "source-a"}))

	byFeed, err := db.ListInboxItemsByFeed(ctx, "p", "feed-a", 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{first.ID}, itemIDs(byFeed))
	byOtherFeed, err := db.ListInboxItemsByFeed(ctx, "p", "feed-b", 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{second.ID}, itemIDs(byOtherFeed))
	counts, err := db.FeedCounts(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, []FeedInboxCount{{FeedID: "feed-a", Total: 1, Unread: 1}, {FeedID: "feed-b", Total: 1, Unread: 0}}, counts)

	// Items that never reached a feed terminal are visible only in Trash.
	trash, err := db.ListInboxItemsTrash(ctx, "p", 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{unmatched.ID}, itemIDs(trash), "unrouted items land in trash, not feeds")

	updated, err := db.SetInboxItemUnread(ctx, first.ID, first.Revision, false)
	require.NoError(t, err)
	assert.Equal(t, first.Revision+1, updated.Revision)
	_, err = db.SetInboxItemUnread(ctx, first.ID, first.Revision, true)
	require.ErrorIs(t, err, ErrStaleInboxItem)

	// Archiving demotes the item into the feed's archived section: it leaves
	// the active list but stays reachable in the same feed.
	archived, err := db.ToggleInboxItemArchived(ctx, first.ID, updated.Revision, 99)
	require.NoError(t, err)
	assert.Equal(t, updated.Revision+1, archived.Revision)
	_, err = db.ToggleInboxItemArchived(ctx, first.ID, updated.Revision, 99)
	require.ErrorIs(t, err, ErrStaleInboxItem)
	byFeed, err = db.ListInboxItemsByFeed(ctx, "p", "feed-a", 10)
	require.NoError(t, err)
	assert.Empty(t, byFeed)
	archivedByFeed, err := db.ListArchivedInboxItemsByFeed(ctx, "p", "feed-a", 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{first.ID}, itemIDs(archivedByFeed))
	counts, err = db.FeedCounts(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, []FeedInboxCount{{FeedID: "feed-a", Total: 0, Unread: 0, Archived: 1}, {FeedID: "feed-b", Total: 1, Unread: 0}}, counts)

	// Archived ordering: newest archive first, id breaks ties.
	third, err := db.InsertInboxItem(ctx, InsertInboxItemParams{ProfileID: "p", SourceKind: "github", ExternalID: "three", Title: "three", Payload: []byte(`{}`), Lifecycle: "active", FirstSeenAt: 1, LastEventAt: 1})
	require.NoError(t, err)
	fourth, err := db.InsertInboxItem(ctx, InsertInboxItemParams{ProfileID: "p", SourceKind: "github", ExternalID: "four", Title: "four", Payload: []byte(`{}`), Lifecycle: "active", FirstSeenAt: 1, LastEventAt: 1})
	require.NoError(t, err)
	require.NoError(t, db.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{ProfileID: "p", FeedID: "feed-a", ItemID: third.ID, SourceID: "source-a"}))
	require.NoError(t, db.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{ProfileID: "p", FeedID: "feed-a", ItemID: fourth.ID, SourceID: "source-a"}))
	_, err = db.ToggleInboxItemArchived(ctx, third.ID, third.Revision, 99)
	require.NoError(t, err)
	_, err = db.ToggleInboxItemArchived(ctx, fourth.ID, fourth.Revision, 99)
	require.NoError(t, err)
	archivedByFeed, err = db.ListArchivedInboxItemsByFeed(ctx, "p", "feed-a", 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{fourth.ID, third.ID, first.ID}, itemIDs(archivedByFeed), "id breaks identical archived_at ties deterministically")

	// Ignoring removes the item from its feed entirely and moves it to Trash.
	ignored, err := db.ToggleInboxItemIgnored(ctx, second.ID, second.Revision, 101)
	require.NoError(t, err)
	assert.NotNil(t, ignored.IgnoredAt)
	assert.False(t, ignored.Unread)
	byOtherFeed, err = db.ListInboxItemsByFeed(ctx, "p", "feed-b", 10)
	require.NoError(t, err)
	assert.Empty(t, byOtherFeed)
	trash, err = db.ListInboxItemsTrash(ctx, "p", 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{unmatched.ID, second.ID}, itemIDs(trash), "trash holds unrouted and ignored items, newest activity first")
	counts, err = db.FeedCounts(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, []FeedInboxCount{{FeedID: "feed-a", Total: 0, Unread: 0, Archived: 3}}, counts, "an ignored item's feed drops from counts entirely")

	// Archiving an ignored item clears the ignore: the states are mutually
	// exclusive, and the item returns to its feed's archived section.
	movedToArchive, err := db.ToggleInboxItemArchived(ctx, second.ID, ignored.Revision, 102)
	require.NoError(t, err)
	assert.Nil(t, movedToArchive.IgnoredAt, "archive and ignored are mutually exclusive")
	assert.NotNil(t, movedToArchive.ArchivedAt)
	trash, err = db.ListInboxItemsTrash(ctx, "p", 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{unmatched.ID}, itemIDs(trash))
	archivedOther, err := db.ListArchivedInboxItemsByFeed(ctx, "p", "feed-b", 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{second.ID}, itemIDs(archivedOther))
}
