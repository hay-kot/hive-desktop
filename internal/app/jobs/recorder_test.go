package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/store"
)

func openJobsTestDB(t *testing.T) *store.DB {
	t.Helper()
	database, err := store.Open(t.Context(), t.TempDir(), store.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestStore_RecordsLifecycleLabelsAndEmits(t *testing.T) {
	database := openJobsTestDB(t)
	ctx := t.Context()
	now := time.UnixMilli(1_000)
	var emitted []int64
	jobStore := NewStore(database, Options{
		Now:  func() time.Time { return now },
		Emit: func(id int64) { emitted = append(emitted, id) },
	})

	id := jobStore.Begin(ctx, "Review PR", "review", "pr-1")
	require.Positive(t, id)
	now = time.UnixMilli(2_000)
	jobStore.Running(ctx, id, 44)
	assert.Equal(t, id, jobStore.Resume(ctx, 44))
	now = time.UnixMilli(3_000)
	jobStore.Done(ctx, id)
	assert.Zero(t, jobStore.Resume(ctx, 44))
	assert.Equal(t, []int64{id, id, id}, emitted)

	rows, err := jobStore.List(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	job := rows[0]
	assert.Equal(t, JobStatusDone, job.Status)
	assert.Equal(t, "Completed", job.Step)
	assert.Equal(t, int64(1_000), job.CreatedAt)
	assert.Equal(t, int64(3_000), job.UpdatedAt)
	require.NotNil(t, job.CommandID)
	assert.Equal(t, int64(44), *job.CommandID)

	failedID := jobStore.Begin(ctx, "Deploy", "deploy", "pr-2")
	jobStore.Running(ctx, failedID, 45)
	jobStore.Fail(ctx, failedID, "boom")
	rows, err = jobStore.List(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, JobStatusFailed, rows[0].Status)
	assert.Equal(t, "Failed", rows[0].Step)
	assert.Equal(t, "boom", rows[0].Error)
	require.NotNil(t, rows[0].CommandID)
	assert.Equal(t, int64(45), *rows[0].CommandID)
	assert.Len(t, emitted, 6)
}

func TestStepFor(t *testing.T) {
	tests := []struct {
		status JobStatus
		want   string
	}{
		{JobStatusQueued, "Queued"},
		{JobStatusRunning, "Running…"},
		{JobStatusDone, "Completed"},
		{JobStatusFailed, "Failed"},
	}
	for _, test := range tests {
		t.Run(test.status.String(), func(t *testing.T) {
			assert.Equal(t, test.want, stepFor(test.status))
		})
	}
}

func TestStore_ListActiveUsesBackendClockWindow(t *testing.T) {
	database := openJobsTestDB(t)
	ctx := t.Context()
	now := time.UnixMilli(10_000)
	jobStore := NewStore(database, Options{Now: func() time.Time { return now }})

	_, err := database.InsertJob(ctx, store.JobRecord{
		CreatedAt: 1, UpdatedAt: now.Add(-DefaultLingerWindow).UnixMilli(), Status: "done", Label: "Boundary",
	})
	require.NoError(t, err)
	_, err = database.InsertJob(ctx, store.JobRecord{
		CreatedAt: 2, UpdatedAt: now.Add(-DefaultLingerWindow - time.Millisecond).UnixMilli(), Status: "failed", Label: "Outside",
	})
	require.NoError(t, err)
	queued, err := database.InsertJob(ctx, store.JobRecord{
		CreatedAt: 3, UpdatedAt: 1, Status: "queued", Label: "Queued",
	})
	require.NoError(t, err)

	active, err := jobStore.ListActive(ctx, DefaultLingerWindow)
	require.NoError(t, err)
	require.Len(t, active, 2)
	assert.Equal(t, queued.ID, active[0].ID)
	assert.Equal(t, "Boundary", active[1].Label)
}

func TestStore_BeginFailureAndZeroTransitionsAreNoOps(t *testing.T) {
	database := openJobsTestDB(t)
	require.NoError(t, database.Close())
	jobStore := NewStore(database, Options{})
	id := jobStore.Begin(t.Context(), "Review", "review", "pr-1")
	assert.Zero(t, id)
	assert.NotPanics(t, func() {
		jobStore.Running(t.Context(), 0, 1)
		jobStore.Done(t.Context(), 0)
		jobStore.Fail(t.Context(), 0, "ignored")
	})
}
