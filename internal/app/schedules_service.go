package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/schedule"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

const (
	// defaultRunHistory is how many runs Runs answers with when the caller
	// names no limit.
	defaultRunHistory = 50
	// previewOccurrences is how far ahead Preview looks. The editor shows the
	// first few; the rest are there to make an unintended cadence ("every
	// minute") obvious at a glance.
	previewOccurrences = 5
)

// ScheduleView is one schedules: entry joined with the state the manifest does
// not carry: when it fires next, and how it went last time.
type ScheduleView struct {
	Workspace string `json:"workspace"`
	ID        string `json:"id"`
	// Name is the manifest's own name, empty when the entry has none. Whatever
	// shows it falls back to the id itself (schedule.Spec.DisplayName); a view
	// that resolved the fallback here would round trip through the editor and
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

// RunView is one execution of a schedule. Timestamps are unix ms; SessionID is
// nil when the run launched no chat.
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

// ScheduleEdit is one row of the workspace editor's schedules section. It
// travels inside WorkspaceEdit, because a schedule is a manifest key like
// mcps: or skills: and is saved with the rest of them.
type ScheduleEdit struct {
	ID       string
	Name     string
	Cron     string
	Prompt   string
	Disabled bool
	OnMissed string
}

// PreviewRequest is an unsaved edit to dry-run.
type PreviewRequest struct {
	Workspace string
	Cron      string
	Prompt    string
}

// PreviewView is that dry run. A bad cron or template lands in CronError or
// PromptError rather than failing the call, so the editor can show it beside
// the field the user is still typing in.
type PreviewView struct {
	Next        []int64
	Prompt      string
	CronError   string
	PromptError string
}

// SchedulesService reads scheduled chats: the definitions in each workspace
// manifest joined with the run state the app keeps beside them, plus the two
// things a user does to one outside the editor -- fire it now, and read what
// it did. Writing a schedule is AgentWorkspacesService's, because a schedule
// is a manifest key saved with the rest of the manifest.
type SchedulesService struct {
	workspaces *agentws.Store
	db         *store.DB
	scheduler  *schedule.Scheduler
	logger     zerolog.Logger
}

func newSchedulesService(workspaces *agentws.Store, db *store.DB, scheduler *schedule.Scheduler, logger zerolog.Logger) *SchedulesService {
	return &SchedulesService{workspaces: workspaces, db: db, scheduler: scheduler, logger: logger}
}

// List returns a workspace's schedules in manifest order.
func (s *SchedulesService) List(ctx context.Context, workspace string) ([]ScheduleView, error) {
	specs, err := s.specs(workspace)
	if err != nil {
		return nil, err
	}
	return scheduleRows(ctx, s.db, s.logger, specs, time.Now()), nil
}

// RunNow fires a schedule outside its timetable. The cursor is untouched, so
// the next real occurrence still happens.
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
		var classified *Error
		if errors.As(err, &classified) {
			return RunView{}, err
		}
		return RunView{}, Wrap(err, KindInternal, "running schedule %q", id)
	}
	return runView(run), nil
}

// Runs returns run history newest first. An empty id spans every schedule in
// the workspace; a limit of zero or less takes the default.
//
// It does not require the workspace to still exist: history outlives the
// manifest entry it came from, and a workspace whose manifest just broke is
// exactly when a caller wants to read what its schedules did.
func (s *SchedulesService) Runs(ctx context.Context, workspace, id string, limit int) ([]RunView, error) {
	if limit <= 0 {
		limit = defaultRunHistory
	}

	var (
		records []store.ScheduleRunRecord
		err     error
	)
	if id == "" {
		records, err = s.db.ListScheduleRuns(ctx, workspace, limit)
	} else {
		records, err = s.db.ListScheduleRunsFor(ctx, workspace, id, limit)
	}
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing runs for workspace %q", workspace)
	}

	out := make([]RunView, 0, len(records))
	for _, rec := range records {
		out = append(out, runView(scheduleRunFromRecord(rec)))
	}
	return out, nil
}

// Preview dry-runs an unsaved edit. It never fails on the edit itself: a cron
// or template that does not parse is what the editor is being told about.
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
	prompt, err := schedule.RenderPrompt(req.Prompt, data)
	if err != nil {
		view.PromptError = err.Error()
	} else {
		view.Prompt = prompt
	}
	return view, nil
}

// specs is the workspace's schedules, and the one place a workspace that is
// missing or broken is refused.
func (s *SchedulesService) specs(workspace string) ([]schedule.Spec, error) {
	if !validWorkspaceDir(workspace) {
		return nil, Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", workspace)
	}
	for _, st := range s.workspaces.Statuses() {
		if st.Dir != workspace {
			continue
		}
		if !st.Valid {
			return nil, Errorf(KindNotFound, "workspace %q could not be read: %s", workspace, st.Err)
		}
		return st.Workspace.Schedules, nil
	}
	return nil, Errorf(KindNotFound, "workspace %q not found", workspace)
}

// scheduleRows joins manifest specs with the state the manifest does not
// carry. Both readers of a workspace's schedules go through it -- the
// schedules list and the workspace view the editor loads -- so a row means the
// same thing wherever it is read.
//
// A run-history read that fails leaves that row's LastRun nil rather than
// failing the listing. The definitions are the manifest's and are already in
// hand; losing the whole list, and with it the editor's form and the workspace
// list behind it, over a decoration on one row is the wrong trade.
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
			last := runView(scheduleRunFromRecord(records[0]))
			view.LastRun = &last
		}
		out = append(out, view)
	}
	return out
}

// spec is the manifest entry this edit stands for. Workspace stays empty: the
// loader stamps it on when the file is read back, and nothing between here and
// the write needs it.
func (e ScheduleEdit) spec() schedule.Spec {
	return schedule.Spec{
		ID:       strings.TrimSpace(e.ID),
		Name:     strings.TrimSpace(e.Name),
		Cron:     strings.TrimSpace(e.Cron),
		Prompt:   e.Prompt,
		Disabled: e.Disabled,
		OnMissed: schedule.OnMissed(strings.TrimSpace(e.OnMissed)),
	}
}

// onMissedName resolves the manifest's optional on_missed to the closed set
// the wire promises: the file omits the key at its default, the contract does
// not have an "unset".
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
