package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/events"
	"github.com/hay-kot/hive-desktop/internal/app/schedule"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// The consumer-defined ports internal/app/schedule declares, filled from what
// this package already owns. They are values rather than pointers: each is a
// handle or two over a store App holds for its whole life.

// scheduleWorkspaces answers both the scheduler's Source and its
// WorkspaceNamer. One type for both because every answer is a projection of
// the same workspace store, which swaps its status set whole on reload: a
// second adapter over the same store would only be a second place to keep the
// "valid workspaces only" rule in step.
type scheduleWorkspaces struct{ store *agentws.Store }

// Snapshot returns every valid workspace's schedules, and the directories they
// came from, out of one Statuses read.
//
// A workspace whose manifest failed to parse contributes neither: its
// last-good schedules may name a cadence the file on disk no longer says, and
// firing that would be the scheduler acting on a manifest the user has already
// replaced. Leaving it out of the directories too is what stops the pass from
// reading "every schedule here was deleted" from a file the app could not
// open, so both halves have to come from the same read.
func (w scheduleWorkspaces) Snapshot() schedule.Snapshot {
	statuses := w.store.Statuses()
	snapshot := schedule.Snapshot{
		Specs:      make([]schedule.Spec, 0, len(statuses)),
		Workspaces: make([]string, 0, len(statuses)),
	}
	for _, st := range statuses {
		if !st.Valid {
			continue
		}
		snapshot.Workspaces = append(snapshot.Workspaces, st.Dir)
		snapshot.Specs = append(snapshot.Specs, st.Workspace.Schedules...)
	}
	return snapshot
}

func (w scheduleWorkspaces) WorkspaceName(dir string) string {
	st, _ := w.store.Status(dir)
	return st.Workspace.Name
}

// scheduleStore is the scheduler's Store over the pipeline database. The
// scheduler works in time.Time and both schedule tables hold unix
// milliseconds, so this is where the two meet.
type scheduleStore struct{ db *store.DB }

func (s scheduleStore) Cursor(ctx context.Context, workspace, id string) (schedule.Cursor, bool, error) {
	rec, ok, err := s.db.GetScheduleCursor(ctx, workspace, id)
	if err != nil || !ok {
		return schedule.Cursor{}, false, err
	}
	return schedule.Cursor{
		Workspace:        rec.Workspace,
		ID:               rec.ScheduleID,
		EvaluatedThrough: time.UnixMilli(rec.EvaluatedThrough),
		Cron:             rec.Cron,
	}, true, nil
}

func (s scheduleStore) SaveCursor(ctx context.Context, cursor schedule.Cursor) error {
	return s.db.UpsertScheduleCursor(ctx, store.ScheduleCursorRecord{
		Workspace:        cursor.Workspace,
		ScheduleID:       cursor.ID,
		EvaluatedThrough: cursor.EvaluatedThrough.UnixMilli(),
		Cron:             cursor.Cron,
	})
}

// PruneCursors deletes the cursors of workspaces the pass could read, minus
// the ones it kept. Everything else is left where it is: a cursor whose
// workspace is missing from the root, or whose manifest did not parse this
// pass, is state the app cannot yet say is stale. Losing it silently drops the
// occurrence between the break and the fix. A schedule the editor removed from
// a manifest the pass could read is exactly the case this does prune, so an id
// reused later starts from now rather than back-firing every occurrence since
// the old one was last seen. A deleted workspace runs through
// DeleteWorkspace instead.
func (s scheduleStore) PruneCursors(ctx context.Context, workspaces []string, keep []schedule.Cursor) error {
	stored, err := s.db.ListScheduleCursors(ctx)
	if err != nil {
		return err
	}
	scope := make(map[string]struct{}, len(workspaces))
	for _, workspace := range workspaces {
		scope[workspace] = struct{}{}
	}
	live := make(map[string]struct{}, len(keep))
	for _, cursor := range keep {
		live[cursorKey(cursor.Workspace, cursor.ID)] = struct{}{}
	}
	for _, rec := range stored {
		if _, ok := scope[rec.Workspace]; !ok {
			continue
		}
		if _, ok := live[cursorKey(rec.Workspace, rec.ScheduleID)]; ok {
			continue
		}
		if err := s.db.DeleteScheduleCursor(ctx, rec.Workspace, rec.ScheduleID); err != nil {
			return err
		}
	}
	return nil
}

func cursorKey(workspace, id string) string { return workspace + "\x00" + id }

func (s scheduleStore) LastLaunchedRun(ctx context.Context, workspace, id string) (schedule.Run, bool, error) {
	rec, ok, err := s.db.LastLaunchedScheduleRun(ctx, workspace, id)
	if err != nil || !ok {
		return schedule.Run{}, false, err
	}
	return scheduleRunFromRecord(rec), true, nil
}

func (s scheduleStore) InsertRun(ctx context.Context, run schedule.Run) (schedule.Run, error) {
	rec, err := s.db.InsertScheduleRun(ctx, store.ScheduleRunRecord{
		Workspace:    run.Workspace,
		ScheduleID:   run.ScheduleID,
		ScheduleName: run.ScheduleName,
		ScheduledFor: run.ScheduledFor.UnixMilli(),
		StartedAt:    run.StartedAt.UnixMilli(),
		Reason:       string(run.Reason),
		Missed:       run.Missed,
		Status:       string(run.Status),
		SessionID:    run.SessionID,
		Prompt:       run.Prompt,
		Error:        run.Error,
	})
	if err != nil {
		return schedule.Run{}, err
	}
	return scheduleRunFromRecord(rec), nil
}

func scheduleRunFromRecord(rec store.ScheduleRunRecord) schedule.Run {
	return schedule.Run{
		ID:           rec.ID,
		Workspace:    rec.Workspace,
		ScheduleID:   rec.ScheduleID,
		ScheduleName: rec.ScheduleName,
		ScheduledFor: time.UnixMilli(rec.ScheduledFor),
		StartedAt:    time.UnixMilli(rec.StartedAt),
		Reason:       schedule.Reason(rec.Reason),
		Missed:       rec.Missed,
		Status:       schedule.Status(rec.Status),
		SessionID:    rec.SessionID,
		Prompt:       rec.Prompt,
		Error:        rec.Error,
	}
}

// scheduleLauncher starts a scheduled chat through the same service a hand
// started one goes through, so a scheduled run gets the workspace's
// regenerated artifacts and is held by the same session cap.
type scheduleLauncher struct{ workspaces *AgentWorkspacesService }

func (l scheduleLauncher) Launch(ctx context.Context, req schedule.LaunchRequest) (int64, error) {
	view, err := l.workspaces.StartScheduledSession(ctx, StartScheduledSession{
		Workspace: req.Workspace, Name: req.Name, Prompt: req.Prompt,
	})
	if err != nil {
		return 0, err
	}
	// An interactive launch shows an early exit as a notice on a pane the user
	// is already looking at. A scheduled one has no pane, so the same fact has
	// to reach the run history as a failure -- a CLI that is not on PATH, or a
	// flag it rejected, is not a chat that ran.
	if view.ExitedEarly {
		return 0, Errorf(KindInternal, "%s", view.Notice)
	}
	return view.ID, nil
}

func (l scheduleLauncher) SessionLive(ctx context.Context, sessionID int64) (bool, error) {
	return l.workspaces.SessionLive(ctx, sessionID)
}

// buildScheduler wires the scheduler over the workspace set, the pipeline
// database and the session launcher. It is built in every mode, mock ones
// included: a fixture root declares no schedules, so the loop passes over an
// empty set rather than becoming a second startup path to keep in step.
func (a *App) buildScheduler(logger zerolog.Logger) *schedule.Scheduler {
	workspaces := scheduleWorkspaces{store: a.agentWorkspaceStore}
	return schedule.New(schedule.Options{
		Source:   workspaces,
		Names:    workspaces,
		Store:    scheduleStore{db: a.Store},
		Launcher: scheduleLauncher{workspaces: a.AgentWorkspaces},
		Logger:   logger,
		OnRun: func(run schedule.Run) {
			a.Events.Publish(a.ctx, events.SchedulesUpdated{Workspace: run.Workspace})
			a.activityStore.Record(a.ctx, activity.ScheduleRun(
				run.ScheduleName, run.Workspace, string(run.Status), scheduleRunDetail(run)))
		},
	})
}

// scheduleRunDetail is the activity body: why the run happened, how much it
// stood in for, where it went, and what stopped it.
func scheduleRunDetail(run schedule.Run) string {
	parts := []string{string(run.Reason)}
	if run.Missed > 0 {
		parts = append(parts, fmt.Sprintf("%d missed", run.Missed))
	}
	if run.SessionID != 0 {
		parts = append(parts, fmt.Sprintf("chat %d", run.SessionID))
	}
	if run.Error != "" {
		parts = append(parts, run.Error)
	}
	return strings.Join(parts, " · ")
}
