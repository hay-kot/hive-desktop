package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleCursor_UpsertGetListDelete(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	_, ok, err := db.GetScheduleCursor(ctx, "ws-1", "weekly")
	require.NoError(t, err)
	assert.False(t, ok, "a cursor that was never written should not be found")

	require.NoError(t, db.UpsertScheduleCursor(ctx, ScheduleCursorRecord{
		Workspace: "ws-1", ScheduleID: "weekly", EvaluatedThrough: 100, Cron: "0 9 * * 5",
	}))
	cursor, ok, err := db.GetScheduleCursor(ctx, "ws-1", "weekly")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, ScheduleCursorRecord{Workspace: "ws-1", ScheduleID: "weekly", EvaluatedThrough: 100, Cron: "0 9 * * 5"}, cursor)

	// A second upsert for the same key replaces it rather than adding a row.
	require.NoError(t, db.UpsertScheduleCursor(ctx, ScheduleCursorRecord{
		Workspace: "ws-1", ScheduleID: "weekly", EvaluatedThrough: 200, Cron: "0 10 * * 5",
	}))
	cursor, ok, err = db.GetScheduleCursor(ctx, "ws-1", "weekly")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(200), cursor.EvaluatedThrough)
	assert.Equal(t, "0 10 * * 5", cursor.Cron)

	require.NoError(t, db.UpsertScheduleCursor(ctx, ScheduleCursorRecord{
		Workspace: "ws-2", ScheduleID: "daily", EvaluatedThrough: 50, Cron: "@daily",
	}))
	cursors, err := db.ListScheduleCursors(ctx)
	require.NoError(t, err)
	require.Len(t, cursors, 2)

	require.NoError(t, db.DeleteScheduleCursor(ctx, "ws-1", "weekly"))
	_, ok, err = db.GetScheduleCursor(ctx, "ws-1", "weekly")
	require.NoError(t, err)
	assert.False(t, ok)
	cursors, err = db.ListScheduleCursors(ctx)
	require.NoError(t, err)
	require.Len(t, cursors, 1)
	assert.Equal(t, "ws-2", cursors[0].Workspace)
}

func TestDeleteScheduleCursors_RemovesOnlyTheWorkspace(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	for _, cursor := range []ScheduleCursorRecord{
		{Workspace: "ws-1", ScheduleID: "weekly", EvaluatedThrough: 100, Cron: "@weekly"},
		{Workspace: "ws-1", ScheduleID: "daily", EvaluatedThrough: 100, Cron: "@daily"},
		{Workspace: "ws-2", ScheduleID: "weekly", EvaluatedThrough: 100, Cron: "@weekly"},
	} {
		require.NoError(t, db.UpsertScheduleCursor(ctx, cursor))
	}

	require.NoError(t, db.DeleteScheduleCursors(ctx, "ws-1"))

	cursors, err := db.ListScheduleCursors(ctx)
	require.NoError(t, err)
	require.Len(t, cursors, 1)
	assert.Equal(t, "ws-2", cursors[0].Workspace)
}

func TestInsertScheduleRun_ReturnsAssignedID(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	run, err := db.InsertScheduleRun(ctx, ScheduleRunRecord{
		Workspace: "ws-1", ScheduleID: "weekly", ScheduleName: "Weekly summary",
		ScheduledFor: 100, StartedAt: 100, Reason: "due", Status: "launched",
		SessionID: 7, Prompt: "Summarize.",
	})
	require.NoError(t, err)
	assert.NotZero(t, run.ID)
	assert.Equal(t, int64(7), run.SessionID)

	stored, err := db.ListScheduleRunsFor(ctx, "ws-1", "weekly", 10)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, run, stored[0])
}

func TestInsertScheduleRun_NullSessionIDRoundTripsAsZero(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	run, err := db.InsertScheduleRun(ctx, ScheduleRunRecord{
		Workspace: "ws-1", ScheduleID: "weekly", ScheduleName: "Weekly summary",
		ScheduledFor: 100, StartedAt: 100, Reason: "due", Status: "skipped",
		Error: "the previous run's chat is still running",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), run.SessionID)

	stored, err := db.ListScheduleRunsFor(ctx, "ws-1", "weekly", 10)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, int64(0), stored[0].SessionID)

	var raw any
	require.NoError(t, db.Conn().QueryRowContext(ctx, `SELECT session_id FROM schedule_run WHERE id = ?`, run.ID).Scan(&raw))
	assert.Nil(t, raw, "session_id should be stored as NULL, not 0")
}

func TestListScheduleRunsFor_ScopedToOneScheduleNewestFirst(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	weeklyOld := mustInsertRun(t, db, "ws-1", "weekly", 100)
	mustInsertRun(t, db, "ws-1", "daily", 150)
	weeklyNew := mustInsertRun(t, db, "ws-1", "weekly", 200)
	// The same schedule id in a different workspace must not appear.
	mustInsertRun(t, db, "ws-2", "weekly", 300)

	runs, err := db.ListScheduleRunsFor(ctx, "ws-1", "weekly", 10)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, []int64{weeklyNew.ID, weeklyOld.ID}, []int64{runs[0].ID, runs[1].ID})

	limited, err := db.ListScheduleRunsFor(ctx, "ws-1", "weekly", 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)
	assert.Equal(t, weeklyNew.ID, limited[0].ID)
}

func TestLastLaunchedScheduleRun_IgnoresFailedAndSkipped(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	_, ok, err := db.LastLaunchedScheduleRun(ctx, "ws-1", "weekly")
	require.NoError(t, err)
	assert.False(t, ok)

	launched := mustInsertRunWithStatus(t, db, "ws-1", "weekly", 100, "launched")
	mustInsertRunWithStatus(t, db, "ws-1", "weekly", 200, "failed")
	mustInsertRunWithStatus(t, db, "ws-1", "weekly", 300, "skipped")

	last, ok, err := db.LastLaunchedScheduleRun(ctx, "ws-1", "weekly")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, launched.ID, last.ID, "the later failed/skipped runs must not shadow the last launched run")

	newerLaunch := mustInsertRunWithStatus(t, db, "ws-1", "weekly", 400, "launched")
	last, ok, err = db.LastLaunchedScheduleRun(ctx, "ws-1", "weekly")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, newerLaunch.ID, last.ID)
}

func TestInsertScheduleRun_PrunesToTheLimitPerSchedule(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	const total = scheduleRunLimit + 5
	var ids []int64
	for i := range total {
		run := mustInsertRun(t, db, "ws-1", "weekly", int64(i))
		ids = append(ids, run.ID)
	}

	// A run for a different schedule must not be affected by the pruning a
	// weekly insert triggers.
	other := mustInsertRun(t, db, "ws-1", "daily", 1)

	var count int
	require.NoError(t, db.Conn().QueryRowContext(ctx,
		`SELECT count(*) FROM schedule_run WHERE workspace = 'ws-1' AND schedule_id = 'weekly'`).Scan(&count))
	assert.Equal(t, scheduleRunLimit, count)

	runs, err := db.ListScheduleRunsFor(ctx, "ws-1", "weekly", scheduleRunLimit+10)
	require.NoError(t, err)
	require.Len(t, runs, scheduleRunLimit)
	// The newest scheduleRunLimit runs survive; the oldest are gone.
	kept := make(map[int64]bool, len(runs))
	for _, run := range runs {
		kept[run.ID] = true
	}
	for i, id := range ids {
		wantKept := i >= total-scheduleRunLimit
		assert.Equal(t, wantKept, kept[id], "run %d (index %d) retention mismatch", id, i)
	}

	daily, err := db.ListScheduleRunsFor(ctx, "ws-1", "daily", 10)
	require.NoError(t, err)
	require.Len(t, daily, 1, "pruning one schedule must not touch another schedule's runs")
	assert.Equal(t, other.ID, daily[0].ID)
}

func TestDeleteScheduleRuns_RemovesOnlyTheWorkspace(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	mustInsertRun(t, db, "ws-1", "weekly", 100)
	mustInsertRun(t, db, "ws-1", "daily", 200)
	kept := mustInsertRun(t, db, "ws-2", "weekly", 300)

	require.NoError(t, db.DeleteScheduleRuns(ctx, "ws-1"))

	for _, id := range []string{"weekly", "daily"} {
		runs, err := db.ListScheduleRunsFor(ctx, "ws-1", id, 10)
		require.NoError(t, err)
		assert.Empty(t, runs, "schedule %q should have no runs left", id)
	}

	runs, err := db.ListScheduleRunsFor(ctx, "ws-2", "weekly", 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, kept.ID, runs[0].ID)
}

func TestScheduleIDsBySession_NewestRunWinsAndSkipsRunsWithNoChat(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	mustInsertRunWithSession(t, db, "ws-1", "weekly", 100, 11)
	mustInsertRun(t, db, "ws-1", "weekly", 150) // failed to launch, so no chat
	mustInsertRunWithSession(t, db, "ws-1", "daily", 200, 12)
	// The same chat recorded twice keeps whichever schedule ran last.
	mustInsertRunWithSession(t, db, "ws-1", "weekly", 250, 12)
	mustInsertRunWithSession(t, db, "ws-2", "monthly", 300, 13)

	scoped, err := db.ScheduleIDsBySession(ctx, "ws-1")
	require.NoError(t, err)
	assert.Equal(t, map[int64]string{11: "weekly", 12: "weekly"}, scoped)

	all, err := db.AllScheduleIDsBySession(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[int64]string{11: "weekly", 12: "weekly", 13: "monthly"}, all)
}

func mustInsertRunWithSession(t *testing.T, db *DB, workspace, scheduleID string, startedAt, sessionID int64) ScheduleRunRecord {
	t.Helper()
	run, err := db.InsertScheduleRun(t.Context(), ScheduleRunRecord{
		Workspace: workspace, ScheduleID: scheduleID, ScheduleName: scheduleID,
		ScheduledFor: startedAt, StartedAt: startedAt, Reason: "due", Status: "launched",
		SessionID: sessionID, Prompt: "Summarize.",
	})
	require.NoError(t, err)
	return run
}

func mustInsertRun(t *testing.T, db *DB, workspace, scheduleID string, startedAt int64) ScheduleRunRecord {
	t.Helper()
	return mustInsertRunWithStatus(t, db, workspace, scheduleID, startedAt, "launched")
}

func mustInsertRunWithStatus(t *testing.T, db *DB, workspace, scheduleID string, startedAt int64, status string) ScheduleRunRecord {
	t.Helper()
	run, err := db.InsertScheduleRun(t.Context(), ScheduleRunRecord{
		Workspace: workspace, ScheduleID: scheduleID, ScheduleName: scheduleID,
		ScheduledFor: startedAt, StartedAt: startedAt, Reason: "due", Status: status,
		Prompt: "Summarize.",
	})
	require.NoError(t, err)
	return run
}
