package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/colonyops/hive/pkg/osopen"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/stores"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/execenv"
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
	DeleteSession(ctx context.Context, id string) error
	RecycleSession(ctx context.Context, id string) error
	PruneSessions(ctx context.Context) (int, error)
	SpawnTmuxSession(ctx context.Context, name, path, repo string) error
}

type sessionStatusSource interface {
	SessionStatuses(context.Context) (dispatch.SessionStatusSnapshot, error)
	RunningSessions(ctx context.Context, ids []string) (map[string]bool, error)
}

// sessionGitSource reads a session's checkout. Its own interface rather than a
// tenth method on sessionManager: reading git is not part of managing a
// session's lifecycle.
type sessionGitSource interface {
	SessionGitStatus(ctx context.Context, id string) (dispatch.SessionGitStatus, error)
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

type inboxItemRefReader interface {
	RefByID(ctx context.Context, itemID int64) (models.ItemRef, error)
}

// itemSessionStore owns durable item-session associations and removes links
// to sessions Hive no longer has.
type itemSessionStore interface {
	List(ctx context.Context, ref models.ItemRef) ([]stores.ItemSession, error)
	Unlink(ctx context.Context, sessionIDs []string) error
}

// SessionsService is the desktop's session surface: the New Session form's
// launch path, read plus lifecycle management of the sessions that exist, and
// the configured actions a terminal session or window offers.
type SessionsService struct {
	launcher   sessionLauncher
	manager    sessionManager
	statuses   sessionStatusSource
	git        sessionGitSource
	tmux       sessionTmux
	jobs       sessionJobRunner
	items      inboxItemRefReader
	links      itemSessionStore
	catalog    *actions.ActionStore
	dispatcher *dispatch.Dispatcher
	recorder   activity.Recorder
	// pullRequests is nil in a build with no GitHub client, which reads as
	// disconnected.
	pullRequests  *sessionPullRequests
	execEnv       *execenv.Resolver
	editorCommand EditorCommandReader
	// defaultAgentEnv is never nil: newSessionsService substitutes
	// NopDefaultAgentReader, so withEnvironmentDefaultAgent never guards it.
	defaultAgentEnv DefaultAgentReader
	logger          zerolog.Logger
}

// DefaultAgentReader reads HIVE_DEFAULT_AGENT from the user's resolved
// terminal environment.
type DefaultAgentReader interface {
	DefaultAgent(ctx context.Context) string
}

// NopDefaultAgentReader leaves Hive's configured agent unchanged.
type NopDefaultAgentReader struct{}

func (NopDefaultAgentReader) DefaultAgent(context.Context) string { return "" }

type NopEditorCommandReader struct{}

func (NopEditorCommandReader) Editor(context.Context) (string, error) { return "", nil }

type SessionsDeps struct {
	Launcher     sessionLauncher
	Manager      sessionManager
	Statuses     sessionStatusSource
	Git          sessionGitSource
	Tmux         sessionTmux
	Jobs         sessionJobRunner
	Items        inboxItemRefReader
	Links        itemSessionStore
	Catalog      *actions.ActionStore
	Dispatcher   *dispatch.Dispatcher
	Recorder     activity.Recorder
	PullRequests *sessionPullRequests
	ExecEnv      *execenv.Resolver
	// EditorCommand reads the configured editor from settings on every call,
	// so a settings change applies without restarting. nil means
	// NopEditorCommandReader.
	EditorCommand EditorCommandReader
	// DefaultAgentEnv reads HIVE_DEFAULT_AGENT the way the user's terminal
	// would. nil means NopDefaultAgentReader.
	DefaultAgentEnv DefaultAgentReader
	Logger          zerolog.Logger
}

func newSessionsService(d SessionsDeps) *SessionsService {
	if d.EditorCommand == nil {
		d.EditorCommand = NopEditorCommandReader{}
	}
	if d.DefaultAgentEnv == nil {
		d.DefaultAgentEnv = NopDefaultAgentReader{}
	}
	return &SessionsService{
		launcher:        d.Launcher,
		manager:         d.Manager,
		statuses:        d.Statuses,
		git:             d.Git,
		tmux:            d.Tmux,
		jobs:            d.Jobs,
		items:           d.Items,
		links:           d.Links,
		catalog:         d.Catalog,
		dispatcher:      d.Dispatcher,
		recorder:        d.Recorder,
		pullRequests:    d.PullRequests,
		execEnv:         d.ExecEnv,
		editorCommand:   d.EditorCommand,
		defaultAgentEnv: d.DefaultAgentEnv,
		logger:          d.Logger,
	}
}

// SessionLaunchOptions supplies the configured repository and agent choices the
// New Session form presents.
func (s *SessionsService) SessionLaunchOptions(ctx context.Context) (dispatch.SessionLaunchOptions, error) {
	opts, err := s.launcher.SessionLaunchOptions(ctx)
	if err != nil {
		return dispatch.SessionLaunchOptions{}, Wrap(err, KindInternal, "resolving session launch options")
	}
	return s.withEnvironmentDefaultAgent(ctx, opts), nil
}

// withEnvironmentDefaultAgent preselects the agent hive itself would run:
// HIVE_DEFAULT_AGENT when it names a configured profile, otherwise the
// agents.default hive's config already resolved (colonyops/hive#365). The
// variable comes from the user's terminal rather than this process, because a
// launched .app has neither it nor anything else a startup file exports
// (ADR hive-env-overrides-resolve-through-the-login-shell).
//
// An unknown profile is ignored rather than refused: preselecting is all this
// does, and hive validates the agent it is handed.
func (s *SessionsService) withEnvironmentDefaultAgent(ctx context.Context, opts dispatch.SessionLaunchOptions) dispatch.SessionLaunchOptions {
	if preferred := s.preferredEnvAgent(ctx, opts.Agents); preferred != "" {
		opts.DefaultAgent = preferred
	}
	return opts
}

// preferredEnvAgent answers HIVE_DEFAULT_AGENT when it names one of the given
// configured profiles, otherwise "". Both the form (withEnvironmentDefaultAgent)
// and the launch path (resolveLaunchAgent) preselect the same env override
// against the same profile list, so the check lives here once.
//
// An unknown profile answers "" rather than the raw env value: hive refuses a
// LaunchSessionRequest.Agent it does not recognize with `unknown agent %q`,
// which would turn a harmless preselection into a failed launch.
func (s *SessionsService) preferredEnvAgent(ctx context.Context, agents []string) string {
	preferred := strings.TrimSpace(s.defaultAgentEnv.DefaultAgent(ctx))
	if preferred == "" || !slices.Contains(agents, preferred) {
		return ""
	}
	return preferred
}

// resolveLaunchAgent answers the agent a launch should run when the request
// names none: HIVE_DEFAULT_AGENT when it names a configured profile,
// otherwise "" so hive resolves its own agents.default. A launch-options read
// failure also falls through to "": a create must not start failing because a
// preselection could not be read.
//
// This runs synchronously on the click that starts a session
// (CreateSession builds the request before handing off to the job), so the
// options read -- a git subprocess per configured workspace
// (desktop/frontend/src/composables/useNewSession.ts) -- is gated on there
// being an override to validate. The env read is a cached map lookup, so
// checking it first is free for the common case of no override at all.
func (s *SessionsService) resolveLaunchAgent(ctx context.Context, requested string) string {
	if trimmed := strings.TrimSpace(requested); trimmed != "" {
		return trimmed
	}
	if strings.TrimSpace(s.defaultAgentEnv.DefaultAgent(ctx)) == "" {
		return ""
	}
	opts, err := s.launcher.SessionLaunchOptions(ctx)
	if err != nil {
		return ""
	}
	return s.preferredEnvAgent(ctx, opts.Agents)
}

// ListSessions returns every session, whatever its state. Only an active one
// can be attached to — Slug is its tmux session name — but a recycled or
// corrupted session still has to be readable and deletable.
func (s *SessionsService) ListSessions(ctx context.Context) ([]dispatch.SessionSummary, error) {
	sessions, err := s.manager.ListSessions(ctx)
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing sessions")
	}
	return sessions, nil
}

// SessionStatuses detects the live agent state for active sessions. Missing or
// unavailable terminals are data, so only a failure to read the session set
// fails the request.
func (s *SessionsService) SessionStatuses(ctx context.Context) (dispatch.SessionStatusSnapshot, error) {
	statuses, err := s.statuses.SessionStatuses(ctx)
	if err != nil {
		return dispatch.SessionStatusSnapshot{}, Wrap(err, KindInternal, "reading session status")
	}
	return statuses, nil
}

// ItemSessions returns the hive sessions an inbox item spawned, newest first,
// joined to the state hive reports for them now. The read is also what
// reconciles (ADR macos-dmg-installer).
func (s *SessionsService) ItemSessions(ctx context.Context, itemID int64) ([]dispatch.ItemSessionView, error) {
	ref, err := s.items.RefByID(ctx, itemID)
	if err != nil {
		if stores.IsNotFound(err) {
			return nil, Wrap(err, KindNotFound, "inbox item %d not found", itemID)
		}
		return nil, Wrap(err, KindInternal, "reading inbox item %d", itemID)
	}
	links, err := s.links.List(ctx, ref)
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing sessions for item %d", itemID)
	}
	if len(links) == 0 {
		return []dispatch.ItemSessionView{}, nil
	}

	sessions, err := s.manager.ListSessions(ctx)
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing sessions")
	}
	known := make(map[string]dispatch.SessionSummary, len(sessions))
	for _, summary := range sessions {
		known[summary.ID] = summary
	}

	views := make([]dispatch.ItemSessionView, 0, len(links))
	ids := make([]string, 0, len(links))
	var gone []string
	for _, link := range links {
		summary, ok := known[link.SessionID]
		if !ok {
			gone = append(gone, link.SessionID)
			continue
		}
		ids = append(ids, summary.ID)
		views = append(views, dispatch.ItemSessionView{
			ID:        summary.ID,
			Name:      summary.Name,
			Slug:      summary.Slug,
			Repo:      summary.Repo,
			State:     summary.State,
			CreatedAt: time.UnixMilli(link.CreatedAt),
		})
	}
	if err := s.links.Unlink(ctx, gone); err != nil {
		// The view above is already correct without the prune; failing the read
		// over a cleanup would hide the sessions that do still exist.
		s.logger.Warn().Err(err).Int64("item_id", itemID).Msg("dropping links to deleted sessions")
	}

	running, err := s.statuses.RunningSessions(ctx, ids)
	if err != nil {
		// Same reason as the prune above: the views are already correct
		// without liveness, and failing the read blanks a pane that had an
		// answer.
		s.logger.Warn().Err(err).Int64("item_id", itemID).Msg("reading session status")
		return views, nil
	}
	for i := range views {
		views[i].Running = running[views[i].ID]
	}
	return views, nil
}

// SessionDetail reads one session in full.
func (s *SessionsService) SessionDetail(ctx context.Context, id string) (dispatch.SessionDetail, error) {
	if strings.TrimSpace(id) == "" {
		return dispatch.SessionDetail{}, Errorf(KindInvalid, "session id is required")
	}
	detail, err := s.manager.SessionDetail(ctx, id)
	if err != nil {
		return dispatch.SessionDetail{}, Wrap(err, KindNotFound, "reading session %q", id)
	}
	return detail, nil
}

// SessionGitStatus reads the session's checkout for the status bar: branch,
// dirty, unpushed, and the line delta against the default branch. A session
// with no live checkout answers an unresolved status rather than an error —
// there is nothing wrong, there is just nothing to read.
func (s *SessionsService) SessionGitStatus(ctx context.Context, id string) (dispatch.SessionGitStatus, error) {
	if strings.TrimSpace(id) == "" {
		return dispatch.SessionGitStatus{}, Errorf(KindInvalid, "session id is required")
	}
	status, err := s.git.SessionGitStatus(ctx, id)
	if err != nil {
		return dispatch.SessionGitStatus{}, Wrap(err, KindNotFound, "reading git status for session %q", id)
	}
	return status, nil
}

// SessionPullRequest resolves the pull request for a branch SessionGitStatus
// already reported. The branch is passed rather than read again: resolving it
// costs a git subprocess the caller has just paid for, and the two halves
// refresh on different cadences.
func (s *SessionsService) SessionPullRequest(ctx context.Context, key dispatch.SessionPullRequestKey, refresh bool) (dispatch.SessionPullRequest, error) {
	if s.pullRequests == nil {
		return dispatch.SessionPullRequest{Status: dispatch.PullRequestStatusDisconnected}, nil
	}
	return s.pullRequests.Lookup(ctx, key, refresh)
}

// OpenSessionInEditor launches the configured editor on the session's
// checkout, detached. It takes a session id, never a path: launching a
// configured program on a caller-supplied directory is not this API's to offer.
func (s *SessionsService) OpenSessionInEditor(ctx context.Context, id string) error {
	dir, err := s.sessionCheckout(ctx, id)
	if err != nil {
		return err
	}
	command := ""
	if configured, err := s.editorCommand.Editor(ctx); err == nil {
		command = configured
	}
	return launchEditor(ctx, s.execEnv, command, dir)
}

// RevealSession opens the session's checkout in the OS file manager.
func (s *SessionsService) RevealSession(ctx context.Context, id string) error {
	dir, err := s.sessionCheckout(ctx, id)
	if err != nil {
		return err
	}
	return Wrap(osopen.Open(dir), KindInternal, "opening %s", dir)
}

// sessionCheckout resolves the directory a session's own record names, which
// is what keeps open and reveal off arbitrary paths.
func (s *SessionsService) sessionCheckout(ctx context.Context, id string) (string, error) {
	detail, err := s.SessionDetail(ctx, id)
	if err != nil {
		return "", err
	}
	if detail.Path == "" {
		return "", Errorf(KindConflict, "session %q has no checkout", detail.Name)
	}
	return detail.Path, nil
}

// SessionRisk reports the work a delete or recycle of id would discard, so the
// caller can name it in its confirmation instead of guessing.
func (s *SessionsService) SessionRisk(ctx context.Context, id string) (dispatch.SessionRisk, error) {
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

	// A form drafted from an item links the session back to it. An item that
	// has gone (pruned between opening the form and submitting it) launches
	// unlinked rather than refusing the session the user asked for.
	var origin models.ItemRef
	if req.ItemID != 0 {
		resolved, err := s.items.RefByID(ctx, req.ItemID)
		if err != nil && !stores.IsNotFound(err) {
			return 0, Wrap(err, KindInternal, "reading inbox item %d", req.ItemID)
		}
		origin = resolved
	}

	launch := dispatch.LaunchSessionRequest{
		Name:   name,
		Prompt: strings.TrimSpace(req.Prompt),
		Agent:  s.resolveLaunchAgent(ctx, req.Agent),
		Repo:   repo,
		Origin: origin,
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
	}, nil
}

// StartTmuxSession spawns the tmux session a slug names. A session hive knows
// about does not have to have one — its tmux server was restarted, the machine
// rebooted, or it was created by something that never spawned one — and this is
// what lets the terminal attach to it anyway.
//
// The windows are hive's: what a session's terminal holds is its spawn
// configuration for the remote, and a second definition of that here would
// drift from the one `hive` itself uses. Spawning a session tmux already has is
// a no-op, so this is safe to call before every cold attach.
func (s *SessionsService) StartTmuxSession(ctx context.Context, slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return Errorf(KindInvalid, "session slug is required")
	}

	detail, err := s.detailBySlug(ctx, slug)
	if err != nil {
		return err
	}
	if detail.State != dispatch.SessionStateActive {
		return Errorf(KindConflict, "session %q is %s, so there is no checkout left to open a terminal in", detail.Name, detail.State)
	}
	// Hive spawns under the slug it derives from the name, so a record whose two
	// disagree would create a tmux session nothing is attaching to (ADR session-rename-keeps-slug-and-tmux-in-step).
	if spawned := dispatch.SlugifySessionName(detail.Name); spawned != slug {
		return Errorf(KindConflict, "session %q would start as %q, not %q", detail.Name, spawned, slug)
	}
	if err := s.manager.SpawnTmuxSession(ctx, detail.Name, detail.Path, detail.Repo); err != nil {
		return Wrap(err, KindInternal, "starting the terminal session for %q", detail.Name)
	}
	return nil
}

// SessionDirectory answers the checkout a slug's terminal should open in. A
// session with no checkout left is a conflict rather than an empty path, so a
// terminal is never opened somewhere the caller did not ask for.
func (s *SessionsService) SessionDirectory(ctx context.Context, slug string) (string, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", Errorf(KindInvalid, "session slug is required")
	}
	detail, err := s.detailBySlug(ctx, slug)
	if err != nil {
		return "", err
	}
	if detail.State != dispatch.SessionStateActive {
		return "", Errorf(KindConflict, "session %q is %s, so there is no checkout left to open a terminal in", detail.Name, detail.State)
	}
	return detail.Path, nil
}

// detailBySlug reads the session a tmux session name belongs to. The listing is
// what maps a slug to an id; nothing queries hive by slug.
func (s *SessionsService) detailBySlug(ctx context.Context, slug string) (dispatch.SessionDetail, error) {
	sessions, err := s.manager.ListSessions(ctx)
	if err != nil {
		return dispatch.SessionDetail{}, Wrap(err, KindInternal, "listing sessions")
	}
	for _, summary := range sessions {
		if summary.Slug != slug {
			continue
		}
		detail, err := s.manager.SessionDetail(ctx, summary.ID)
		if err != nil {
			return dispatch.SessionDetail{}, Wrap(err, KindNotFound, "reading session %q", summary.Name)
		}
		return detail, nil
	}
	return dispatch.SessionDetail{}, Errorf(KindNotFound, "no session named %q", slug)
}

// DeleteSession removes a session, its worktree or clone, and its tmux session,
// as a background job: it runs git and filesystem work that a caller must not
// block on. The caller is expected to have confirmed against SessionRisk first.
func (s *SessionsService) DeleteSession(ctx context.Context, id string) (int64, error) {
	return s.destructiveJob(ctx, id, "Delete session", deleteSessionJobActionID, s.manager.DeleteSession)
}

// RecycleSession resets a session for reuse, as a background job for the same
// reason DeleteSession is one. For a worktree session hive recycles by
// deleting, which is what SessionRisk.RecycleDeletes warns about.
func (s *SessionsService) RecycleSession(ctx context.Context, id string) (int64, error) {
	return s.destructiveJob(ctx, id, "Recycle session", recycleSessionJobActionID, s.manager.RecycleSession)
}

// PruneSessions deletes every recycled and corrupted session as a background
// job, and reports the job id rather than the count: the work outlives the
// call, so the count is not known yet.
func (s *SessionsService) PruneSessions(ctx context.Context) (int64, error) {
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
