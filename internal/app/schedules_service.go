package app

import (
	"context"
	"errors"
	"strings"
	"time"

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
	Name      string `json:"name"`
	Cron      string `json:"cron"`
	Prompt    string `json:"prompt"`
	Disabled  bool   `json:"disabled"`
	OnMissed  string `json:"onMissed"`
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

// ScheduleEdit names the manifest fields the schedule editor writes. Anything
// else the entry says is untouched, the same way WorkspaceEdit leaves the rest
// of the manifest alone.
type ScheduleEdit struct {
	Workspace string
	ID        string
	Name      string
	Cron      string
	Prompt    string
	Disabled  bool
	OnMissed  string
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

// SchedulesService owns scheduled chats: the definitions in each workspace
// manifest, and the run state the app keeps beside them. Writes go to the
// manifest, then reload both readers of it -- the workspace store the UI lists
// from, and the scheduler's own view of what is due.
type SchedulesService struct {
	workspaces *agentws.Store
	db         *store.DB
	scheduler  *schedule.Scheduler
	publish    func(workspace string)
}

func newSchedulesService(workspaces *agentws.Store, db *store.DB, scheduler *schedule.Scheduler, publish func(workspace string)) *SchedulesService {
	if publish == nil {
		publish = func(string) {}
	}
	return &SchedulesService{workspaces: workspaces, db: db, scheduler: scheduler, publish: publish}
}

// List returns a workspace's schedules in manifest order.
func (s *SchedulesService) List(ctx context.Context, workspace string) ([]ScheduleView, error) {
	specs, err := s.specs(workspace)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]ScheduleView, 0, len(specs))
	for _, spec := range specs {
		view, err := s.scheduleView(ctx, spec, now)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// Save upserts one schedule into its workspace manifest, matched by id, and
// returns it as it reads back.
func (s *SchedulesService) Save(ctx context.Context, edit ScheduleEdit) (ScheduleView, error) {
	spec := edit.spec()
	if err := spec.Validate(); err != nil {
		return ScheduleView{}, Errorf(KindInvalid, "%s", err)
	}
	if _, err := s.specs(edit.Workspace); err != nil {
		return ScheduleView{}, err
	}
	if err := agentws.WriteSchedule(s.workspaces.Root(), edit.Workspace, spec); err != nil {
		return ScheduleView{}, Wrap(err, KindInternal, "saving schedule %q", spec.ID)
	}
	if err := s.refresh(edit.Workspace); err != nil {
		return ScheduleView{}, err
	}

	specs, err := s.specs(edit.Workspace)
	if err != nil {
		return ScheduleView{}, err
	}
	for _, saved := range specs {
		if saved.ID == spec.ID {
			return s.scheduleView(ctx, saved, time.Now())
		}
	}
	return ScheduleView{}, Errorf(KindInternal, "schedule %q was written but the workspace did not reload with it", spec.ID)
}

// Delete removes one schedule from its manifest and drops the cursor that
// tracked how far it had been evaluated, so an id reused later starts from now
// instead of back-firing every occurrence since the old one was last seen. The
// run history stays: it is the record of what the schedule did, and deleting
// the definition is not a reason to erase it.
func (s *SchedulesService) Delete(ctx context.Context, workspace, id string) error {
	if _, err := s.specs(workspace); err != nil {
		return err
	}
	if err := agentws.RemoveSchedule(s.workspaces.Root(), workspace, id); err != nil {
		return Wrap(err, KindInternal, "deleting schedule %q", id)
	}
	if err := s.db.DeleteScheduleCursor(ctx, workspace, id); err != nil {
		return Wrap(err, KindInternal, "deleting the cursor for schedule %q", id)
	}
	return s.refresh(workspace)
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

// refresh re-reads the manifest the write just changed, points the scheduler
// at the new set, and announces it. The store reload has to succeed: every
// read after a write, this service's own included, comes out of that snapshot.
func (s *SchedulesService) refresh(workspace string) error {
	if err := s.workspaces.Reload(); err != nil {
		return Wrap(err, KindInternal, "reloading the workspace root")
	}
	s.scheduler.Reload()
	s.publish(workspace)
	return nil
}

func (s *SchedulesService) scheduleView(ctx context.Context, spec schedule.Spec, now time.Time) (ScheduleView, error) {
	view := ScheduleView{
		Workspace: spec.Workspace,
		ID:        spec.ID,
		Name:      spec.DisplayName(),
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

	records, err := s.db.ListScheduleRunsFor(ctx, spec.Workspace, spec.ID, 1)
	if err != nil {
		return ScheduleView{}, Wrap(err, KindInternal, "reading the last run of schedule %q", spec.ID)
	}
	if len(records) > 0 {
		last := runView(scheduleRunFromRecord(records[0]))
		view.LastRun = &last
	}
	return view, nil
}

func (e ScheduleEdit) spec() schedule.Spec {
	return schedule.Spec{
		Workspace: e.Workspace,
		ID:        strings.TrimSpace(e.ID),
		Name:      strings.TrimSpace(e.Name),
		Cron:      strings.TrimSpace(e.Cron),
		Prompt:    e.Prompt,
		Disabled:  e.Disabled,
		OnMissed:  schedule.OnMissed(strings.TrimSpace(e.OnMissed)),
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
