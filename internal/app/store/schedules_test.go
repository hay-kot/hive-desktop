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

func TestGetAgentWorkspaceSessionByEndToken(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()

	minted, err := db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
		Workspace: "ws-1", Name: "s1", Agent: "claude", AgentSessionID: "a", EndToken: "tok-1",
	})
	require.NoError(t, err)
	// A row from before the column existed carries no token.
	_, err = db.CreateAgentWorkspaceSession(ctx, AgentWorkspaceSession{
		Workspace: "ws-1", Name: "s2", Agent: "claude", AgentSessionID: "b",
	})
	require.NoError(t, err)

	found, ok, err := db.GetAgentWorkspaceSessionByEndToken(ctx, "tok-1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, minted.ID, found.ID)

	_, ok, err = db.GetAgentWorkspaceSessionByEndToken(ctx, "tok-2")
	require.NoError(t, err)
	assert.False(t, ok)

	_, ok, err = db.GetAgentWorkspaceSessionByEndToken(ctx, "")
	require.NoError(t, err)
	assert.False(t, ok, "a blank bearer must not match a row that has no token")
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

func TestInsertScheduleRun_RoundTripsWithTheAssignedID(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		sessionID int64
		wantNull  bool
	}{
		{name: "a launched run keeps its chat id", status: "launched", sessionID: 7},
		{name: "a run with no chat stores NULL, not 0", status: "skipped", wantNull: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestDB(t)
			ctx := t.Context()

			run, err := db.InsertScheduleRun(ctx, ScheduleRunRecord{
				Workspace: "ws-1", ScheduleID: "weekly", ScheduleName: "Weekly summary",
				ScheduledFor: 100, StartedAt: 100, Reason: "due", Status: tt.status,
				SessionID: tt.sessionID, Prompt: "Summarize.",
			})
			require.NoError(t, err)
			assert.NotZero(t, run.ID)
			assert.Equal(t, tt.sessionID, run.SessionID)

			stored, err := db.ListScheduleRunsFor(ctx, "ws-1", "weekly", 10)
			require.NoError(t, err)
			require.Len(t, stored, 1)
			assert.Equal(t, run, stored[0])

			var raw any
			require.NoError(t, db.Conn().QueryRowContext(ctx, `SELECT session_id FROM schedule_run WHERE id = ?`, run.ID).Scan(&raw))
			if tt.wantNull {
				assert.Nil(t, raw, "session 0 must not read back as a chat id")
			} else {
				assert.NotNil(t, raw)
			}
		})
	}
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
