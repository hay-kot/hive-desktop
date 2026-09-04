package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// scheduleRunLimit is the number of newest runs InsertScheduleRun keeps per
// (workspace, schedule) after each insert.
const scheduleRunLimit = 200

// ScheduleCursorRecord is how far one workspace schedule has been evaluated.
// Named Record, like JobRecord and NodeRunRecord, because sqlc already emits
// a ScheduleCursor row type from the schedule_cursor table; this is the
// boundary shape callers use. Cron is snapshotted alongside the cursor: a
// re-timed schedule's cron no longer matches the stored one, which is what
// tells the scheduler to treat it as new rather than back-filling
// occurrences under the old cadence.
type ScheduleCursorRecord struct {
	Workspace        string
	ScheduleID       string
	EvaluatedThrough int64 // unix ms
	Cron             string
}

// ScheduleRunRecord is one schedule execution attempt. SessionID is 0 when
// the run launched no chat (a failed or skipped run); the table stores that
// as NULL. Named Record for the same reason as ScheduleCursorRecord.
type ScheduleRunRecord struct {
	ID           int64
	Workspace    string
	ScheduleID   string
	ScheduleName string
	ScheduledFor int64 // unix ms
	StartedAt    int64 // unix ms
	Reason       string
	Missed       int
	Status       string
	SessionID    int64 // 0 when none
	Prompt       string
	Error        string
}

// GetScheduleCursor reads one schedule's cursor. ok reports whether it
// exists.
func (db *DB) GetScheduleCursor(ctx context.Context, workspace, scheduleID string) (ScheduleCursorRecord, bool, error) {
	db = db.Ctx(ctx)
	row, err := db.queries.GetScheduleCursor(ctx, GetScheduleCursorParams{
		Workspace:  workspace,
		ScheduleID: scheduleID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduleCursorRecord{}, false, nil
	}
	if err != nil {
		return ScheduleCursorRecord{}, false, fmt.Errorf("getting schedule cursor for %s/%s: %w", workspace, scheduleID, err)
	}
	return scheduleCursorFromRow(row), true, nil
}

// UpsertScheduleCursor writes a schedule's cursor, replacing any existing
// one for the same (workspace, schedule_id).
func (db *DB) UpsertScheduleCursor(ctx context.Context, cursor ScheduleCursorRecord) error {
	db = db.Ctx(ctx)
	err := db.queries.UpsertScheduleCursor(ctx, UpsertScheduleCursorParams(cursor))
	return wrap(fmt.Sprintf("upserting schedule cursor for %s/%s", cursor.Workspace, cursor.ScheduleID), err)
}

// ListScheduleCursors returns every stored cursor, across every workspace.
func (db *DB) ListScheduleCursors(ctx context.Context) ([]ScheduleCursorRecord, error) {
	db = db.Ctx(ctx)
	rows, err := db.queries.ListScheduleCursors(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing schedule cursors: %w", err)
	}
	out := make([]ScheduleCursorRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, scheduleCursorFromRow(row))
	}
	return out, nil
}

// DeleteScheduleCursor removes one schedule's cursor, e.g. when the manifest
// no longer defines it.
func (db *DB) DeleteScheduleCursor(ctx context.Context, workspace, scheduleID string) error {
	db = db.Ctx(ctx)
	err := db.queries.DeleteScheduleCursor(ctx, DeleteScheduleCursorParams{
		Workspace:  workspace,
		ScheduleID: scheduleID,
	})
	return wrap(fmt.Sprintf("deleting schedule cursor for %s/%s", workspace, scheduleID), err)
}

// InsertScheduleRun persists one run and returns the stored row with its
// assigned id, then prunes that schedule's run history back to
// scheduleRunLimit.
func (db *DB) InsertScheduleRun(ctx context.Context, run ScheduleRunRecord) (ScheduleRunRecord, error) {
	db = db.Ctx(ctx)
	row, err := db.queries.InsertScheduleRun(ctx, InsertScheduleRunParams{
		Workspace:    run.Workspace,
		ScheduleID:   run.ScheduleID,
		ScheduleName: run.ScheduleName,
		ScheduledFor: run.ScheduledFor,
		StartedAt:    run.StartedAt,
		Reason:       run.Reason,
		Missed:       int64(run.Missed),
		Status:       run.Status,
		SessionID:    nullableInt64FromZero(run.SessionID),
		Prompt:       run.Prompt,
		Error:        run.Error,
	})
	if err != nil {
		return ScheduleRunRecord{}, fmt.Errorf("inserting schedule run for %s/%s: %w", run.Workspace, run.ScheduleID, err)
	}

	if err := db.queries.PruneScheduleRuns(ctx, PruneScheduleRunsParams{
		Workspace:  run.Workspace,
		ScheduleID: run.ScheduleID,
		Keep:       scheduleRunLimit,
	}); err != nil {
		return ScheduleRunRecord{}, fmt.Errorf("pruning schedule runs for %s/%s: %w", run.Workspace, run.ScheduleID, err)
	}

	return scheduleRunFromRow(row), nil
}

// ListScheduleRuns returns up to limit of a workspace's runs across every
// schedule, newest first.
func (db *DB) ListScheduleRuns(ctx context.Context, workspace string, limit int) ([]ScheduleRunRecord, error) {
	db = db.Ctx(ctx)
	rows, err := db.queries.ListScheduleRuns(ctx, ListScheduleRunsParams{
		Workspace: workspace,
		Limit:     int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("listing schedule runs for workspace %q: %w", workspace, err)
	}
	return scheduleRunsFromRows(rows), nil
}

// ListScheduleRunsFor returns up to limit of one schedule's runs, newest
// first.
func (db *DB) ListScheduleRunsFor(ctx context.Context, workspace, scheduleID string, limit int) ([]ScheduleRunRecord, error) {
	db = db.Ctx(ctx)
	rows, err := db.queries.ListScheduleRunsForSchedule(ctx, ListScheduleRunsForScheduleParams{
		Workspace:  workspace,
		ScheduleID: scheduleID,
		Limit:      int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("listing schedule runs for %s/%s: %w", workspace, scheduleID, err)
	}
	return scheduleRunsFromRows(rows), nil
}

// LastLaunchedScheduleRun returns the most recent run that actually started
// a chat for a schedule, skipping over failed and skipped attempts. ok
// reports whether such a run exists.
func (db *DB) LastLaunchedScheduleRun(ctx context.Context, workspace, scheduleID string) (ScheduleRunRecord, bool, error) {
	db = db.Ctx(ctx)
	row, err := db.queries.LastLaunchedScheduleRun(ctx, LastLaunchedScheduleRunParams{
		Workspace:  workspace,
		ScheduleID: scheduleID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduleRunRecord{}, false, nil
	}
	if err != nil {
		return ScheduleRunRecord{}, false, fmt.Errorf("getting last launched schedule run for %s/%s: %w", workspace, scheduleID, err)
	}
	return scheduleRunFromRow(row), true, nil
}

// DeleteScheduleRuns removes every run recorded for a workspace. Workspace
// deletion calls this alongside DeleteAgentWorkspaceSessionsByWorkspace.
func (db *DB) DeleteScheduleRuns(ctx context.Context, workspace string) error {
	db = db.Ctx(ctx)
	return wrap("deleting schedule runs by workspace", db.queries.DeleteScheduleRunsByWorkspace(ctx, workspace))
}

// ScheduleIDsBySession maps each chat a schedule started to that schedule's
// id, for one workspace. A session listed twice keeps its newest run's
// schedule. Returning the map rather than the rows is what keeps that
// tie-break in one place: every caller wants the lookup, none wants the runs.
func (db *DB) ScheduleIDsBySession(ctx context.Context, workspace string) (map[int64]string, error) {
	db = db.Ctx(ctx)
	rows, err := db.queries.ListScheduleRunSessions(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("listing scheduled chats for workspace %q: %w", workspace, err)
	}
	out := make(map[int64]string, len(rows))
	for _, row := range rows {
		out[row.SessionID.Int64] = row.ScheduleID
	}
	return out, nil
}

// AllScheduleIDsBySession is ScheduleIDsBySession across every workspace.
func (db *DB) AllScheduleIDsBySession(ctx context.Context) (map[int64]string, error) {
	db = db.Ctx(ctx)
	rows, err := db.queries.ListAllScheduleRunSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing scheduled chats: %w", err)
	}
	out := make(map[int64]string, len(rows))
	for _, row := range rows {
		out[row.SessionID.Int64] = row.ScheduleID
	}
	return out, nil
}

// scheduleCursorFromRow adapts a generated row to the domain record. The two
// are field-identical today; if a future column makes them diverge this
// stops compiling and becomes an explicit mapping (activity_event.go follows
// the same pattern).
func scheduleCursorFromRow(row ScheduleCursor) ScheduleCursorRecord {
	return ScheduleCursorRecord(row)
}

func scheduleRunsFromRows(rows []ScheduleRun) []ScheduleRunRecord {
	out := make([]ScheduleRunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, scheduleRunFromRow(row))
	}
	return out
}

func scheduleRunFromRow(row ScheduleRun) ScheduleRunRecord {
	var sessionID int64
	if row.SessionID.Valid {
		sessionID = row.SessionID.Int64
	}
	return ScheduleRunRecord{
		ID:           row.ID,
		Workspace:    row.Workspace,
		ScheduleID:   row.ScheduleID,
		ScheduleName: row.ScheduleName,
		ScheduledFor: row.ScheduledFor,
		StartedAt:    row.StartedAt,
		Reason:       row.Reason,
		Missed:       int(row.Missed),
		Status:       row.Status,
		SessionID:    sessionID,
		Prompt:       row.Prompt,
		Error:        row.Error,
	}
}

// nullableInt64FromZero treats 0 as "no value": ScheduleRunRecord.SessionID
// uses 0 rather than a pointer to mean none, unlike JobRecord.CommandID
// (which is a *int64) — a schedule run's session id is never legitimately 0.
func nullableInt64FromZero(value int64) sql.NullInt64 {
	if value == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value, Valid: true}
}
