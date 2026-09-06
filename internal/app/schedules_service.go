package app

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/schedule"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

const (
	defaultRunHistory = 50
	// previewOccurrences is more than the editor shows, so an unintended
	// cadence ("every minute") is obvious at a glance.
	previewOccurrences = 5
)

type ScheduleView struct {
	Workspace string `json:"workspace"`
	ID        string `json:"id"`
	// Name is empty when the entry has none; the reader falls back to the id.
	// Resolving the fallback here would round trip through the editor and
	// persist the id as a name the user never typed.
	Name     string `json:"name"`
	Cron     string `json:"cron"`
	Prompt   string `json:"prompt"`
	Disabled bool   `json:"disabled"`
	OnMissed string `json:"onMissed"`
	// NextRunAt is unix ms, nil when the schedule is disabled or its cron does
	// not parse.
	NextRunAt *int64 `json:"nextRunAt"`
	// LastRun is the newest run whatever its status, not the newest launched
	// one: a schedule that has been failing must say so where it is listed.
	LastRun *RunView `json:"lastRun"`
}

// RunView timestamps are unix ms; SessionID is nil when the run launched no chat.
type RunView struct {
	ID           int64  `json:"id"`
	Workspace    string `json:"workspace"`
	ScheduleID   string `json:"scheduleId"`
	ScheduleName string `json:"scheduleName"`
	ScheduledFor int64  `json:"scheduledFor"`
	StartedAt    int64  `json:"startedAt"`
	Reason       string `json:"reason"`
	Status       string `json:"status"`
	Missed       int    `json:"missed"`
	SessionID    *int64 `json:"sessionId"`
	Prompt       string `json:"prompt"`
	Error        string `json:"error"`
}

type PreviewRequest struct {
	Workspace string
	Cron      string
	Prompt    string
}

// A bad cron or template lands in CronError or PromptError rather than
// failing the call, so the editor can show it beside the field the user is
// still typing in.
type PreviewView struct {
	Next           []int64
	Prompt         string
	FirstRunPrompt string
	CronError      string
	PromptError    string
}

// SchedulesService reads and fires schedules. Writing one is
// AgentWorkspacesService's, because a schedule is a manifest key.
type SchedulesService struct {
	workspaces *agentws.Store
	db         *store.DB
	scheduler  *schedule.Scheduler
	logger     zerolog.Logger
}

func newSchedulesService(workspaces *agentws.Store, db *store.DB, scheduler *schedule.Scheduler, logger zerolog.Logger) *SchedulesService {
	return &SchedulesService{workspaces: workspaces, db: db, scheduler: scheduler, logger: logger}
}

// List refuses a manifest that does not parse rather than answering with
// last-good rows the file no longer says.
func (s *SchedulesService) List(ctx context.Context, workspace string) ([]ScheduleView, error) {
	if !validWorkspaceDir(workspace) {
		return nil, Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", workspace)
	}
	st, ok := s.workspaces.Status(workspace)
	if !ok {
		return nil, Errorf(KindNotFound, "workspace %q not found", workspace)
	}
	if !st.Valid {
		return nil, Errorf(KindInvalid, "agent-workspace.yaml has a problem (%s)", st.Err)
	}
	return scheduleRows(ctx, s.db, s.logger, st.Workspace.Schedules, time.Now()), nil
}

// RunNow leaves the cursor untouched, so the next real occurrence still happens.
func (s *SchedulesService) RunNow(ctx context.Context, workspace, id string) (RunView, error) {
	run, err := s.scheduler.RunNow(ctx, workspace, id)
	switch {
	case errors.Is(err, schedule.ErrNotFound):
		return RunView{}, Errorf(KindNotFound, "schedule %q not found in workspace %q", id, workspace)
	case err != nil:
		// Anything the launcher already classified keeps its own Kind. A refusal
		// (tmux down, the session cap reached) reclassified as internal would
		// tell the caller to retry something that will keep failing until they
		// act on it.
		if _, ok := errors.AsType[*Error](err); ok {
			return RunView{}, err
		}
		return RunView{}, Wrap(err, KindInternal, "running schedule %q", id)
	}
	return runView(run), nil
}

// Runs answers for a broken manifest: that is exactly when a caller wants to
// read what its schedules did. A removed schedule still answers because
// history outlives its entry, but an id that is neither declared nor has run
// is not_found rather than an empty list a caller would read as "it never
// fired".
func (s *SchedulesService) Runs(ctx context.Context, workspace, id string, limit int) ([]RunView, error) {
	if !validWorkspaceDir(workspace) {
		return nil, Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", workspace)
	}
	if id == "" {
		return nil, Errorf(KindInvalid, "a schedule id is required")
	}
	st, ok := s.workspaces.Status(workspace)
	if !ok {
		return nil, Errorf(KindNotFound, "workspace %q not found", workspace)
	}
	if limit <= 0 {
		limit = defaultRunHistory
	}

	records, err := s.db.ListScheduleRunsFor(ctx, workspace, id, limit)
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing runs for schedule %q in workspace %q", id, workspace)
	}
	if len(records) == 0 && !declaresSchedule(st.Workspace, id) {
		return nil, Errorf(KindNotFound, "schedule %q is not declared in workspace %q and has never run there", id, workspace)
	}

	out := make([]RunView, 0, len(records))
	for _, rec := range records {
		out = append(out, withoutDeletedChat(ctx, s.db, runView(scheduleRunFromRecord(rec))))
	}
	return out, nil
}

func declaresSchedule(workspace agentws.Workspace, id string) bool {
	for _, spec := range workspace.Schedules {
		if spec.ID == id {
			return true
		}
	}
	return false
}

// A scheduled chat deletes itself when its task is done, and a history entry
// must not offer to open a chat nobody can. A lookup that fails leaves the
// pointer as recorded: the history is still worth answering with.
func withoutDeletedChat(ctx context.Context, db *store.DB, run RunView) RunView {
	if run.SessionID == nil {
		return run
	}
	if _, ok, err := db.GetAgentWorkspaceSession(ctx, *run.SessionID); err == nil && !ok {
		run.SessionID = nil
	}
	return run
}

func (s *SchedulesService) Preview(_ context.Context, req PreviewRequest) (PreviewView, error) {
	view := PreviewView{Next: []int64{}}

	occurrences, err := schedule.NextOccurrences(req.Cron, time.Now(), previewOccurrences)
	if err != nil {
		view.CronError = err.Error()
	}
	for _, at := range occurrences {
		view.Next = append(view.Next, at.UnixMilli())
	}

	data := schedule.SamplePromptData()
	data.Schedule.Cron = req.Cron
	data.Workspace.Dir = req.Workspace
	names := scheduleWorkspaces{store: s.workspaces}
	if name := names.WorkspaceName(req.Workspace); name != "" {
		data.Workspace.Name = name
	}
	rendered, err := schedule.PreviewPrompt(req.Prompt, data)
	if err != nil {
		view.PromptError = err.Error()
	} else {
		view.Prompt = rendered.Prompt
		view.FirstRunPrompt = rendered.FirstRunPrompt
	}
	return view, nil
}

// A run-history read that fails leaves that row's LastRun nil rather than
// failing the listing: losing the editor's form and the workspace list behind
// it over a decoration on one row is the wrong trade.
func scheduleRows(ctx context.Context, db *store.DB, logger zerolog.Logger, specs []schedule.Spec, now time.Time) []ScheduleView {
	out := make([]ScheduleView, 0, len(specs))
	for _, spec := range specs {
		view := ScheduleView{
			Workspace: spec.Workspace,
			ID:        spec.ID,
			Name:      spec.Name,
			Cron:      spec.Cron,
			Prompt:    spec.Prompt,
			Disabled:  spec.Disabled,
			OnMissed:  onMissedName(spec),
		}
		if !spec.Disabled {
			if next, err := schedule.NextOccurrences(spec.Cron, now, 1); err == nil && len(next) > 0 {
				at := next[0].UnixMilli()
				view.NextRunAt = &at
			}
		}

		records, err := db.ListScheduleRunsFor(ctx, spec.Workspace, spec.ID, 1)
		switch {
		case err != nil:
			logger.Warn().Err(err).
				Str("workspace", spec.Workspace).Str("schedule", spec.ID).
				Msg("reading a schedule's last run")
		case len(records) > 0:
			last := withoutDeletedChat(ctx, db, runView(scheduleRunFromRecord(records[0])))
			view.LastRun = &last
		}
		out = append(out, view)
	}
	return out
}

// The file omits on_missed at its default; the wire has no "unset".
func onMissedName(spec schedule.Spec) string {
	if spec.OnMissed == "" {
		return string(schedule.OnMissedRun)
	}
	return string(spec.OnMissed)
}

func runView(run schedule.Run) RunView {
	view := RunView{
		ID:           run.ID,
		Workspace:    run.Workspace,
		ScheduleID:   run.ScheduleID,
		ScheduleName: run.ScheduleName,
		ScheduledFor: run.ScheduledFor.UnixMilli(),
		StartedAt:    run.StartedAt.UnixMilli(),
		Reason:       string(run.Reason),
		Status:       string(run.Status),
		Missed:       run.Missed,
		Prompt:       run.Prompt,
		Error:        run.Error,
	}
	if run.SessionID != 0 {
		sessionID := run.SessionID
		view.SessionID = &sessionID
	}
	return view
}
