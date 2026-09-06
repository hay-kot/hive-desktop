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

type ScheduleCursorRecord struct {
	Workspace        string
	ScheduleID       string
	EvaluatedThrough int64 // unix ms
	Cron             string
}

// ScheduleRunRecord.SessionID is 0 when the run launched no chat; the table
// stores that as NULL.
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

func (db *DB) UpsertScheduleCursor(ctx context.Context, cursor ScheduleCursorRecord) error {
	db = db.Ctx(ctx)
	err := db.queries.UpsertScheduleCursor(ctx, UpsertScheduleCursorParams(cursor))
	return wrap(fmt.Sprintf("upserting schedule cursor for %s/%s", cursor.Workspace, cursor.ScheduleID), err)
}

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

func (db *DB) DeleteScheduleCursor(ctx context.Context, workspace, scheduleID string) error {
	db = db.Ctx(ctx)
	err := db.queries.DeleteScheduleCursor(ctx, DeleteScheduleCursorParams{
		Workspace:  workspace,
		ScheduleID: scheduleID,
	})
	return wrap(fmt.Sprintf("deleting schedule cursor for %s/%s", workspace, scheduleID), err)
}

func (db *DB) DeleteScheduleCursors(ctx context.Context, workspace string) error {
	db = db.Ctx(ctx)
	return wrap("deleting schedule cursors by workspace", db.queries.DeleteScheduleCursorsByWorkspace(ctx, workspace))
}

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

func (db *DB) DeleteScheduleRuns(ctx context.Context, workspace string) error {
	db = db.Ctx(ctx)
	return wrap("deleting schedule runs by workspace", db.queries.DeleteScheduleRunsByWorkspace(ctx, workspace))
}

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

func nullableInt64FromZero(value int64) sql.NullInt64 {
	if value == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value, Valid: true}
}
