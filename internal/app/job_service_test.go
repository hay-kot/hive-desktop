package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
	"github.com/hay-kot/hive-desktop/internal/app/data/stores"
	"github.com/hay-kot/hive-desktop/internal/app/jobs"
)

func newTestJobService(t *testing.T) *JobService {
	t.Helper()
	db, err := queries.Open(t.Context(), t.TempDir(), queries.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return newJobService(stores.New(db, stores.Options{}).Jobs, nil)
}

func TestJobService_ListAndListActive(t *testing.T) {
	db, err := queries.Open(t.Context(), t.TempDir(), queries.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	st := stores.New(db, stores.Options{Now: func() time.Time { return now }})
	service := newJobService(st.Jobs, nil)
	ctx := t.Context()

	outside, err := db.InsertJob(ctx, queries.InsertJobParams{
		CreatedAt: now.Add(-time.Minute).UnixMilli(), UpdatedAt: now.Add(-jobs.DefaultLingerWindow - time.Millisecond).UnixMilli(),
		Status: "done", Label: "Outside", Step: "Completed",
	})
	require.NoError(t, err)
	inside, err := db.InsertJob(ctx, queries.InsertJobParams{
		CreatedAt: now.Add(-time.Minute).UnixMilli(), UpdatedAt: now.Add(-jobs.DefaultLingerWindow + time.Millisecond).UnixMilli(),
		Status: "failed", Label: "Inside", Step: "Failed",
	})
	require.NoError(t, err)
	queued, err := db.InsertJob(ctx, queries.InsertJobParams{
		CreatedAt: now.Add(-time.Hour).UnixMilli(), UpdatedAt: now.Add(-time.Hour).UnixMilli(),
		Status: "queued", Label: "Queued", Step: "Queued",
	})
	require.NoError(t, err)

	active, err := service.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, active, 2)
	assert.Equal(t, []int64{queued.ID, inside.ID}, []int64{active[0].ID, active[1].ID})

	page, err := service.List(ctx, 0, 2)
	require.NoError(t, err)
	require.Len(t, page, 2)
	assert.Equal(t, []int64{queued.ID, inside.ID}, []int64{page[0].ID, page[1].ID})

	older, err := service.List(ctx, page[1].ID, 2)
	require.NoError(t, err)
	require.Len(t, older, 1)
	assert.Equal(t, outside.ID, older[0].ID)
}

// TestJobService_RecordsLifecycleLabelsAndEmits moved from
// jobs/recorder_test.go along with the persistence it exercises.
func TestJobService_RecordsLifecycleLabelsAndEmits(t *testing.T) {
	db, err := queries.Open(t.Context(), t.TempDir(), queries.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	now := time.UnixMilli(1_000)
	st := stores.New(db, stores.Options{Now: func() time.Time { return now }})
	var emitted []int64
	service := newJobService(st.Jobs, func(id int64) { emitted = append(emitted, id) })

	id := service.Begin(ctx, "Review PR", "review", "pr-1")
	require.Positive(t, id)
	now = time.UnixMilli(2_000)
	service.Running(ctx, id, 44)
	assert.Equal(t, id, service.Resume(ctx, 44))
	now = time.UnixMilli(3_000)
	service.Done(ctx, id)
	assert.Zero(t, service.Resume(ctx, 44))
	assert.Equal(t, []int64{id, id, id}, emitted)

	rows, err := service.List(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	job := rows[0]
	assert.Equal(t, jobs.JobStatusDone, job.Status)
	assert.Equal(t, "Completed", job.Step)
	assert.Equal(t, int64(1_000), job.CreatedAt)
	assert.Equal(t, int64(3_000), job.UpdatedAt)
	require.NotNil(t, job.CommandID)
	assert.Equal(t, int64(44), *job.CommandID)

	failedID := service.Begin(ctx, "Deploy", "deploy", "pr-2")
	service.Running(ctx, failedID, 45)
	service.Fail(ctx, failedID, "boom")
	rows, err = service.List(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, jobs.JobStatusFailed, rows[0].Status)
	assert.Equal(t, "Failed", rows[0].Step)
	assert.Equal(t, "boom", rows[0].Error)
	require.NotNil(t, rows[0].CommandID)
	assert.Equal(t, int64(45), *rows[0].CommandID)
	assert.Len(t, emitted, 6)
}

func TestJobService_TrackRunsToCompletionUnlinkedToACommand(t *testing.T) {
	service := newTestJobService(t)

	ran := make(chan struct{})
	id := service.Track(t.Context(), "Create session", "new-session", "sess-1", func(context.Context) error {
		close(ran)
		return nil
	})
	require.Positive(t, id)
	<-ran

	require.Eventually(t, func() bool {
		rows, err := service.List(t.Context(), 0, 10)
		return err == nil && len(rows) == 1 && rows[0].Status == jobs.JobStatusDone
	}, time.Second, 5*time.Millisecond)

	rows, err := service.List(t.Context(), 0, 10)
	require.NoError(t, err)
	assert.Nil(t, rows[0].CommandID, "a tracked background job has no output_command link")
	assert.Equal(t, "sess-1", rows[0].Target)
}

func TestJobService_TrackRecordsFailure(t *testing.T) {
	service := newTestJobService(t)

	id := service.Track(t.Context(), "Create session", "new-session", "sess-2", func(context.Context) error {
		return errors.New("clone failed")
	})
	require.Positive(t, id)

	require.Eventually(t, func() bool {
		rows, err := service.List(t.Context(), 0, 10)
		return err == nil && len(rows) == 1 && rows[0].Status == jobs.JobStatusFailed && rows[0].Error == "clone failed"
	}, time.Second, 5*time.Millisecond)
}

func TestJobService_ListActiveUsesBackendClockWindow(t *testing.T) {
	db, err := queries.Open(t.Context(), t.TempDir(), queries.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	now := time.UnixMilli(10_000)
	st := stores.New(db, stores.Options{Now: func() time.Time { return now }})
	service := newJobService(st.Jobs, nil)

	_, err = db.InsertJob(ctx, queries.InsertJobParams{
		CreatedAt: 1, UpdatedAt: now.Add(-jobs.DefaultLingerWindow).UnixMilli(), Status: "done", Label: "Boundary",
	})
	require.NoError(t, err)
	_, err = db.InsertJob(ctx, queries.InsertJobParams{
		CreatedAt: 2, UpdatedAt: now.Add(-jobs.DefaultLingerWindow - time.Millisecond).UnixMilli(), Status: "failed", Label: "Outside",
	})
	require.NoError(t, err)
	queued, err := db.InsertJob(ctx, queries.InsertJobParams{
		CreatedAt: 3, UpdatedAt: 1, Status: "queued", Label: "Queued",
	})
	require.NoError(t, err)

	active, err := service.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, active, 2)
	assert.Equal(t, queued.ID, active[0].ID)
	assert.Equal(t, "Boundary", active[1].Label)
}

func TestJobService_BeginFailureAndZeroTransitionsAreNoOps(t *testing.T) {
	db, err := queries.Open(t.Context(), t.TempDir(), queries.DefaultOpenOptions())
	require.NoError(t, err)
	require.NoError(t, db.Close())
	st := stores.New(db, stores.Options{})
	service := newJobService(st.Jobs, nil)
	id := service.Begin(t.Context(), "Review", "review", "pr-1")
	assert.Zero(t, id)
	assert.NotPanics(t, func() {
		service.Running(t.Context(), 0, 1)
		service.Done(t.Context(), 0)
		service.Fail(t.Context(), 0, "ignored")
	})
}
