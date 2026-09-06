package queries

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedLookupItem(t *testing.T, database *DB, ctx context.Context, externalID string) int64 {
	t.Helper()
	item, err := database.InsertInboxItem(ctx, InsertInboxItemParams{
		ProfileID: "flow-1", SourceKind: "github", SourceScope: "source-a", ExternalID: externalID,
		Payload: []byte(`{"v":1}`), Lifecycle: "active",
	})
	require.NoError(t, err)
	return item.ID
}

func TestInboxItemID(t *testing.T) {
	database := openTestDB(t)
	ctx := t.Context()
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

func TestFindInboxItemsByExternalID(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	insert := func(profile, scope, ext string) {
		_, err := db.InsertInboxItem(ctx, InsertInboxItemParams{
			ProfileID: profile, SourceKind: "github", SourceScope: scope, ExternalID: ext,
			Payload: []byte(`{"v":1}`), Lifecycle: "active",
		})
		require.NoError(t, err)
	}
	insert("p1", "s-a", "PR_1")
	insert("p1", "s-b", "PR_1")
	insert("p2", "s-a", "PR_1")
	insert("p1", "s-a", "PR_2")

	all, err := db.FindInboxItemsByExternalID(ctx, "", "PR_1")
	require.NoError(t, err)
	assert.Len(t, all, 3, "one external id spans profiles and source scopes")

	scoped, err := db.FindInboxItemsByExternalID(ctx, "p1", "PR_1")
	require.NoError(t, err)
	assert.Len(t, scoped, 2)
	for _, item := range scoped {
		assert.Equal(t, "p1", item.ProfileID)
	}

	none, err := db.FindInboxItemsByExternalID(ctx, "", "missing")
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestListAllInboxItems(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	insert := func(profile, ext string) {
		_, err := db.InsertInboxItem(ctx, InsertInboxItemParams{
			ProfileID: profile, SourceKind: "github", SourceScope: "s", ExternalID: ext,
			Payload: []byte(`{"v":1}`), Lifecycle: "active",
		})
		require.NoError(t, err)
	}
	insert("p1", "a")
	insert("p1", "b")
	insert("p2", "c")

	all, err := db.ListAllInboxItems(ctx, "", 10)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	scoped, err := db.ListAllInboxItems(ctx, "p1", 10)
	require.NoError(t, err)
	assert.Len(t, scoped, 2)

	empty, err := db.ListAllInboxItems(ctx, "", 0)
	require.NoError(t, err)
	assert.Empty(t, empty, "a non-positive limit short-circuits")
}

func TestInboxItemFeedID(t *testing.T) {
	database := openTestDB(t)
	ctx := t.Context()
	itemID := seedLookupItem(t, database, ctx, "item-1")

	// An unrouted item belongs to no feed; the UI shows it in Trash.
	feedID, err := database.InboxItemFeedID(ctx, "flow-1", itemID)
	require.NoError(t, err)
	assert.Empty(t, feedID)

	// Several feeds may claim one item; the answer is stable across calls.
	for _, claim := range []string{"flow-1/team", "flow-1/all"} {
		require.NoError(t, database.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{
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
