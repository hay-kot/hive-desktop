package queries

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
)

func (db *DB) ListRunnableOutputCommandsAfter(ctx context.Context, afterID int64, limit int) ([]OutputCommand, error) {
	rows, err := db.Queries.ListRunnableOutputCommandsAfter(ctx, ListRunnableOutputCommandsAfterParams{ID: afterID, Limit: int64(limit)})
	return rows, wrap("listing runnable output commands", err)
}

func (db *DB) ConfirmOutputCommand(ctx context.Context, actionID, key string, payload []byte, ref models.ItemRef) (OutputCommand, bool, error) {
	row, err := db.Queries.ConfirmOutputCommand(ctx, ConfirmOutputCommandParams{
		ActionID: actionID, Key: key, Payload: payload, CreatedAt: time.Now().UnixMilli(),
		ProfileID: ref.ProfileID, SourceKind: ref.SourceKind, SourceScope: ref.SourceScope, ExternalID: ref.ExternalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		existing, lookupErr := db.GetLatestOutputCommandForAction(ctx, GetLatestOutputCommandForActionParams{ActionID: actionID, Key: key})
		return existing, false, wrap("getting existing output command", lookupErr)
	}
	return row, err == nil, wrap("confirming output command", err)
}

func (db *DB) RerunOutputCommand(ctx context.Context, actionID, key string, payload []byte, ref models.ItemRef) (OutputCommand, error) {
	row, err := db.Queries.RerunOutputCommand(ctx, RerunOutputCommandParams{
		ActionID: actionID, Key: key, Payload: payload, CreatedAt: time.Now().UnixMilli(),
		ProfileID: ref.ProfileID, SourceKind: ref.SourceKind, SourceScope: ref.SourceScope, ExternalID: ref.ExternalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		// %w deliberately: the boundary classifies this by unwrapping, and
		// without it a rerun-without-a-prior-run reports as an internal
		// failure rather than the invalid request it is.
		return OutputCommand{}, fmt.Errorf("action %q cannot rerun for %q without a completed prior run: %w", actionID, key, err)
	}
	return row, wrap("rerunning output command", err)
}

// ItemRef is the inbox item this command was routed from. A command with no
// inbox origin — a notify command, or an action invoked from a surface that
// has no item behind it — returns a zero ref, which reads as not Known.
func (c OutputCommand) ItemRef() models.ItemRef {
	return models.ItemRef{ProfileID: c.ProfileID, SourceKind: c.SourceKind, SourceScope: c.SourceScope, ExternalID: c.ExternalID}
}

func (db *DB) OutputCommand(ctx context.Context, id int64) (OutputCommand, error) {
	row, err := db.GetOutputCommand(ctx, id)
	return row, wrap("getting output command", err)
}

func (db *DB) MarkOutputCommandDone(ctx context.Context, id int64, values ...string) error {
	var resultJSON, stdout, stderr string
	if len(values) > 0 {
		resultJSON = values[0]
	}
	if len(values) > 1 {
		stdout = values[1]
	}
	if len(values) > 2 {
		stderr = values[2]
	}

	return wrap("marking output command done", db.Queries.MarkOutputCommandDone(ctx, MarkOutputCommandDoneParams{ID: id, ResultJson: null(resultJSON), Stdout: null(boundOutputCommandStream(stdout)), Stderr: null(boundOutputCommandStream(stderr))}))
}

func (db *DB) RetryOutputCommand(ctx context.Context, id int64, lastErr string, values ...string) error {
	var stdout, stderr string
	if len(values) > 0 {
		stdout = values[0]
	}
	if len(values) > 1 {
		stderr = values[1]
	}

	return wrap("recording output command retry", db.Queries.RetryOutputCommand(ctx, RetryOutputCommandParams{ID: id, LastError: null(lastErr), Stdout: null(boundOutputCommandStream(stdout)), Stderr: null(boundOutputCommandStream(stderr))}))
}

func (db *DB) MarkOutputCommandFailed(ctx context.Context, id int64, lastErr string, values ...string) error {
	var stdout, stderr string
	if len(values) > 0 {
		stdout = values[0]
	}
	if len(values) > 1 {
		stderr = values[1]
	}

	return wrap("marking output command failed", db.Queries.MarkOutputCommandFailed(ctx, MarkOutputCommandFailedParams{ID: id, LastError: null(lastErr), Stdout: null(boundOutputCommandStream(stdout)), Stderr: null(boundOutputCommandStream(stderr))}))
}

const (
	maxOutputCommandStreamBytes  = 64 * 1024
	outputCommandTruncatedMarker = "\n... (truncated)"
)

// boundOutputCommandStream is a persistence boundary: executors and tests
// cannot make durable command diagnostics exceed the per-stream cap.
func boundOutputCommandStream(stream string) string {
	if len(stream) <= maxOutputCommandStreamBytes {
		return stream
	}
	return stream[:maxOutputCommandStreamBytes-len(outputCommandTruncatedMarker)] + outputCommandTruncatedMarker
}

func null(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }
func wrap(msg string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

const interruptedOutputCommandError = "interrupted: application stopped while action was running"

// RecoverInterruptedOutputCommands makes stale explicit invocations and their
// linked jobs terminal. It also fails unlinked queued jobs, which in v1 can only
// be left by a crash between Begin and Running. A running command may already
// have performed its side effect before a crash, so retrying it in the
// background would be unauthorized and unsafe.
func (db *DB) RecoverInterruptedOutputCommands(ctx context.Context) error {
	return db.WithinTx(ctx, func(ctx context.Context, tx *DB) error {
		if _, err := tx.querier().ExecContext(ctx, `
		UPDATE job
		SET status = 'failed', step = 'Failed', error = ?, updated_at = ?
		WHERE (status = 'queued' AND command_id IS NULL)
			OR (status = 'running'
				AND command_id IN (SELECT id FROM output_command WHERE status = 'running'))`,
			interruptedOutputCommandError, time.Now().UnixMilli()); err != nil {
			return wrap("recovering interrupted jobs", err)
		}
		if _, err := tx.querier().ExecContext(ctx, `
		UPDATE output_command
		SET status = 'failed', attempts = attempts + 1, last_error = ?
		WHERE status = 'running'`, interruptedOutputCommandError); err != nil {
			return wrap("recovering interrupted output commands", err)
		}
		return nil
	})
}
