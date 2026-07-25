package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedLookupItem(t *testing.T, database *DB, ctx context.Context, externalID string) int64 {
	t.Helper()
	item, err := database.Queries().InsertInboxItem(ctx, InsertInboxItemParams{
		ProfileID: "flow-1", SourceKind: "github", SourceScope: "source-a", ExternalID: externalID,
		Payload: []byte(`{"v":1}`), Lifecycle: "active",
	})
	require.NoError(t, err)
	return item.ID
}

func TestInboxItemID(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	want := seedLookupItem(t, database, ctx, "item-1")

	got, err := database.InboxItemID(ctx, "flow-1", "github", "source-a", "item-1")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	// An identity with no inbox row is "nothing to link to", not a failure:
	// a notification about it still fires, it just has nothing to reveal.
	for _, missing := range [][4]string{
		{"flow-1", "github", "source-a", "item-missing"},
		{"flow-1", "github", "other-source", "item-1"},
		{"other-flow", "github", "source-a", "item-1"},
		{"flow-1", "github", "source-a", ""},
	} {
		got, err := database.InboxItemID(ctx, missing[0], missing[1], missing[2], missing[3])
		require.NoError(t, err, missing)
		assert.Zero(t, got, missing)
	}
}

func TestInboxItemFeedID(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	itemID := seedLookupItem(t, database, ctx, "item-1")

	// An unrouted item belongs to no feed; the UI shows it in Trash.
	feedID, err := database.InboxItemFeedID(ctx, "flow-1", itemID)
	require.NoError(t, err)
	assert.Empty(t, feedID)

	// Several feeds may claim one item; the answer is stable across calls.
	for _, claim := range []string{"flow-1/team", "flow-1/all"} {
		require.NoError(t, database.Queries().UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{
			ProfileID: "flow-1", FeedID: claim, ItemID: itemID, SourceID: "source:flow-1/source-a",
		}))
	}
	feedID, err = database.InboxItemFeedID(ctx, "flow-1", itemID)
	require.NoError(t, err)
	assert.Equal(t, "flow-1/all", feedID)

	// Another workspace's claim is never returned.
	feedID, err = database.InboxItemFeedID(ctx, "other-flow", itemID)
	require.NoError(t, err)
	assert.Empty(t, feedID)
}

func TestInboxItemNotifiable(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	seed := func(t *testing.T, externalID string, unread int64, occurrence string) {
		t.Helper()
		item, err := db.Queries().InsertInboxItem(ctx, InsertInboxItemParams{
			ProfileID: "triage", SourceKind: "github", SourceScope: "src", ExternalID: externalID,
			Payload: []byte(`{}`), Lifecycle: "active", Unread: unread,
		})
		require.NoError(t, err)
		if occurrence == "" {
			return
		}
		_, err = db.Queries().InsertInboxEvent(ctx, InsertInboxEventParams{
			ItemID: item.ID, Kind: "review_requested", Transition: "none", Attention: "activity",
			OccurrenceKey: sql.NullString{String: occurrence, Valid: true}, CreatedAt: 1,
		})
		require.NoError(t, err)
	}

	seed(t, "new", 1, "new:open:100:review_requested")
	seed(t, "read", 0, "read:open:100:review_requested")
	seed(t, "trivial", 1, "")

	notifiable := func(externalID, occurrence string) bool {
		t.Helper()
		ok, err := db.InboxItemNotifiable(ctx, "triage", "github", "src", externalID, occurrence)
		require.NoError(t, err)
		return ok
	}

	assert.True(t, notifiable("new", "new:open:100:review_requested"),
		"unread with a recorded event is exactly the case worth interrupting for")
	assert.False(t, notifiable("read", "read:open:100:review_requested"),
		"triage decided this one does not demand attention")
	// A trivial re-observation records no event, and its occurrence key is a
	// backfilled event-log offset that matches nothing.
	assert.False(t, notifiable("trivial", "17"))
	assert.False(t, notifiable("new", ""), "no occurrence key, nothing to check against")
	assert.False(t, notifiable("missing", "whatever"), "an identity with no inbox row")
}
