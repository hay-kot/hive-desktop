package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
	"github.com/hay-kot/hive-desktop/internal/app/data/stores"
)

// newTestActivityService moved from activity/recorder_test.go along with the
// persistence it exercises: activity.Store no longer holds a database, so its
// round-trip and validation behaviour is ActivityService's to test.
func newTestActivityService(t *testing.T, onAppended func(id int64)) *ActivityService {
	t.Helper()
	db, err := queries.Open(t.Context(), t.TempDir(), queries.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return newActivityService(stores.New(db, stores.Options{}).ActivityEvents, onAppended)
}

func TestActivityService_AppendRoundTrip(t *testing.T) {
	emitted := 0
	var lastEmittedID int64
	service := newTestActivityService(t, func(id int64) { emitted++; lastEmittedID = id })
	ctx := t.Context()

	stored, err := service.Append(ctx, activity.ActionRun("Reproduce & fix", "exit 0"))
	require.NoError(t, err)
	require.NotZero(t, stored.ID)
	require.NotZero(t, stored.CreatedAt)
	require.Equal(t, activity.CategoryAction, stored.Category)
	require.Equal(t, activity.SeveritySuccess, stored.Severity)
	require.Equal(t, "Ran Reproduce & fix", stored.Title)
	require.Equal(t, 1, emitted, "onAppended fires once per successful append")
	require.Equal(t, stored.ID, lastEmittedID, "onAppended carries the new event id")

	events, err := service.List(ctx, 0, 50)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, stored, events[0])
}

func TestActivityService_ListNewestFirstAndCursor(t *testing.T) {
	db, err := queries.Open(t.Context(), t.TempDir(), queries.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// A fixed, monotonically advancing clock keeps created_at deterministic;
	// ordering itself is by the autoincrement id, not the timestamp.
	now := time.Unix(0, 0)
	st := stores.New(db, stores.Options{Now: func() time.Time { now = now.Add(time.Second); return now }})
	service := newActivityService(st.ActivityEvents, nil)
	ctx := t.Context()

	for range 5 {
		_, err := service.Append(ctx, activity.ActionRun("Reproduce & fix", "exit 0"))
		require.NoError(t, err)
	}

	page1, err := service.List(ctx, 0, 3)
	require.NoError(t, err)
	require.Len(t, page1, 3)
	require.Greater(t, page1[0].ID, page1[1].ID, "newest first")
	require.Greater(t, page1[1].ID, page1[2].ID)

	page2, err := service.List(ctx, page1[2].ID, 3)
	require.NoError(t, err)
	require.Len(t, page2, 2, "cursor pages the remainder")
	require.Less(t, page2[0].ID, page1[2].ID)
}

func TestActivityService_AppendRejectsBadInput(t *testing.T) {
	service := newTestActivityService(t, nil)
	ctx := t.Context()

	_, err := service.Append(ctx, activity.Event{Category: activity.CategorySystem, Severity: activity.SeverityInfo})
	require.Error(t, err, "missing title is rejected")
	require.Equal(t, KindInvalid, KindOf(err))

	_, err = service.Append(ctx, activity.Event{Title: "bad", Category: activity.Category("nope"), Severity: activity.SeverityInfo})
	require.Error(t, err, "invalid category is rejected")
	require.Equal(t, KindInvalid, KindOf(err))
}

func TestActivityService_AppendDefaultsCategoryAndSeverity(t *testing.T) {
	service := newTestActivityService(t, nil)
	stored, err := service.Append(t.Context(), activity.Event{Title: "something happened"})
	require.NoError(t, err)
	require.Equal(t, activity.CategorySystem, stored.Category)
	require.Equal(t, activity.SeverityInfo, stored.Severity)
}

// TestActivityService_RecordSwallowsErrors covers the fire-and-forget path
// activity.Recorder promises: a persistence failure must never reach the
// caller.
func TestActivityService_RecordSwallowsErrors(t *testing.T) {
	service := newTestActivityService(t, nil)
	require.NotPanics(t, func() {
		service.Record(t.Context(), activity.Event{}) // missing title would error from Append
	})
}
