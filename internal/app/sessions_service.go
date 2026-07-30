package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
)

// Job action ids label session jobs in the jobs UI.
const (
	newSessionJobActionID     = "new-session"
	deleteSessionJobActionID  = "delete-session"
	recycleSessionJobActionID = "recycle-session"
	pruneSessionsJobActionID  = "prune-sessions"
)

type sessionLauncher interface {
	LaunchSession(context.Context, dispatch.LaunchSessionRequest) (dispatch.SessionExecutionOutcome, error)
	SessionLaunchOptions(context.Context) (dispatch.SessionLaunchOptions, error)
}

// sessionManager is the read and lifecycle half of the session surface, kept
// apart from sessionLauncher because launching is a dispatch action: an output
// command holds a launcher, and it has no business holding a delete.
type sessionManager interface {
	ListSessions(context.Context) ([]dispatch.SessionSummary, error)
	SessionDetail(ctx context.Context, id string) (dispatch.SessionDetail, error)
	SessionRisk(ctx context.Context, id string) (dispatch.SessionRisk, error)
	RenameSession(ctx context.Context, id, name string) error
	SetSessionGroup(ctx context.Context, id, group string) error
	DeleteSession(ctx context.Context, id string) error
	RecycleSession(ctx context.Context, id string) error
	PruneSessions(ctx context.Context) (int, error)
}

// sessionTmux renames the live tmux session behind a slug. Hive's rename
// recomputes the slug and saves; the tmux session keeps its old name, so
// without this the stored slug addresses nothing.
type sessionTmux interface {
	RenameSession(ctx context.Context, from, to string) error
}

// sessionJobRunner runs slow session work as a tracked background job so a
// clone, a worktree removal or a prune does not block the caller.
type sessionJobRunner interface {
	Track(ctx context.Context, label, actionID, target string, fn func(context.Context) error) int64
}

// SessionsService is the desktop's session surface: the New Session form's
// launch path, and read plus lifecycle management of the sessions that exist.
type SessionsService struct {
	launcher sessionLauncher
	manager  sessionManager
	tmux     sessionTmux
	jobs     sessionJobRunner
}

func newSessionsService(launcher sessionLauncher, manager sessionManager, tmux sessionTmux, jobs sessionJobRunner) *SessionsService {
	return &SessionsService{launcher: launcher, manager: manager, tmux: tmux, jobs: jobs}
}

// SessionLaunchOptions supplies the configured repository and agent choices the
// New Session form presents.
func (s *SessionsService) SessionLaunchOptions(ctx context.Context) (dispatch.SessionLaunchOptions, error) {
	if s.launcher == nil {
		return dispatch.SessionLaunchOptions{}, Errorf(KindUnavailable, "session launch options are unavailable")
	}
	opts, err := s.launcher.SessionLaunchOptions(ctx)
	return opts, Wrap(err, KindInternal, "resolving session launch options")
}

// ListSessions returns every session, whatever its state. Only an active one
// can be attached to — Slug is its tmux session name — but a recycled or
// corrupted session still has to be readable and deletable.
func (s *SessionsService) ListSessions(ctx context.Context) ([]dispatch.SessionSummary, error) {
	if s.manager == nil {
		return nil, Errorf(KindUnavailable, "session listing is unavailable")
	}
	sessions, err := s.manager.ListSessions(ctx)
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing sessions")
	}
	return sessions, nil
}

// SessionDetail reads one session in full.
func (s *SessionsService) SessionDetail(ctx context.Context, id string) (dispatch.SessionDetail, error) {
	if s.manager == nil {
		return dispatch.SessionDetail{}, Errorf(KindUnavailable, "session details are unavailable")
	}
	if strings.TrimSpace(id) == "" {
		return dispatch.SessionDetail{}, Errorf(KindInvalid, "session id is required")
	}
	detail, err := s.manager.SessionDetail(ctx, id)
	if err != nil {
		return dispatch.SessionDetail{}, Wrap(err, KindNotFound, "reading session %q", id)
	}
	return detail, nil
}

// SessionRisk reports the work a delete or recycle of id would discard, so the
// caller can name it in its confirmation instead of guessing.
func (s *SessionsService) SessionRisk(ctx context.Context, id string) (dispatch.SessionRisk, error) {
	if s.manager == nil {
		return dispatch.SessionRisk{}, Errorf(KindUnavailable, "session risk checks are unavailable")
	}
	if strings.TrimSpace(id) == "" {
		return dispatch.SessionRisk{}, Errorf(KindInvalid, "session id is required")
	}
	risk, err := s.manager.SessionRisk(ctx, id)
	if err != nil {
		return dispatch.SessionRisk{}, Wrap(err, KindNotFound, "checking session %q", id)
	}
	return risk, nil
}

// CreateSession validates a New Session form, then launches the session as a
// background job and returns its id. Creation (which may clone a repository)
// runs asynchronously and its outcome surfaces in the jobs UI, so only
// validation errors are returned here.
func (s *SessionsService) CreateSession(ctx context.Context, req dispatch.CreateSessionRequest) (int64, error) {
	if s.launcher == nil || s.jobs == nil {
		return 0, Errorf(KindUnavailable, "session creation is unavailable")
	}

	repo := strings.TrimSpace(req.Repository)
	if repo == "" {
		return 0, Errorf(KindInvalid, "repository is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return 0, Errorf(KindInvalid, "session name is required")
	}
	if err := dispatch.ValidateSessionName(name); err != nil {
		return 0, Wrap(err, KindInvalid, "session name")
	}

	launch := dispatch.LaunchSessionRequest{
		Name:   name,
		Prompt: strings.TrimSpace(req.Prompt),
		Agent:  strings.TrimSpace(req.Agent),
		Repo:   repo,
	}
	jobID := s.jobs.Track(ctx, "Create session", newSessionJobActionID, name, func(bg context.Context) error {
		if _, err := s.launcher.LaunchSession(bg, launch); err != nil {
			if errors.Is(err, dispatch.ErrDuplicateSessionName) {
				return fmt.Errorf("a session named %q already exists", name)
			}
			return err
		}
		return nil
	})
	return jobID, nil
}

// RenameSession renames a session and returns its new summary.
//
// Hive's rename recomputes the slug, and the slug is the tmux session name, so
// the live tmux session is renamed in the same breath — otherwise the stored
// slug addresses a session tmux does not have and every subsequent attach
// fails. tmux goes first: a collision or a missing binary then aborts before
// anything is written, and the store write is the only step left to fail. If it
// does, the tmux rename is put back, because the one state we must not leave
// behind is a slug that does not name its own tmux session.
func (s *SessionsService) RenameSession(ctx context.Context, id, name string) (dispatch.SessionSummary, error) {
	if s.manager == nil || s.tmux == nil {
		return dispatch.SessionSummary{}, Errorf(KindUnavailable, "renaming sessions is unavailable")
	}
	if strings.TrimSpace(id) == "" {
		return dispatch.SessionSummary{}, Errorf(KindInvalid, "session id is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return dispatch.SessionSummary{}, Errorf(KindInvalid, "session name is required")
	}
	if err := dispatch.ValidateSessionName(name); err != nil {
		return dispatch.SessionSummary{}, Wrap(err, KindInvalid, "session name")
	}

	current, err := s.manager.SessionDetail(ctx, id)
	if err != nil {
		return dispatch.SessionSummary{}, Wrap(err, KindNotFound, "reading session %q", id)
	}
	slug := dispatch.SlugifySessionName(name)
	if err := s.assertSlugFree(ctx, id, slug); err != nil {
		return dispatch.SessionSummary{}, err
	}

	if err := s.tmux.RenameSession(ctx, current.Slug, slug); err != nil {
		return dispatch.SessionSummary{}, Wrap(err, KindConflict, "renaming the terminal session for %q", current.Name)
	}
	if err := s.manager.RenameSession(ctx, id, name); err != nil {
		if rollback := s.tmux.RenameSession(ctx, slug, current.Slug); rollback != nil {
			return dispatch.SessionSummary{}, Wrap(err, KindInternal, "renaming session %q (its terminal session is now named %q)", current.Name, slug)
		}
		return dispatch.SessionSummary{}, Wrap(err, KindInternal, "renaming session %q", current.Name)
	}

	renamed, err := s.manager.SessionDetail(ctx, id)
	if err != nil {
		return dispatch.SessionSummary{}, Wrap(err, KindInternal, "re-reading renamed session %q", id)
	}
	return dispatch.SessionSummary{
		ID:    renamed.ID,
		Name:  renamed.Name,
		Slug:  renamed.Slug,
		Repo:  renamed.Repo,
		State: renamed.State,
		Group: renamed.Group,
	}, nil
}

// SetSessionGroup sets, or with an empty group clears, a session's group.
func (s *SessionsService) SetSessionGroup(ctx context.Context, id, group string) error {
	if s.manager == nil {
		return Errorf(KindUnavailable, "grouping sessions is unavailable")
	}
	if strings.TrimSpace(id) == "" {
		return Errorf(KindInvalid, "session id is required")
	}
	return Wrap(s.manager.SetSessionGroup(ctx, id, strings.TrimSpace(group)), KindInternal, "setting the group for session %q", id)
}

// DeleteSession removes a session, its worktree or clone, and its tmux session,
// as a background job: it runs git and filesystem work that a caller must not
// block on. The caller is expected to have confirmed against SessionRisk first.
func (s *SessionsService) DeleteSession(ctx context.Context, id string) (int64, error) {
	if s.manager == nil || s.jobs == nil {
		return 0, Errorf(KindUnavailable, "deleting sessions is unavailable")
	}
	return s.destructiveJob(ctx, id, "Delete session", deleteSessionJobActionID, s.manager.DeleteSession)
}

// RecycleSession resets a session for reuse, as a background job for the same
// reason DeleteSession is one. For a worktree session hive recycles by
// deleting, which is what SessionRisk.RecycleDeletes warns about.
func (s *SessionsService) RecycleSession(ctx context.Context, id string) (int64, error) {
	if s.manager == nil || s.jobs == nil {
		return 0, Errorf(KindUnavailable, "recycling sessions is unavailable")
	}
	return s.destructiveJob(ctx, id, "Recycle session", recycleSessionJobActionID, s.manager.RecycleSession)
}

// PruneSessions deletes every recycled and corrupted session as a background
// job, and reports the job id rather than the count: the work outlives the
// call, so the count is not known yet.
func (s *SessionsService) PruneSessions(ctx context.Context) (int64, error) {
	if s.manager == nil || s.jobs == nil {
		return 0, Errorf(KindUnavailable, "pruning sessions is unavailable")
	}
	return s.jobs.Track(ctx, "Prune sessions", pruneSessionsJobActionID, "", func(bg context.Context) error {
		_, err := s.manager.PruneSessions(bg)
		return err
	}), nil
}

// destructiveJob resolves the session's name for the job label, then runs op in
// the background. The name is read up front because after the operation there
// may be no session left to read it from.
func (s *SessionsService) destructiveJob(ctx context.Context, id, label, actionID string, op func(context.Context, string) error) (int64, error) {
	if strings.TrimSpace(id) == "" {
		return 0, Errorf(KindInvalid, "session id is required")
	}
	detail, err := s.manager.SessionDetail(ctx, id)
	if err != nil {
		return 0, Wrap(err, KindNotFound, "reading session %q", id)
	}
	return s.jobs.Track(ctx, label, actionID, detail.Name, func(bg context.Context) error {
		return op(bg, id)
	}), nil
}

// assertSlugFree rejects a rename that would give two sessions the same slug.
// Hive validates the new name but does not check it for collisions, and the
// session table has no uniqueness constraint, so nothing upstream stops two
// sessions sharing one tmux session name and one directory slug.
func (s *SessionsService) assertSlugFree(ctx context.Context, id, slug string) error {
	sessions, err := s.manager.ListSessions(ctx)
	if err != nil {
		return Wrap(err, KindInternal, "checking existing session names")
	}
	for _, other := range sessions {
		if other.ID != id && other.Slug == slug {
			return Errorf(KindInvalid, "a session named %q already exists", other.Name)
		}
	}
	return nil
}
