package queries

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
)

func seedReplayItem(t *testing.T, db *DB, profile, external string) InboxItem {
	t.Helper()
	item, err := db.InsertInboxItem(t.Context(), InsertInboxItemParams{
		ProfileID: profile, SourceKind: "github", SourceScope: "scope", ExternalID: external,
		Payload: []byte(`{}`), Lifecycle: "active",
	})
	require.NoError(t, err)
	return item
}

func TestCommitBatch_EmptySourceSnapshotClearsOnlyThatSourceClaims(t *testing.T) {
	db := openTestDB(t)
	item := seedReplayItem(t, db, "flow", "item")
	feed := models.Sink{Kind: models.SinkKindFeed, TargetID: "flow/feed"}
	commit := func(offset int64, source string, snapshot bool) {
		snapshotID := strconv.FormatInt(offset, 10)
		batch := models.CommitBatch{Consumer: "flow", UpToOffset: offset, Outputs: []models.Output{{Sink: feed, Key: "item", SourceKind: "github", SourceScope: "scope", SourceTopic: source}}}
		if snapshot {
			batch.Outputs[0].SnapshotID = snapshotID
			batch.FeedSnapshots = []models.FeedSnapshot{{FeedID: feed.TargetID, SourceTopic: source, SnapshotID: snapshotID}}
		}
		require.NoError(t, db.CommitBatch(t.Context(), batch))
	}
	commit(1, "source:flow/a", true)
	commit(2, "source:flow/b", true)
	require.NoError(t, db.CommitBatch(t.Context(), models.CommitBatch{Consumer: "flow", UpToOffset: 3, FeedSnapshots: []models.FeedSnapshot{{FeedID: feed.TargetID, SourceTopic: "source:flow/a", SnapshotID: "3"}}}))

	var source string
	require.NoError(t, db.Conn().QueryRowContext(t.Context(), `SELECT source_id FROM feed_membership_claim WHERE item_id = ?`, item.ID).Scan(&source))
	assert.Equal(t, "source:flow/b", source)
}

func activateReplay(t *testing.T, db *DB, profile string, claims []FeedMembershipClaim) {
	t.Helper()
	feeds := make([]string, 0, len(claims))
	sources := make([]string, 0, len(claims))
	for _, claim := range claims {
		feeds = append(feeds, claim.FeedID)
		sources = append(sources, claim.SourceID)
	}
	tail, err := db.EventLogTailOffset(t.Context())
	require.NoError(t, err)
	require.NoError(t, db.ActivateReplay(t.Context(), profile, tail, claims, feeds, sources, nil))
}

func TestActivateReplay_OnlyWritesClaimsAndOffset(t *testing.T) {
	db := openTestDB(t)
	item := seedReplayItem(t, db, "flow", "item")
	activateReplay(t, db, "flow", []FeedMembershipClaim{{FeedID: "flow/feed", ItemID: item.ID, SourceID: "source:flow/a"}})
	var commands, offsets, claims int
	require.NoError(t, db.Conn().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM output_command`).Scan(&commands))
	require.NoError(t, db.Conn().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM consumer_offset`).Scan(&offsets))
	require.NoError(t, db.Conn().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM feed_membership_claim`).Scan(&claims))
	assert.Zero(t, commands)
	assert.Equal(t, 1, offsets)
	assert.Equal(t, 1, claims)
}

func TestActivateReplay_RollsBackOffsetAndClaimsOnFailure(t *testing.T) {
	db := openTestDB(t)
	item := seedReplayItem(t, db, "flow", "item")
	require.NoError(t, db.UpsertFeedMembershipClaim(t.Context(), UpsertFeedMembershipClaimParams{ProfileID: "flow", FeedID: "flow/old", ItemID: item.ID, SourceID: "source:flow/old"}))
	_, err := db.Append(t.Context(), "source:flow/source", "item", []byte(`{}`))
	require.NoError(t, err)

	err = db.ActivateReplay(t.Context(), "flow", 1, []FeedMembershipClaim{{ProfileID: "other", FeedID: "flow/new", ItemID: item.ID, SourceID: "source:flow/new"}}, []string{"flow/new"}, []string{"source:flow/new"}, nil)
	require.EqualError(t, err, `activating replay: claim profile "other" does not match "flow"`)

	var feed string
	require.NoError(t, db.Conn().QueryRowContext(t.Context(), `SELECT feed_id FROM feed_membership_claim WHERE item_id = ?`, item.ID).Scan(&feed))
	assert.Equal(t, "flow/old", feed)
	var offsets int
	require.NoError(t, db.Conn().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM consumer_offset`).Scan(&offsets))
	assert.Zero(t, offsets)
}

func TestActivateReplay_ReplacesOnlyUnarchivedClaims(t *testing.T) {
	db := openTestDB(t)
	open := seedReplayItem(t, db, "flow", "open")
	archived := seedReplayItem(t, db, "flow", "archived")
	require.NoError(t, db.UpsertFeedMembershipClaim(t.Context(), UpsertFeedMembershipClaimParams{ProfileID: "flow", FeedID: "flow/feed", ItemID: open.ID, SourceID: "source:flow/old"}))
	require.NoError(t, db.UpsertFeedMembershipClaim(t.Context(), UpsertFeedMembershipClaimParams{ProfileID: "flow", FeedID: "flow/feed", ItemID: archived.ID, SourceID: "source:flow/old"}))
	_, err := db.Conn().ExecContext(t.Context(), `UPDATE inbox_item SET archived_at = 1, archived_actor = 'manual' WHERE id = ?`, archived.ID)
	require.NoError(t, err)

	require.NoError(t, db.ActivateReplay(t.Context(), "flow", 0, []FeedMembershipClaim{{FeedID: "flow/new", ItemID: open.ID, SourceID: "source:flow/new"}}, []string{"flow/feed", "flow/new"}, []string{"source:flow/new"}, nil))
	rows, err := db.Conn().QueryContext(t.Context(), `SELECT feed_id, item_id, source_id FROM feed_membership_claim ORDER BY item_id`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var got []string
	for rows.Next() {
		var feed, source string
		var itemID int64
		require.NoError(t, rows.Scan(&feed, &itemID, &source))
		got = append(got, feed+"/"+source)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"flow/new/source:flow/new", "flow/feed/source:flow/old"}, got)
}

func TestActivateReplay_DoesNotMutateInboxTriage(t *testing.T) {
	db := openTestDB(t)
	item := seedReplayItem(t, db, "flow", "item")
	_, err := db.Conn().ExecContext(t.Context(), `UPDATE inbox_item SET unread = 1, lifecycle = 'terminal', archived_reason = 'manual' WHERE id = ?`, item.ID)
	require.NoError(t, err)
	require.NoError(t, db.UpsertFeedMembershipClaim(t.Context(), UpsertFeedMembershipClaimParams{ProfileID: "flow", FeedID: "flow/old", ItemID: item.ID, SourceID: "source:flow/old"}))

	// A narrowed replay removes the membership but must not treat the inbox row
	// as a fresh observation or alter its lifecycle/triage state.
	activateReplay(t, db, "flow", nil)
	var unread int64
	var lifecycle, reason string
	require.NoError(t, db.Conn().QueryRowContext(t.Context(), `SELECT unread, lifecycle, archived_reason FROM inbox_item WHERE id = ?`, item.ID).Scan(&unread, &lifecycle, &reason))
	assert.Equal(t, int64(1), unread)
	assert.Equal(t, "terminal", lifecycle)
	assert.Equal(t, "manual", reason)
	var claims int
	require.NoError(t, db.Conn().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM feed_membership_claim WHERE item_id = ?`, item.ID).Scan(&claims))
	assert.Zero(t, claims)
}

func TestActivateReplay_NoFeedsDeletesAllClaims(t *testing.T) {
	db := openTestDB(t)
	open := seedReplayItem(t, db, "flow", "open")
	archived := seedReplayItem(t, db, "flow", "archived")
	_, err := db.Conn().ExecContext(t.Context(), `UPDATE inbox_item SET archived_at = 1, archived_actor = 'manual' WHERE id = ?`, archived.ID)
	require.NoError(t, err)
	for _, itemID := range []int64{open.ID, archived.ID} {
		require.NoError(t, db.UpsertFeedMembershipClaim(t.Context(), UpsertFeedMembershipClaimParams{ProfileID: "flow", FeedID: "flow/removed", ItemID: itemID, SourceID: "source:flow/removed"}))
	}

	require.NoError(t, db.ActivateReplay(t.Context(), "flow", 0, nil, nil, []string{"source:flow/live"}, nil))
	var claims int
	require.NoError(t, db.Conn().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM feed_membership_claim WHERE profile_id = 'flow'`).Scan(&claims))
	assert.Zero(t, claims)
}

func TestActivateReplay_UsesRetentionSafeHighWaterMark(t *testing.T) {
	db := openTestDB(t)
	for _, key := range []string{"one", "two"} {
		_, err := db.Append(t.Context(), "source:flow/a", key, []byte(`{}`))
		require.NoError(t, err)
	}
	tail, err := db.EventLogTailOffset(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(2), tail)

	_, err = db.Conn().ExecContext(t.Context(), `DELETE FROM event_log`)
	require.NoError(t, err)
	var rows int
	require.NoError(t, db.Conn().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM event_log`).Scan(&rows))
	require.Zero(t, rows)

	require.NoError(t, db.ActivateReplay(t.Context(), "flow", tail, nil, nil, nil, nil))
	offset, err := db.ConsumerOffset(t.Context(), "flow")
	require.NoError(t, err)
	assert.Equal(t, tail, offset)

	err = db.ActivateReplay(t.Context(), "other-flow", tail+1, nil, nil, nil, nil)
	require.EqualError(t, err, `activating replay for "other-flow": supplied tail 3 exceeds current event log tail 2`)
}

func TestListReplaySourceSnapshots_ReturnsLatestSnapshotPerOwnedSource(t *testing.T) {
	db := openTestDB(t)
	_, err := db.AppendSnapshot(t.Context(), "source:flow/a", "github", "scope-a", []models.SnapshotItem{{Key: "old", Payload: []byte(`{"version":1}`)}})
	require.NoError(t, err)
	latestA, err := db.AppendSnapshot(t.Context(), "source:flow/a", "github", "scope-a", []models.SnapshotItem{{Key: "new", Payload: []byte(`{"version":2}`)}})
	require.NoError(t, err)
	latestB, err := db.AppendSnapshot(t.Context(), "source:flow/b", "github", "scope-b", []models.SnapshotItem{})
	require.NoError(t, err)
	_, err = db.AppendSnapshot(t.Context(), "source:flow-other/a", "github", "other", []models.SnapshotItem{{Key: "wrong-profile", Payload: []byte(`{}`)}})
	require.NoError(t, err)

	tail, err := db.EventLogTailOffset(t.Context())
	require.NoError(t, err)
	messages, err := db.ListReplaySourceSnapshots(t.Context(), "flow", tail)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	assert.Equal(t, models.Msg{ID: strconv.FormatInt(latestA, 10), Topic: "source:flow/a", Ts: messages[0].Ts, Payload: json.RawMessage(`[{"key":"new","payload":{"version":2}}]`), Snapshot: []models.SnapshotItem{{Key: "new", Payload: json.RawMessage(`{"version":2}`)}}, SourceKind: "github", SourceScope: "scope-a"}, messages[0])
	assert.Equal(t, models.Msg{ID: strconv.FormatInt(latestB, 10), Topic: "source:flow/b", Ts: messages[1].Ts, Payload: json.RawMessage(`[]`), Snapshot: []models.SnapshotItem{}, SourceKind: "github", SourceScope: "scope-b"}, messages[1])
}

func TestListReplaySourceSnapshots_HonorsCapturedTail(t *testing.T) {
	db := openTestDB(t)
	capturedTail, err := db.AppendSnapshot(t.Context(), "source:flow/a", "github", "scope", []models.SnapshotItem{{Key: "old", Payload: []byte(`{}`)}})
	require.NoError(t, err)
	_, err = db.AppendSnapshot(t.Context(), "source:flow/a", "github", "scope", []models.SnapshotItem{{Key: "new", Payload: []byte(`{}`)}})
	require.NoError(t, err)

	messages, err := db.ListReplaySourceSnapshots(t.Context(), "flow", capturedTail)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "old", messages[0].Snapshot[0].Key)
	assert.Equal(t, "github", messages[0].SourceKind)
	assert.Equal(t, "scope", messages[0].SourceScope)
}

func TestActivateReplay_ProtectsArchivedClaims(t *testing.T) {
	db := openTestDB(t)
	open := seedReplayItem(t, db, "flow", "open")
	archived := seedReplayItem(t, db, "flow", "archived")
	_, err := db.Conn().ExecContext(t.Context(), `UPDATE inbox_item SET archived_at = 1, archived_actor = 'manual' WHERE id = ?`, archived.ID)
	require.NoError(t, err)
	for _, id := range []int64{open.ID, archived.ID} {
		require.NoError(t, db.UpsertFeedMembershipClaim(t.Context(), UpsertFeedMembershipClaimParams{ProfileID: "flow", FeedID: "flow/feed", ItemID: id, SourceID: "source:flow/removed"}))
	}
	require.NoError(t, db.ActivateReplay(t.Context(), "flow", 0, nil, []string{"flow/feed"}, []string{"source:flow/live"}, nil))
	var ids []int64
	rows, err := db.Conn().QueryContext(t.Context(), `SELECT item_id FROM feed_membership_claim ORDER BY item_id`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int64{archived.ID}, ids)
}

func TestPurgeProfile_DeletesAllOwnedStateIdempotently(t *testing.T) {
	db := openTestDB(t)
	item := seedReplayItem(t, db, "flow", "item")
	_, err := db.InsertInboxEvent(t.Context(), InsertInboxEventParams{ItemID: item.ID, Kind: "updated", Transition: "none", Attention: "activity", Detail: []byte(`{}`), CreatedAt: 1})
	require.NoError(t, err)
	require.NoError(t, db.UpsertFeedMembershipClaim(t.Context(), UpsertFeedMembershipClaimParams{ProfileID: "flow", FeedID: "flow/feed", ItemID: item.ID, SourceID: "source:flow/source"}))
	_, err = db.Append(t.Context(), "source:flow/source", "item", []byte(`{}`))
	require.NoError(t, err)
	require.NoError(t, db.UpsertSourceHead(t.Context(), UpsertSourceHeadParams{Topic: "source:flow/source", Key: "item", Payload: []byte(`{}`)}))
	require.NoError(t, db.CommitBatch(t.Context(), models.CommitBatch{Consumer: "flow", UpToOffset: 1}))

	for _, table := range []string{"inbox_item", "inbox_event", "feed_membership_claim", "consumer_offset", "source_head", "event_log"} {
		var n int
		require.NoError(t, db.Conn().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n))
		require.Equalf(t, 1, n, "seed %s", table)
	}

	require.NoError(t, db.PurgeProfile(t.Context(), "flow"))
	require.NoError(t, db.PurgeProfile(t.Context(), "flow"))
	for _, table := range []string{"inbox_item", "inbox_event", "feed_membership_claim", "consumer_offset", "source_head", "event_log"} {
		var n int
		require.NoError(t, db.Conn().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n))
		assert.Zero(t, n, table)
	}
}

func TestActivateReplay_ReconcilesNodeKV(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	require.NoError(t, db.NodeKVSet(ctx, "flow", "kept", "seen", `1`, 0))
	require.NoError(t, db.NodeKVSet(ctx, "flow", "removed", "seen", `1`, 0))
	require.NoError(t, db.NodeKVSet(ctx, "other", "kept", "seen", `1`, 0))

	// "kept" stays — this also models a rename and a same-id recreate, both of
	// which preserve the id and therefore the KV.
	require.NoError(t, db.ActivateReplay(ctx, "flow", 0, nil, nil, nil, []string{"kept"}))

	_, found, err := db.NodeKVGet(ctx, "flow", "kept", "seen", 1)
	require.NoError(t, err)
	assert.True(t, found)
	_, found, err = db.NodeKVGet(ctx, "flow", "removed", "seen", 1)
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = db.NodeKVGet(ctx, "other", "kept", "seen", 1)
	require.NoError(t, err)
	assert.True(t, found, "another flow's rows are untouched")

	// No KV-capable nodes left clears the whole flow's KV.
	require.NoError(t, db.ActivateReplay(ctx, "flow", 0, nil, nil, nil, nil))
	_, found, err = db.NodeKVGet(ctx, "flow", "kept", "seen", 1)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestActivateReplay_FailureLeavesNodeKVIntact(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	item := seedReplayItem(t, db, "flow", "item")

	require.NoError(t, db.NodeKVSet(ctx, "flow", "old", "seen", `1`, 0))

	// The mismatched claim profile fails the transaction after the KV
	// reconcile would have cleared "old".
	err := db.ActivateReplay(ctx, "flow", 0,
		[]FeedMembershipClaim{{ProfileID: "other", FeedID: "flow/f", ItemID: item.ID, SourceID: "source:flow/s"}},
		nil, nil, []string{"survivor"})
	require.Error(t, err)

	_, found, err := db.NodeKVGet(ctx, "flow", "old", "seen", 1)
	require.NoError(t, err)
	assert.True(t, found, "a failed activation must leave last-known-good KV")
}

func TestPurgeProfile_DeletesNodeKV(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	require.NoError(t, db.NodeKVSet(ctx, "flow", "fn", "seen", `1`, 0))
	require.NoError(t, db.NodeKVSet(ctx, "other", "fn", "seen", `1`, 0))

	require.NoError(t, db.PurgeProfile(ctx, "flow"))

	_, found, err := db.NodeKVGet(ctx, "flow", "fn", "seen", 1)
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = db.NodeKVGet(ctx, "other", "fn", "seen", 1)
	require.NoError(t, err)
	assert.True(t, found)
}
