package schedule

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// DefaultGrace is how late an occurrence may be and still be reported as due.
const DefaultGrace = 5 * time.Minute

// DefaultMaxSleep bounds one wait between passes.
//
// darwin's monotonic clock does not advance while the machine sleeps, so a
// timer armed for several hours fires hours late after a lid is opened. A
// bounded sleep costs one cheap pass a minute and puts the catch-up within a
// minute of wake instead.
const DefaultMaxSleep = time.Minute

// ErrNotFound reports a workspace/id pair that names no live schedule.
var ErrNotFound = errors.New("schedule: not found")

// Run is one execution of a schedule. ID is assigned by the store.
type Run struct {
	ID           int64
	Workspace    string
	ScheduleID   string
	ScheduleName string
	ScheduledFor time.Time
	StartedAt    time.Time
	Reason       Reason
	Missed       int
	Status       Status
	// SessionID is the chat the run launched, 0 when it launched none.
	SessionID int64
	Prompt    string
	Error     string
}

// Snapshot is one read of the workspace set.
type Snapshot struct {
	// Specs are the schedules of every workspace the source could read.
	Specs []Spec
	// Workspaces are the directories those specs came from. They bound what a
	// pass may prune: a workspace missing from this list contributed no specs,
	// so its cursors say nothing about schedules that were deleted.
	Workspaces []string
}

// Source is the live set of schedules, across every workspace. It answers both
// halves in one call because they have to agree: a pass that read its specs
// from one view of the root and its prune scope from another would delete the
// cursors of a workspace whose schedules it never saw.
type Source interface {
	Snapshot() Snapshot
}

// WorkspaceNamer resolves a workspace directory to its display name, for the
// prompt template's .Workspace.Name.
type WorkspaceNamer interface {
	WorkspaceName(dir string) string
}

// Store persists what the app has to remember between launches: how far each
// schedule has been evaluated, and what its runs did.
type Store interface {
	Cursor(ctx context.Context, workspace, id string) (Cursor, bool, error)
	SaveCursor(ctx context.Context, cursor Cursor) error
	// PruneCursors drops every cursor inside workspaces that is outside keep,
	// so a deleted schedule does not leave a cursor that back-fires when its id
	// is reused. A cursor in any other workspace is left alone: a manifest that
	// momentarily does not parse must not delete the state that says how far
	// its schedules got.
	PruneCursors(ctx context.Context, workspaces []string, keep []Cursor) error
	LastLaunchedRun(ctx context.Context, workspace, id string) (Run, bool, error)
	InsertRun(ctx context.Context, run Run) (Run, error)
}

// LaunchRequest is one chat to start.
type LaunchRequest struct {
	Workspace string
	Name      string
	Prompt    string
}

// Launcher starts chats and reports whether one is still running.
type Launcher interface {
	Launch(ctx context.Context, req LaunchRequest) (int64, error)
	SessionLive(ctx context.Context, sessionID int64) (bool, error)
}

// Options configure a Scheduler. Source, Store and Launcher are required.
type Options struct {
	Source   Source
	Store    Store
	Launcher Launcher
	Names    WorkspaceNamer

	// Now defaults to time.Now. Cron is evaluated in whatever location it
	// returns, so a clock that reports time.Local is what makes "0 9 * * 5"
	// mean 09:00 where the user is.
	Now      func() time.Time
	Grace    time.Duration
	MaxSleep time.Duration
	// OnRun is called after a run is recorded, launched or not.
	OnRun  func(Run)
	Logger zerolog.Logger
}

// Scheduler runs every workspace's schedules. One per process: a second one
// would double-launch every occurrence.
//
// Reload is a level-triggered latch, like runtime.Engine's: a signal arriving
// mid-pass is serviced by the next pass rather than dropped or queued.
type Scheduler struct {
	opts Options

	// pass serializes evaluation. Pass runs on the loop goroutine while RunNow
	// arrives from an HTTP handler, and two executions interleaving would
	// launch the same schedule twice.
	pass sync.Mutex

	reload chan struct{}

	cancel   context.CancelFunc
	stopped  chan struct{}
	stopOnce sync.Once
}

// New builds a Scheduler. Nothing runs until Start.
func New(opts Options) *Scheduler {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Grace <= 0 {
		opts.Grace = DefaultGrace
	}
	if opts.MaxSleep <= 0 {
		opts.MaxSleep = DefaultMaxSleep
	}
	return &Scheduler{
		opts:    opts,
		reload:  make(chan struct{}, 1),
		stopped: make(chan struct{}),
	}
}

// Start runs the loop. The first pass happens immediately, on the loop
// goroutine: that pass is the catch-up for everything that came due while the
// app was closed.
func (s *Scheduler) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go func() {
		defer close(s.stopped)
		s.loop(runCtx)
	}()
}

// Stop ends the loop and joins its goroutine. Calling it twice, or without
// Start, is a normal call: Close is the single teardown path and runs even
// when startup did not get that far.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		if s.cancel == nil {
			return
		}
		s.cancel()
		<-s.stopped
	})
}

// Reload asks for a pass now, because the schedule set changed. It never
// blocks.
func (s *Scheduler) Reload() {
	select {
	case s.reload <- struct{}{}:
	default:
	}
}

func (s *Scheduler) loop(ctx context.Context) {
	for {
		if err := s.Pass(ctx); err != nil && ctx.Err() == nil {
			s.opts.Logger.Warn().Err(err).Msg("a schedule pass reported an error")
		}

		timer := time.NewTimer(s.wait())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.reload:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// wait is how long the loop sleeps before the next pass: until the soonest
// occurrence, capped at MaxSleep.
func (s *Scheduler) wait() time.Duration {
	now := s.opts.Now()
	d := s.opts.MaxSleep
	if due, ok := NextDue(s.opts.Source.Snapshot().Specs, now); ok {
		if until := due.Sub(now); until < d {
			d = until
		}
	}
	return max(d, 0)
}

// Pass evaluates every spec once. A spec that fails is logged and the pass
// continues; the returned error is the first failure, for tests.
func (s *Scheduler) Pass(ctx context.Context) error {
	s.pass.Lock()
	defer s.pass.Unlock()

	snapshot := s.opts.Source.Snapshot()
	now := s.opts.Now()

	keep := make([]Cursor, 0, len(snapshot.Specs))
	var first error
	for _, spec := range snapshot.Specs {
		cursor, err := s.evaluate(ctx, spec, now)
		keep = append(keep, cursor)
		if err != nil {
			s.opts.Logger.Warn().Err(err).Str("workspace", spec.Workspace).Str("schedule", spec.ID).
				Msg("a schedule could not be evaluated")
			if first == nil {
				first = err
			}
		}
	}

	if err := s.opts.Store.PruneCursors(ctx, snapshot.Workspaces, keep); err != nil {
		s.opts.Logger.Warn().Err(err).Msg("stale schedule cursors could not be pruned")
		if first == nil {
			first = err
		}
	}
	return first
}

// RunNow executes a schedule immediately. The cursor is untouched: a manual
// run answers "does this work", and consuming the window would silently cancel
// the next real occurrence.
func (s *Scheduler) RunNow(ctx context.Context, workspace, id string) (Run, error) {
	s.pass.Lock()
	defer s.pass.Unlock()

	var spec Spec
	found := false
	for _, candidate := range s.opts.Source.Snapshot().Specs {
		if candidate.Workspace == workspace && candidate.ID == id {
			spec, found = candidate, true
			break
		}
	}
	if !found {
		return Run{}, fmt.Errorf("%w: schedule %q in workspace %q", ErrNotFound, id, workspace)
	}

	now := s.opts.Now()
	run, _, err := s.execute(ctx, spec, Decision{ScheduledFor: now, Reason: ReasonManual}, now)
	return run, err
}

// evaluate plans one spec, executes the decision it produced, and closes the
// cursor.
//
// The cursor closes even when the execution failed. An occurrence is never
// retried: a run that launched but could not be recorded would otherwise fire
// a second chat on the next pass, which is worse than the missing row. The one
// exception is a failure the quit itself caused: nothing was launched, so the
// occurrence is left open for the next start rather than burned.
func (s *Scheduler) evaluate(ctx context.Context, spec Spec, now time.Time) (Cursor, error) {
	identity := Cursor{Workspace: spec.Workspace, ID: spec.ID, EvaluatedThrough: now, Cron: spec.Cron}

	stored, ok, err := s.opts.Store.Cursor(ctx, spec.Workspace, spec.ID)
	if err != nil {
		return identity, fmt.Errorf("reading the cursor: %w", err)
	}
	var previous *Cursor
	if ok {
		previous = &stored
	}

	evaluation := Evaluate(spec, previous, now, s.opts.Grace)
	var (
		runErr   error
		launched bool
	)
	if evaluation.Decision != nil {
		_, launched, runErr = s.execute(ctx, spec, *evaluation.Decision, now)
		if runErr != nil && !launched && ctx.Err() != nil {
			return evaluation.Cursor, runErr
		}
	}

	// A chat that exists has to be recorded even though the app is quitting:
	// the cursor is what stops the next start from launching it a second time
	// as a catch-up.
	saveCtx := ctx
	if launched {
		saveCtx = context.WithoutCancel(ctx)
	}
	if err := s.opts.Store.SaveCursor(saveCtx, evaluation.Cursor); err != nil && runErr == nil {
		runErr = fmt.Errorf("saving the cursor: %w", err)
	}
	return evaluation.Cursor, runErr
}

// execute performs one decision and records what it did. A refusal (the
// previous chat is still open, a missed run the schedule says to skip) and a
// failure are both recorded as runs: a schedule that silently does nothing is
// indistinguishable from one that is broken.
//
// launched reports that a chat was started, whether or not the run recording
// it made it into the store. The caller needs it to decide whether the
// occurrence has been consumed.
func (s *Scheduler) execute(ctx context.Context, spec Spec, decision Decision, now time.Time) (_ Run, launched bool, _ error) {
	run := Run{
		Workspace:    spec.Workspace,
		ScheduleID:   spec.ID,
		ScheduleName: spec.DisplayName(),
		ScheduledFor: decision.ScheduledFor,
		StartedAt:    now,
		Reason:       decision.Reason,
		Missed:       decision.Missed,
	}

	last, hasLast, err := s.opts.Store.LastLaunchedRun(ctx, spec.Workspace, spec.ID)
	if err != nil {
		return Run{}, false, fmt.Errorf("reading the last run: %w", err)
	}

	live := false
	if hasLast && last.SessionID != 0 {
		if live, err = s.opts.Launcher.SessionLive(ctx, last.SessionID); err != nil {
			return Run{}, false, fmt.Errorf("checking the previous run's chat: %w", err)
		}
	}

	// Bookkeeping outlives the pass once a chat exists: database/sql refuses a
	// cancelled context before it touches the connection, so a quit landing
	// here would leave a chat running with no run row and an open cursor, and
	// the next start would launch it again.
	bookkeeping := ctx

	switch {
	case live:
		run.Status = StatusSkipped
		run.Error = "the previous run's chat is still running"
	case decision.Skip:
		run.Status = StatusSkipped
		run.Error = "missed while the app was closed (on_missed: skip)"
	default:
		prompt, err := RenderPrompt(spec.Prompt, s.promptData(spec, decision, now, last, hasLast))
		if err != nil {
			run.Status = StatusFailed
			run.Error = err.Error()
			break
		}
		run.Prompt = prompt

		name := fmt.Sprintf("%s - %s", spec.DisplayName(), decision.ScheduledFor.Format("Jan 2 15:04"))
		sessionID, err := s.opts.Launcher.Launch(ctx, LaunchRequest{Workspace: spec.Workspace, Name: name, Prompt: prompt})
		if err != nil {
			// A launch the quit itself stopped is not the schedule failing.
			// Reporting it would spend the occurrence on a chat that never
			// started, so it stays open for the next start instead.
			if ctx.Err() != nil {
				return Run{}, false, fmt.Errorf("launching the chat: %w", err)
			}
			run.Status = StatusFailed
			run.Error = err.Error()
			break
		}
		run.Status = StatusLaunched
		run.SessionID = sessionID
		launched = true
		bookkeeping = context.WithoutCancel(ctx)
	}

	recorded, err := s.opts.Store.InsertRun(bookkeeping, run)
	if err != nil {
		return Run{}, launched, fmt.Errorf("recording the run: %w", err)
	}
	if s.opts.OnRun != nil {
		s.opts.OnRun(recorded)
	}
	return recorded, launched, nil
}

func (s *Scheduler) promptData(spec Spec, decision Decision, now time.Time, last Run, hasLast bool) PromptData {
	data := PromptData{
		Now:          now,
		ScheduledFor: decision.ScheduledFor,
		Reason:       string(decision.Reason),
		Missed:       decision.Missed,
	}
	data.Schedule.ID = spec.ID
	data.Schedule.Name = spec.DisplayName()
	data.Schedule.Cron = spec.Cron
	data.Workspace.Dir = spec.Workspace
	data.Workspace.Name = spec.Workspace
	if s.opts.Names != nil {
		if name := s.opts.Names.WorkspaceName(spec.Workspace); name != "" {
			data.Workspace.Name = name
		}
	}
	if hasLast {
		scheduledFor := last.ScheduledFor
		data.LastRun = &scheduledFor
	}
	return data
}
