package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func itemRef() ItemRef {
	return ItemRef{ProfileID: "p", SourceKind: "github", SourceScope: "acct", ExternalID: "acme/repo#1"}
}

func TestItemSessions_LinksAndListsNewestFirst(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	ref := itemRef()

	require.NoError(t, db.LinkItemSession(ctx, "sess-a", ref))
	require.NoError(t, db.LinkItemSession(ctx, "sess-b", ref))
	// Links are minted with time.Now(), so two created in the same test would
	// tie on created_at; pin them apart to assert the ordering itself.
	_, err := db.Conn().ExecContext(ctx, `UPDATE item_session SET created_at = 100 WHERE session_id = 'sess-a'`)
	require.NoError(t, err)
	_, err = db.Conn().ExecContext(ctx, `UPDATE item_session SET created_at = 200 WHERE session_id = 'sess-b'`)
	require.NoError(t, err)

	links, err := db.ItemSessions(ctx, ref)
	require.NoError(t, err)
	require.Len(t, links, 2)
	assert.Equal(t, "sess-b", links[0].SessionID)
	assert.Equal(t, "sess-a", links[1].SessionID)
}

func TestItemSessions_ScopedToTheItem(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	ref := itemRef()
	other := ref
	other.ExternalID = "acme/repo#2"

	require.NoError(t, db.LinkItemSession(ctx, "mine", ref))
	require.NoError(t, db.LinkItemSession(ctx, "theirs", other))

	links, err := db.ItemSessions(ctx, ref)
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, "mine", links[0].SessionID)
}

// A session is created once and hive owns its id, so a second link for the same
// id is the same association rather than a duplicate row.
func TestLinkItemSession_IsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	ref := itemRef()

	require.NoError(t, db.LinkItemSession(ctx, "sess-a", ref))
	require.NoError(t, db.LinkItemSession(ctx, "sess-a", ref))

	links, err := db.ItemSessions(ctx, ref)
	require.NoError(t, err)
	assert.Len(t, links, 1)
}

// An action invoked from a surface with no inbox item behind it carries a zero
// ref, which must not become a link every such action shares.
func TestLinkItemSession_IgnoresAnUnknownRef(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	require.NoError(t, db.LinkItemSession(ctx, "sess-a", ItemRef{}))
	require.NoError(t, db.LinkItemSession(ctx, "", itemRef()))

	var count int
	require.NoError(t, db.Conn().QueryRowContext(ctx, `SELECT count(*) FROM item_session`).Scan(&count))
	assert.Equal(t, 0, count)
}

func TestUnlinkItemSessions_DropsOnlyTheNamedSessions(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	ref := itemRef()

	require.NoError(t, db.LinkItemSession(ctx, "gone", ref))
	require.NoError(t, db.LinkItemSession(ctx, "kept", ref))
	require.NoError(t, db.UnlinkItemSessions(ctx, []string{"gone"}))

	links, err := db.ItemSessions(ctx, ref)
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, "kept", links[0].SessionID)
}

// The row id is deliberately not what a link is keyed on: a replay deletes and
// rebuilds every row a profile owns, and an association must outlive that.
func TestItemSessions_SurviveAnItemRowBeingRebuilt(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	current := Observation{ExternalID: "acme/repo#1", Title: "one", SourceKind: "github", SourceScope: "acct", ObservedAt: 100, Payload: []byte(`{"v":1}`)}
	first, err := db.IngestObservation(ctx, activityClassifier("one"), IngestObservationParams{ProfileID: "p", Topic: "source:p/a", Current: current})
	require.NoError(t, err)

	ref, err := db.ItemRefByID(ctx, first.ItemID)
	require.NoError(t, err)
	require.NoError(t, db.LinkItemSession(ctx, "sess-a", ref))

	_, err = db.Conn().ExecContext(ctx, `DELETE FROM inbox_item WHERE profile_id = 'p'`)
	require.NoError(t, err)
	// A rebuild re-observes the item; the payload has moved on, or the ingest
	// would match the source head and write nothing.
	current.Payload = []byte(`{"v":2}`)
	rebuilt, err := db.IngestObservation(ctx, activityClassifier("two"), IngestObservationParams{ProfileID: "p", Topic: "source:p/a", Current: current})
	require.NoError(t, err)
	require.NotEqual(t, first.ItemID, rebuilt.ItemID)

	rebuiltRef, err := db.ItemRefByID(ctx, rebuilt.ItemID)
	require.NoError(t, err)
	links, err := db.ItemSessions(ctx, rebuiltRef)
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, "sess-a", links[0].SessionID)
}

// resolveInboxItemScoped rewrites a pre-#63 row's source_scope in place, and
// the links are keyed on that scope — so they have to move with it.
func TestItemSessions_FollowALegacyRowOntoItsHealedScope(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	legacy, err := db.Queries().InsertInboxItem(ctx, InsertInboxItemParams{
		ProfileID: "p", SourceKind: "github", SourceScope: "", ExternalID: "acme/repo#1",
		Payload: []byte(`{"v":1}`), Lifecycle: "active", Unread: 1,
	})
	require.NoError(t, err)

	legacyRef, err := db.ItemRefByID(ctx, legacy.ID)
	require.NoError(t, err)
	require.NoError(t, db.LinkItemSession(ctx, "sess-a", legacyRef))

	current := Observation{ExternalID: "acme/repo#1", Title: "one", SourceKind: "github", SourceScope: "acct", ObservedAt: 100, Payload: []byte(`{"v":2}`)}
	_, err = db.IngestObservation(ctx, activityClassifier("one"), IngestObservationParams{ProfileID: "p", Topic: "source:p/a", Current: current})
	require.NoError(t, err)

	links, err := db.ItemSessions(ctx, itemRef())
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, "sess-a", links[0].SessionID)
}

func TestPurgeProfile_DropsItsItemSessionLinks(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	ref := itemRef()
	other := ref
	other.ProfileID = "q"

	require.NoError(t, db.LinkItemSession(ctx, "purged", ref))
	require.NoError(t, db.LinkItemSession(ctx, "kept", other))
	require.NoError(t, db.PurgeProfile(ctx, "p"))

	links, err := db.ItemSessions(ctx, ref)
	require.NoError(t, err)
	assert.Empty(t, links)
	links, err = db.ItemSessions(ctx, other)
	require.NoError(t, err)
	assert.Len(t, links, 1)
}

// A flow-fired action's dedup key is the occurrence key, so the item it came
// from can only be found again if the enqueue recorded it.
func TestCommitBatch_RecordsTheItemAnActionCommandCameFrom(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	require.NoError(t, db.CommitBatch(ctx, CommitBatch{
		Consumer:   "p",
		UpToOffset: 1,
		Outputs: []Output{{
			Sink:          Sink{Kind: SinkKindAction, TargetID: "review-pr"},
			Key:           "acme/repo#1",
			OccurrenceKey: "oc-1",
			Payload:       []byte(`{"v":1}`),
			SourceKind:    "github",
			SourceScope:   "acct",
		}},
	}))

	rows, err := db.ListRunnableOutputCommands(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "oc-1", rows[0].Key)
	assert.Equal(t, itemRef(), rows[0].ItemRef())
}

// Claiming a queued flow command must not overwrite the attribution the
// enqueue recorded, and must fill one it never had.
func TestConfirmOutputCommand_KeepsTheRoutedOriginAndFillsAMissingOne(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	routed := itemRef()

	require.NoError(t, db.CommitBatch(ctx, CommitBatch{
		Consumer: "p", UpToOffset: 1,
		Outputs: []Output{{
			Sink: Sink{Kind: SinkKindAction, TargetID: "review-pr"}, Key: "acme/repo#1",
			OccurrenceKey: "oc-1", Payload: []byte(`{"v":1}`), SourceKind: "github", SourceScope: "acct",
		}},
	}))
	other := ItemRef{ProfileID: "q", SourceKind: "webhook", ExternalID: "other"}
	claimed, _, err := db.ConfirmOutputCommand(ctx, "review-pr", "oc-1", []byte(`{"v":1}`), other)
	require.NoError(t, err)
	assert.Equal(t, routed, claimed.ItemRef())

	// A command enqueued with no item behind it takes the confirming caller's,
	// whole rather than column by column.
	_, err = db.Conn().ExecContext(ctx,
		`INSERT INTO output_command (action_id, key, payload, status, created_at) VALUES ('shell-it', 'oc-2', CAST('{}' AS BLOB), 'pending', 1)`)
	require.NoError(t, err)
	filled, _, err := db.ConfirmOutputCommand(ctx, "shell-it", "oc-2", []byte(`{"v":1}`), routed)
	require.NoError(t, err)
	assert.Equal(t, routed, filled.ItemRef())
}
