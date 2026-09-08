package app

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/data/stores"
	"github.com/hay-kot/hive-desktop/internal/app/events"
	"github.com/hay-kot/hive-desktop/internal/app/jobs"
)

const (
	jobDefaultListLimit = 200
	jobMaxListLimit     = 1000
)

// JobService owns live action-run jobs: the frontend's read surface, the
// jobs.Recorder port the output worker holds, and sessionJobRunner's Track
// for tracked background session work.
type JobService struct {
	store  *stores.JobStore
	events *events.Bus
	log    zerolog.Logger
}

func newJobService(store *stores.JobStore, bus *events.Bus, logger zerolog.Logger) *JobService {
	return &JobService{store: store, events: bus, log: logger}
}

// List returns up to limit jobs with id < before, newest first.
func (s *JobService) List(ctx context.Context, before int64, limit int) ([]jobs.Job, error) {
	if limit <= 0 || limit > jobMaxListLimit {
		limit = jobDefaultListLimit
	}
	rows, err := s.store.List(ctx, before, limit)
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing jobs")
	}
	return jobsFromStore(rows), nil
}

// ListActive returns non-terminal jobs plus terminal jobs completed within
// the lingering window, so a just-finished run stays visible briefly.
func (s *JobService) ListActive(ctx context.Context) ([]jobs.Job, error) {
	rows, err := s.store.ListActiveWithin(ctx, jobs.DefaultLingerWindow)
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing active jobs")
	}
	return jobsFromStore(rows), nil
}

// Begin implements jobs.Recorder: it creates a queued job and returns its
// id, or zero after logging a persistence failure.
func (s *JobService) Begin(ctx context.Context, label, actionID, target string) int64 {
	job, err := s.store.Insert(ctx, stores.JobCreate{
		Status: jobs.JobStatusQueued.String(), Label: label, Step: jobs.StepFor(jobs.JobStatusQueued),
		ActionID: actionID, Target: target,
	})
	if err != nil {
		s.log.Warn().Err(err).Str("label", label).Str("action_id", actionID).Msg("beginning job failed")
		return 0
	}
	s.events.Publish(ctx, events.JobsUpdated{JobID: job.ID})
	return job.ID
}

// Running implements jobs.Recorder: it advances a job to running and links
// its output_command id. A zero id is a safe no-op for a caller whose Begin
// failed or whose recorder is off.
func (s *JobService) Running(ctx context.Context, id int64, commandID int64) {
	if id == 0 {
		return
	}
	if _, err := s.store.SetRunning(ctx, id, jobs.StepFor(jobs.JobStatusRunning), commandID); err != nil {
		s.log.Warn().Err(err).Int64("job_id", id).Int64("command_id", commandID).Msg("marking job running failed")
		return
	}
	s.events.Publish(ctx, events.JobsUpdated{JobID: id})
}

// Resume implements jobs.Recorder: it returns the running job linked to
// commandID, or zero when none can be restored. Lookup failures are logged
// and treated as no match so job tracking never derails command work.
func (s *JobService) Resume(ctx context.Context, commandID int64) int64 {
	job, found, err := s.store.FindRunningByCommand(ctx, commandID)
	if err != nil {
		s.log.Warn().Err(err).Int64("command_id", commandID).Msg("resuming job failed")
		return 0
	}
	if !found {
		return 0
	}
	return job.ID
}

// Done implements jobs.Recorder: it advances a job to done. A zero id is a
// safe no-op.
func (s *JobService) Done(ctx context.Context, id int64) {
	s.setStatus(ctx, id, jobs.JobStatusDone, "")
}

// Fail implements jobs.Recorder: it advances a job to failed with reason. A
// zero id is a safe no-op.
func (s *JobService) Fail(ctx context.Context, id int64, reason string) {
	s.setStatus(ctx, id, jobs.JobStatusFailed, reason)
}

// Track runs fn as a live background job — begin queued, mark running, then
// record done or failed by fn's result — and returns the job id. fn runs on a
// context detached from the caller's, so the work survives the request that
// started it (an RPC handler returns immediately). The job is not linked to an
// output_command, so it shows in the jobs UI without a deep-link. Persistence
// failures never derail fn.
//
// Do not call Track from inside Stores.Tx: context.WithoutCancel
// copies context values, including a transaction, into the goroutine, and no
// store call may run on a goroutine that did not open the transaction.
func (s *JobService) Track(ctx context.Context, label, actionID, target string, fn func(context.Context) error) int64 {
	id := s.Begin(ctx, label, actionID, target)
	bg := context.WithoutCancel(ctx)
	go func() {
		s.setStatus(bg, id, jobs.JobStatusRunning, "")
		if err := fn(bg); err != nil {
			s.Fail(bg, id, err.Error())
			return
		}
		s.Done(bg, id)
	}()
	return id
}

func (s *JobService) setStatus(ctx context.Context, id int64, status jobs.JobStatus, errText string) {
	if id == 0 {
		return
	}
	if _, err := s.store.SetStatus(ctx, id, status.String(), jobs.StepFor(status), errText); err != nil {
		s.log.Warn().Err(err).Int64("job_id", id).Str("status", status.String()).Msg("updating job status failed")
		return
	}
	s.events.Publish(ctx, events.JobsUpdated{JobID: id})
}

func jobsFromStore(rows []stores.Job) []jobs.Job {
	out := make([]jobs.Job, 0, len(rows))
	for _, row := range rows {
		out = append(out, jobFromStore(row))
	}
	return out
}

func jobFromStore(row stores.Job) jobs.Job {
	return jobs.Job{
		ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Status: jobs.JobStatus(row.Status),
		Label: row.Label, Step: row.Step, ActionID: row.ActionID, Target: row.Target,
		Error: row.Error, CommandID: row.CommandID,
	}
}
