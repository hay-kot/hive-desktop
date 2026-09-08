package stores

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

func enqueueTestCommand(t *testing.T, st *Stores, actionID, key string) {
	t.Helper()
	require.NoError(t, st.EventLog.Commit(t.Context(), models.CommitBatch{
		Consumer:   "flow-" + actionID + "-" + key,
		UpToOffset: 1,
		Outputs: []models.Output{
			{
				Sink:          models.Sink{Kind: models.SinkKindAction, TargetID: actionID},
				OccurrenceKey: key,
				Payload:       []byte(`{"v":1}`),
			},
		},
	}))
}

func TestRecoverInterruptedOutputCommands_JoinsTransaction(t *testing.T) {
	st, db := openTestStores(t)
	ctx := t.Context()

	command, created, err := st.OutputCommands.Confirm(ctx, "review", "item-1", []byte(`{}`), models.ItemRef{})
	require.NoError(t, err)
	require.True(t, created)
	job, err := st.Jobs.Insert(ctx, JobCreate{Status: "queued", Label: "Review"})
	require.NoError(t, err)
	_, err = st.Jobs.SetRunning(ctx, job.ID, "Running", command.ID)
	require.NoError(t, err)

	require.NoError(t, db.RecoverInterruptedOutputCommands(ctx))

	command, err = st.OutputCommands.Get(ctx, command.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", command.Status)
	jobs, err := st.Jobs.List(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, "failed", jobs[0].Status)
	rows, err := st.OutputCommands.ListRunnableAfter(ctx, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestListRunnableOutputCommands_ReturnsOldestIDFirst(t *testing.T) {
	st, _ := openTestStores(t)
	ctx := t.Context()

	enqueueTestCommand(t, st, "action-a", "k1")
	enqueueTestCommand(t, st, "action-a", "k2")

	rows, err := st.OutputCommands.ListRunnableAfter(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "k1", rows[0].Key)
	assert.Equal(t, "k2", rows[1].Key)
	assert.Equal(t, "pending", rows[0].Status)
	assert.Equal(t, int64(0), rows[0].Attempts)
	assert.Empty(t, rows[0].LastError)
}

func TestListRunnableOutputCommands_RespectsLimit(t *testing.T) {
	st, _ := openTestStores(t)
	ctx := t.Context()

	enqueueTestCommand(t, st, "action-a", "k1")
	enqueueTestCommand(t, st, "action-a", "k2")

	rows, err := st.OutputCommands.ListRunnableAfter(ctx, 0, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "k1", rows[0].Key)
}

// TestConfirmOutputCommandDeduplicatesExistingCommand guards the dedup
// fallback behind UNIQUE (action_id, key): a sql.ErrNoRows from the guarded
// UPDATE falls back to the latest existing command with created=false, which
// is what stops an already-run action re-firing.
func TestConfirmOutputCommandDeduplicatesExistingCommand(t *testing.T) {
	st, _ := openTestStores(t)
	ctx := t.Context()

	enqueueTestCommand(t, st, "action-a", "k1")
	row, created, err := st.OutputCommands.Confirm(ctx, "action-a", "k1", []byte(`{"v":2}`), models.ItemRef{})
	require.NoError(t, err)
	assert.True(t, created, "confirmation atomically claims the pending command")
	assert.Equal(t, "running", row.Status)
	assert.JSONEq(t, `{"v":1}`, string(row.Payload), "the queued payload remains authoritative")
	existing, created, err := st.OutputCommands.Confirm(ctx, "action-a", "k1", []byte(`{"v":3}`), models.ItemRef{})
	require.NoError(t, err)
	assert.False(t, created, "a running command cannot be claimed twice")
	assert.Equal(t, row.ID, existing.ID, "the existing command is returned for confirmation UX")
	require.NoError(t, st.OutputCommands.MarkDone(ctx, row.ID))

	rerun, err := st.OutputCommands.Rerun(ctx, "action-a", "k1", []byte(`{"v":4}`), models.ItemRef{})
	require.NoError(t, err)
	assert.NotEqual(t, row.ID, rerun.ID)
	assert.True(t, rerun.IsRerun)
	assert.JSONEq(t, `{"v":4}`, string(rerun.Payload))
}

// TestRerunOutputCommandRequiresPriorRun guards RerunOutputCommand's
// deliberate classification of "no completed prior run" as not-found: the
// message is now the store's generic NotFoundError text, but the
// classification -- IsNotFound true, still unwraps to sql.ErrNoRows -- is
// the meaning that has to survive.
func TestRerunOutputCommandRequiresPriorRun(t *testing.T) {
	st, _ := openTestStores(t)

	_, err := st.OutputCommands.Rerun(t.Context(), "action-a", "missing", []byte(`{}`), models.ItemRef{})
	require.Error(t, err)
	assert.True(t, IsNotFound(err))
}

func TestRerunOutputCommandRejectsActivePriorRun(t *testing.T) {
	st, _ := openTestStores(t)
	enqueueTestCommand(t, st, "action-a", "active")

	_, err := st.OutputCommands.Rerun(t.Context(), "action-a", "active", []byte(`{}`), models.ItemRef{})
	require.Error(t, err)
	assert.True(t, IsNotFound(err))
}

func TestMarkOutputCommandDone_ExcludesFromRunnable(t *testing.T) {
	st, _ := openTestStores(t)
	ctx := t.Context()

	enqueueTestCommand(t, st, "action-a", "k1")
	rows, err := st.OutputCommands.ListRunnableAfter(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NoError(t, st.OutputCommands.MarkDone(ctx, rows[0].ID))

	rows, err = st.OutputCommands.ListRunnableAfter(ctx, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestRetryOutputCommand_IncrementsAttemptsAndStaysRunnable(t *testing.T) {
	st, _ := openTestStores(t)
	ctx := t.Context()

	enqueueTestCommand(t, st, "action-a", "k1")
	rows, err := st.OutputCommands.ListRunnableAfter(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	id := rows[0].ID

	require.NoError(t, st.OutputCommands.Retry(ctx, id, "boom"))

	rows, err = st.OutputCommands.ListRunnableAfter(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1, "retried command stays runnable")
	assert.Equal(t, int64(1), rows[0].Attempts)
	assert.Equal(t, "boom", rows[0].LastError)

	require.NoError(t, st.OutputCommands.Retry(ctx, id, "boom again"))
	rows, err = st.OutputCommands.ListRunnableAfter(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(2), rows[0].Attempts)
}

func TestMarkOutputCommandDoneClearsPreviousFailure(t *testing.T) {
	st, _ := openTestStores(t)
	enqueueTestCommand(t, st, "action-a", "k1")
	rows, err := st.OutputCommands.ListRunnableAfter(t.Context(), 0, 1)
	require.NoError(t, err)
	require.NoError(t, st.OutputCommands.Retry(t.Context(), rows[0].ID, "first failure", "old stdout", "old stderr"))
	require.NoError(t, st.OutputCommands.MarkDone(t.Context(), rows[0].ID, `{"message":{"topic":"agent.inbox"}}`, "new stdout", ""))

	row, err := st.OutputCommands.Get(t.Context(), rows[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "done", row.Status)
	assert.Empty(t, row.LastError, "successful retry must not retain stale failure")
	assert.JSONEq(t, `{"message":{"topic":"agent.inbox"}}`, row.ResultJSON)
	assert.Equal(t, "new stdout", row.Stdout)
	assert.Empty(t, row.Stderr)
}

func TestOutputCommandPersistenceBoundsStreams(t *testing.T) {
	st, _ := openTestStores(t)
	enqueueTestCommand(t, st, "action-a", "k1")
	rows, err := st.OutputCommands.ListRunnableAfter(t.Context(), 0, 1)
	require.NoError(t, err)
	noisy := strings.Repeat("x", maxOutputCommandStreamBytes+1)
	require.NoError(t, st.OutputCommands.MarkFailed(t.Context(), rows[0].ID, "failed", noisy, noisy))

	row, err := st.OutputCommands.Get(t.Context(), rows[0].ID)
	require.NoError(t, err)
	assert.Len(t, row.Stdout, maxOutputCommandStreamBytes)
	assert.Len(t, row.Stderr, maxOutputCommandStreamBytes)
	assert.True(t, strings.HasSuffix(row.Stdout, outputCommandTruncatedMarker))
	assert.True(t, strings.HasSuffix(row.Stderr, outputCommandTruncatedMarker))
}

func TestExecutionResultAndLogsPersistAcrossReopenBeforeDone(t *testing.T) {
	dir := t.TempDir()
	db, err := queries.Open(t.Context(), dir, queries.DefaultOpenOptions())
	require.NoError(t, err)
	st := New(db, Options{})
	enqueueTestCommand(t, st, "action-a", "k1")
	rows, err := st.OutputCommands.ListRunnableAfter(t.Context(), 0, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NoError(t, st.OutputCommands.MarkDone(t.Context(), rows[0].ID, `{"message":{"topic":"agent.inbox"}}`, "stdout", "stderr"))
	require.NoError(t, db.Close())

	db, err = queries.Open(t.Context(), dir, queries.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	st = New(db, Options{})
	row, err := st.OutputCommands.Get(t.Context(), rows[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "done", row.Status)
	assert.JSONEq(t, `{"message":{"topic":"agent.inbox"}}`, row.ResultJSON)
	assert.Equal(t, "stdout", row.Stdout)
	assert.Equal(t, "stderr", row.Stderr)
}

func TestMarkOutputCommandFailed_ExcludesFromRunnable(t *testing.T) {
	st, db := openTestStores(t)
	ctx := t.Context()

	enqueueTestCommand(t, st, "action-a", "k1")
	rows, err := st.OutputCommands.ListRunnableAfter(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	id := rows[0].ID

	require.NoError(t, st.OutputCommands.MarkFailed(ctx, id, "gave up"))

	rows, err = st.OutputCommands.ListRunnableAfter(ctx, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, rows)

	var status string
	var lastErr string
	require.NoError(t, db.Conn().QueryRowContext(ctx,
		`SELECT status, last_error FROM output_command WHERE id = ?`, id,
	).Scan(&status, &lastErr))
	assert.Equal(t, "failed", status)
	assert.Equal(t, "gave up", lastErr)
}

func TestOutputCommandStore_CountNonterminalForAction(t *testing.T) {
	st, _ := openTestStores(t)
	ctx := t.Context()

	enqueueTestCommand(t, st, "action-a", "k1")
	enqueueTestCommand(t, st, "action-a", "k2")
	enqueueTestCommand(t, st, "action-b", "k3")

	count, err := st.OutputCommands.CountNonterminalForAction(ctx, "action-a")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	rows, err := st.OutputCommands.ListRunnableAfter(ctx, 0, 10)
	require.NoError(t, err)
	for _, row := range rows {
		if row.ActionID == "action-a" {
			require.NoError(t, st.OutputCommands.MarkDone(ctx, row.ID))
		}
	}

	count, err = st.OutputCommands.CountNonterminalForAction(ctx, "action-a")
	require.NoError(t, err)
	assert.Zero(t, count, "a terminal command no longer counts")
}
