package stores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

const (
	maxOutputCommandStreamBytes  = 64 * 1024
	outputCommandTruncatedMarker = "\n... (truncated)"
)

// OutputCommandStore owns output_command: the queue of enqueued and
// completed action invocations the dispatch worker drains.
type OutputCommandStore struct {
	q   *queries.DB
	now func() time.Time
}

func NewOutputCommandStore(q *queries.DB, opts Options) *OutputCommandStore {
	return &OutputCommandStore{q: q, now: opts.Now}
}

// ListRunnableAfter returns runnable commands with id > afterID, oldest
// first, bounded by limit -- a worker's paged scan.
func (s *OutputCommandStore) ListRunnableAfter(ctx context.Context, afterID int64, limit int) ([]OutputCommand, error) {
	rows, err := s.q.Ctx(ctx).ListRunnableOutputCommandsAfter(ctx, queries.ListRunnableOutputCommandsAfterParams{ID: afterID, Limit: int64(limit)})
	if err != nil {
		return nil, wrap("listing runnable output commands", err)
	}
	out := make([]OutputCommand, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapOutputCommandFromDB(row))
	}
	return out, nil
}

// Enqueue records a flow-produced action or notify invocation, deduplicated
// on (actionID, key) by the unique index a replayed commit relies on so the
// same batch replayed twice never fires an action twice. Used by
// EventLogStore.Commit.
func (s *OutputCommandStore) Enqueue(ctx context.Context, actionID, key string, payload []byte, createdAt int64, ref models.ItemRef) error {
	return wrap("enqueuing output command", s.q.Ctx(ctx).EnqueueOutputCommand(ctx, queries.EnqueueOutputCommandParams{
		ActionID: actionID, Key: key, Payload: payload, CreatedAt: createdAt,
		ProfileID: ref.ProfileID, SourceKind: ref.SourceKind, SourceScope: ref.SourceScope, ExternalID: ref.ExternalID,
	}))
}

// Confirm claims a queued command or enqueues a fresh one for an explicit
// detail-pane invocation. When the action is already terminal for this key,
// the sql.ErrNoRows the guarded UPDATE produces falls back to the latest
// existing command and reports created=false -- the dedup behind
// UNIQUE (action_id, key) that stops an already-run action re-firing.
func (s *OutputCommandStore) Confirm(ctx context.Context, actionID, key string, payload []byte, ref models.ItemRef) (OutputCommand, bool, error) {
	q := s.q.Ctx(ctx)
	row, err := q.ConfirmOutputCommand(ctx, queries.ConfirmOutputCommandParams{
		ActionID: actionID, Key: key, Payload: payload, CreatedAt: s.now().UnixMilli(),
		ProfileID: ref.ProfileID, SourceKind: ref.SourceKind, SourceScope: ref.SourceScope, ExternalID: ref.ExternalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		existing, lookupErr := q.GetLatestOutputCommandForAction(ctx, queries.GetLatestOutputCommandForActionParams{ActionID: actionID, Key: key})
		return mapOutputCommandFromDB(existing), false, wrap("getting existing output command", lookupErr)
	}
	if err != nil {
		return OutputCommand{}, false, wrap("confirming output command", err)
	}
	return mapOutputCommandFromDB(row), true, nil
}

// Rerun creates a separate command repeating a prior completed run, so its
// diagnostics and Activity links stay intact. sql.ErrNoRows -- no completed
// prior run to repeat -- becomes a NotFoundError: this one *should* go
// through the generic transform, unlike the revision-guarded writes, because
// "nothing to rerun" really is a missing row from the caller's point of view.
func (s *OutputCommandStore) Rerun(ctx context.Context, actionID, key string, payload []byte, ref models.ItemRef) (OutputCommand, error) {
	row, err := s.q.Ctx(ctx).RerunOutputCommand(ctx, queries.RerunOutputCommandParams{
		ActionID: actionID, Key: key, Payload: payload, CreatedAt: s.now().UnixMilli(),
		ProfileID: ref.ProfileID, SourceKind: ref.SourceKind, SourceScope: ref.SourceScope, ExternalID: ref.ExternalID,
	})
	if err != nil {
		return OutputCommand{}, errTransformQueryOne("output_command", fmt.Sprintf("%s/%s", actionID, key), err)
	}
	return mapOutputCommandFromDB(row), nil
}

// Get reads one output_command row by id.
func (s *OutputCommandStore) Get(ctx context.Context, id int64) (OutputCommand, error) {
	row, err := s.q.Ctx(ctx).GetOutputCommand(ctx, id)
	if err != nil {
		return OutputCommand{}, errTransformQueryOne("output_command", fmt.Sprint(id), err)
	}
	return mapOutputCommandFromDB(row), nil
}

// MarkDone records a successful terminal result. values are (result JSON,
// stdout, stderr), each optional and bounded.
func (s *OutputCommandStore) MarkDone(ctx context.Context, id int64, values ...string) error {
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
	return wrap("marking output command done", s.q.Ctx(ctx).MarkOutputCommandDone(ctx, queries.MarkOutputCommandDoneParams{
		ID: id, ResultJson: null(resultJSON), Stdout: null(boundOutputCommandStream(stdout)), Stderr: null(boundOutputCommandStream(stderr)),
	}))
}

// MarkFailed records a permanent terminal failure. values are (stdout,
// stderr), each optional and bounded.
func (s *OutputCommandStore) MarkFailed(ctx context.Context, id int64, lastErr string, values ...string) error {
	var stdout, stderr string
	if len(values) > 0 {
		stdout = values[0]
	}
	if len(values) > 1 {
		stderr = values[1]
	}
	return wrap("marking output command failed", s.q.Ctx(ctx).MarkOutputCommandFailed(ctx, queries.MarkOutputCommandFailedParams{
		ID: id, LastError: null(lastErr), Stdout: null(boundOutputCommandStream(stdout)), Stderr: null(boundOutputCommandStream(stderr)),
	}))
}

// Retry records a non-terminal failure so the worker retries it later.
// values are (stdout, stderr), each optional and bounded.
func (s *OutputCommandStore) Retry(ctx context.Context, id int64, lastErr string, values ...string) error {
	var stdout, stderr string
	if len(values) > 0 {
		stdout = values[0]
	}
	if len(values) > 1 {
		stderr = values[1]
	}
	return wrap("recording output command retry", s.q.Ctx(ctx).RetryOutputCommand(ctx, queries.RetryOutputCommandParams{
		ID: id, LastError: null(lastErr), Stdout: null(boundOutputCommandStream(stdout)), Stderr: null(boundOutputCommandStream(stderr)),
	}))
}

// RecoverInterrupted makes stale explicit invocations and their linked jobs
// terminal. It also fails unlinked queued jobs, which in v1 can only be left
// by a crash between Begin and Running. A running command may already have
// performed its side effect before a crash, so retrying it in the
// background would be unauthorized and unsafe.
//
// This stays a queries.DB method (RecoverInterruptedOutputCommands) rather
// than moving its body here: it writes job rows too, and Open calls it
// before any store exists. The store delegates so callers reach it as an
// OutputCommandStore verb either way.
func (s *OutputCommandStore) RecoverInterrupted(ctx context.Context) error {
	return s.q.Ctx(ctx).RecoverInterruptedOutputCommands(ctx)
}

// CountNonterminalForAction counts an action's pending/running commands, for
// "is anything still using this action?" usage checks.
func (s *OutputCommandStore) CountNonterminalForAction(ctx context.Context, actionID string) (int64, error) {
	count, err := s.q.Ctx(ctx).CountNonterminalCommandsForAction(ctx, actionID)
	return count, wrap("counting nonterminal output commands", err)
}

// boundOutputCommandStream is a persistence boundary: executors and tests
// cannot make durable command diagnostics exceed the per-stream cap.
func boundOutputCommandStream(stream string) string {
	if len(stream) <= maxOutputCommandStreamBytes {
		return stream
	}
	return stream[:maxOutputCommandStreamBytes-len(outputCommandTruncatedMarker)] + outputCommandTruncatedMarker
}
