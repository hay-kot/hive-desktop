package dispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/git"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/messaging"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/session"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/terminal"
	"github.com/hay-kot/hive-desktop/internal/hivecore/hive"
	"github.com/rs/zerolog"
)

// AgentActivityStatus is this app's own vocabulary for a captured tmux pane's
// detected state, projected from hive's vendored terminal.Status at this seam
// so no app signature carries a vendored type (Bounded Context,
// architecture.md). Values match terminal.Status's own strings.
type AgentActivityStatus string

const (
	// AgentActivityReady reports an input prompt: the agent is idle.
	AgentActivityReady AgentActivityStatus = AgentActivityStatus(terminal.StatusReady)
	// AgentActivityActive reports a busy indicator (spinner, "esc to interrupt").
	AgentActivityActive AgentActivityStatus = AgentActivityStatus(terminal.StatusActive)
	// AgentActivityApproval reports a permission prompt blocking on the user —
	// the highest-urgency state, per terminal.Detector's own IsBusy-wins,
	// NeedsApproval-before-IsReady precedence.
	AgentActivityApproval AgentActivityStatus = AgentActivityStatus(terminal.StatusApproval)
)

// ClassifyAgentScreen classifies a captured tmux pane's screen for the named
// agent CLI using terminal.NewDetector(agent).DetectStatus — the same
// capture-pane -> Detector path SessionStatuses/FetchBatch below already runs
// for hive's own sessions, so this is the input the detector was tuned
// against rather than a raw PTY ring tail (hc-alqns469 spiked the latter and
// found it does not classify).
func ClassifyAgentScreen(agent, screen string) AgentActivityStatus {
	return AgentActivityStatus(terminal.NewDetector(agent).DetectStatus(screen))
}

// ErrDuplicateSessionName is the seam-local translation of Hive's
// session.ErrDuplicateName, so a core service can classify a name collision
// without importing the vendored session package.
var ErrDuplicateSessionName = errors.New("session name already exists")

type SessionCreator interface {
	CreateSession(context.Context, hive.CreateOptions) (*session.Session, error)
}

type sessionLaunchOptionsSource interface {
	SessionLaunchOptions(context.Context) (hive.SessionLaunchOptions, error)
	ResolveSessionLaunchRepository(context.Context, string) (hive.SessionLaunchRepository, error)
}

// SessionManagement is the vendored session surface the desktop manages
// sessions through. Every method matches hive's SessionService structurally, so
// an upstream signature change breaks this file rather than the core.
type SessionManagement interface {
	ListSessions(context.Context) ([]session.Session, error)
	GetSession(context.Context, string) (session.Session, error)
	RenameSession(ctx context.Context, id, newName string) error
	DeleteSession(ctx context.Context, id string) error
	RecycleSession(ctx context.Context, id string, w io.Writer) error
	Prune(ctx context.Context, all bool) (int, error)
	CheckSessionRisk(ctx context.Context, id string) (hive.SessionRisk, error)
	OpenTmuxSession(ctx context.Context, name, path, remote, targetWindow string, background bool) error
}

// SessionStateActive is the one state with a live checkout behind it, and so the
// only one whose terminal can be started or attached to.
const SessionStateActive = string(session.StateActive)

type sessionStatusSource interface {
	Available() bool
	FetchBatch(context.Context, []*session.Session, []hive.RootRepoTarget) map[string]hive.TerminalStatus
}

// SessionSummary is one session as the desktop's session list sees it. Slug is
// the tmux session name, which is what a terminal attach targets. It stays a
// projection: the rest of a session is read on demand as a SessionDetail.
type SessionSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Repo  string `json:"repo"`
	State string `json:"state"`
}

// ItemSessionView is one hive session an inbox item spawned. Only CreatedAt
// comes from the link — everything else is read live from hive, so a session
// renamed or recycled outside this app reports what it actually is.
type ItemSessionView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Repo      string    `json:"repo"`
	State     string    `json:"state"`
	Running   bool      `json:"running"`
	CreatedAt time.Time `json:"createdAt"`
}

// SessionWindowStatus is one tmux window's detected agent activity.
type SessionWindowStatus struct {
	WindowID string `json:"windowId"`
	Status   string `json:"status"`
	Tool     string `json:"tool"`
}

// SessionStatus separates tmux liveness from the activity detected in each
// agent window.
type SessionStatus struct {
	SessionID string                `json:"sessionId"`
	Running   bool                  `json:"running"`
	Windows   []SessionWindowStatus `json:"windows"`
}

// SessionStatusSnapshot carries one poll result and the Hive-configured delay
// the caller should use before requesting the next one.
type SessionStatusSnapshot struct {
	Items        []SessionStatus
	PollInterval time.Duration
}

// SessionDetail is one session read in full, for a detail view.
type SessionDetail struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Repo           string    `json:"repo"`
	State          string    `json:"state"`
	Path           string    `json:"path"`
	CloneStrategy  string    `json:"cloneStrategy"`
	WorktreeBranch string    `json:"worktreeBranch"`
	Tags           []string  `json:"tags"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// SessionRisk is the pre-flight a destructive operation confirms against: what
// unsaved work the session holds, and whether recycling it is really a delete.
type SessionRisk struct {
	UncommittedChanges bool `json:"uncommittedChanges"`
	UnpushedCommits    bool `json:"unpushedCommits"`
	// RecycleDeletes reports that recycling this session destroys it: hive
	// routes a worktree session's recycle straight to DeleteSession, because a
	// worktree has no clone of its own to reset.
	RecycleDeletes bool `json:"recycleDeletes"`
}

// SessionGitStatus is one session's checkout as the session status bar reads
// it. Resolved separates "git answered" from the zero value, so a bar can tell
// a clean branch from a session it has not read yet or cannot read at all;
// Error carries why when a read failed, and is never a substitute for it.
type SessionGitStatus struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Dirty    bool   `json:"dirty"`
	Unpushed bool   `json:"unpushed"`
	// Additions and Deletions are lines against the default branch, not
	// against HEAD — the same figure hive's own session list shows.
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	// Owner and Repo are the session remote's GitHub coordinates, empty for a
	// remote that is not a GitHub one.
	Owner    string `json:"owner"`
	Repo     string `json:"repo"`
	Resolved bool   `json:"resolved"`
	Error    string `json:"error"`
}

// SessionPullRequestKey addresses the pull request a session's branch has.
// Git is what resolves the branch, so the key is built from a SessionGitStatus
// rather than read again.
type SessionPullRequestKey struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

// PullRequestStatus is why a session has no pull request to show, or that it
// does. The four are kept apart deliberately: a bar that renders "no pull
// request" for a failed lookup or a disconnected account tells the user the
// branch has none, which is a different and wrong fact.
type PullRequestStatus string

const (
	// PullRequestStatusNone reports a branch GitHub has no pull request for.
	PullRequestStatusNone PullRequestStatus = "none"
	// PullRequestStatusFound reports one, described by the rest of the view.
	PullRequestStatusFound PullRequestStatus = "found"
	// PullRequestStatusDisconnected reports that no GitHub account is
	// connected, so nothing was asked.
	PullRequestStatusDisconnected PullRequestStatus = "disconnected"
	// PullRequestStatusUnsupported reports a session whose remote is not a
	// GitHub one, or whose branch did not resolve.
	PullRequestStatusUnsupported PullRequestStatus = "unsupported"
)

// SessionPullRequest is the branch's pull request as the session status bar
// shows it. Everything below Status is meaningful only for
// PullRequestStatusFound.
type SessionPullRequest struct {
	Status  PullRequestStatus `json:"status"`
	Number  int               `json:"number"`
	Title   string            `json:"title"`
	State   string            `json:"state"`
	IsDraft bool              `json:"isDraft"`
	URL     string            `json:"url"`
	// ReviewDecision is GitHub's own vocabulary — APPROVED,
	// CHANGES_REQUESTED, REVIEW_REQUIRED — or empty when review is not
	// required.
	ReviewDecision string `json:"reviewDecision"`
	// Checks is passing, pending, failing, or empty for a head commit with no
	// checks configured.
	Checks string `json:"checks"`
	// Additions and Deletions are the pull request's own line counts, which are
	// deliberately not SessionGitStatus's: those measure the working tree
	// against the default branch and drift the moment the branch moves on.
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	// Cached reports that this answer came from the in-process cache rather
	// than the network, which is what lets a caller tell "this just arrived"
	// from "this was already known". The bar animates only the former.
	Cached bool `json:"cached"`
}

// ItemSessionLinker persists the association between an inbox item and a
// session created for it, so the item can find the session again after a
// restart. Consumer-defined: the launcher needs one write, not a store.
type ItemSessionLinker interface {
	LinkItemSession(ctx context.Context, sessionID string, ref store.ItemRef) error
}

// HiveSessionLauncher adapts Hive's session service to SessionLauncher.
type HiveSessionLauncher struct {
	sessions SessionCreator
	recorder activity.Recorder
	links    ItemSessionLinker
	logger   zerolog.Logger
}

func NewHiveSessionLauncher(sessions SessionCreator) *HiveSessionLauncher {
	return &HiveSessionLauncher{sessions: sessions}
}

// SetRecorder attaches an activity recorder so created sessions surface in the
// Activity view. Optional: nil (the default) records nothing.
func (l *HiveSessionLauncher) SetRecorder(r activity.Recorder) { l.recorder = r }

// SetItemSessionLinker attaches the store that remembers which item a session
// came from. Optional: nil (the default) links nothing, and the session is
// still created.
func (l *HiveSessionLauncher) SetItemSessionLinker(links ItemSessionLinker, logger zerolog.Logger) {
	l.links, l.logger = links, logger
}

func (l *HiveSessionLauncher) LaunchSession(ctx context.Context, req LaunchSessionRequest) (SessionExecutionOutcome, error) {
	if l.sessions == nil {
		return SessionExecutionOutcome{}, fmt.Errorf("launch session: hive session service is unavailable")
	}
	remote, source := req.Repo, ""
	if known, ok := l.sessions.(sessionLaunchOptionsSource); ok {
		repo, err := known.ResolveSessionLaunchRepository(ctx, req.Repo)
		if err != nil {
			return SessionExecutionOutcome{}, fmt.Errorf("resolve launch repository: %w", err)
		}
		remote, source = repo.Remote, repo.Source
	}
	// The tag is presentational, for a reader inside hive, and is never read
	// back — LinkItemSession below writes the association this app queries.
	linked := req.Origin.Known()
	var tags []string
	if linked {
		tags = []string{req.Origin.ExternalID}
	}
	s, err := l.sessions.CreateSession(ctx, hive.CreateOptions{Name: req.Name, Prompt: req.Prompt, Remote: remote, Source: source, AgentKey: req.Agent, Background: true, UseBatchSpawn: false, Tags: tags})
	if err != nil {
		if errors.Is(err, session.ErrDuplicateName) {
			return SessionExecutionOutcome{}, fmt.Errorf("%w: %w", ErrDuplicateSessionName, err)
		}
		return SessionExecutionOutcome{}, fmt.Errorf("create hive session: %w", err)
	}
	// The session exists either way, so a failed link is logged rather than
	// returned: reporting the launch as failed would be a lie, and would
	// invite a retry that creates a second session.
	if l.links != nil && linked {
		if linkErr := l.links.LinkItemSession(ctx, s.ID, req.Origin); linkErr != nil {
			l.logger.Warn().Err(linkErr).Str("session_id", s.ID).Msg("linking session to its inbox item")
		}
	}
	if l.recorder != nil {
		name := req.Name
		if s != nil && s.Name != "" {
			name = s.Name
		}
		l.recorder.Record(ctx, activity.SessionCreated(name, req.Agent, req.Repo))
	}
	return SessionExecutionOutcome{ID: s.ID, Name: s.Name}, nil
}

// SessionLaunchOptions exposes only labels, remotes, and configured agent keys
// to the desktop; local source paths remain in the Hive service.
func (l *HiveSessionLauncher) SessionLaunchOptions(ctx context.Context) (SessionLaunchOptions, error) {
	known, ok := l.sessions.(sessionLaunchOptionsSource)
	if !ok {
		return SessionLaunchOptions{}, fmt.Errorf("session launch options are unavailable")
	}
	options, err := known.SessionLaunchOptions(ctx)
	if err != nil {
		return SessionLaunchOptions{}, err
	}
	view := SessionLaunchOptions{
		DefaultRepository: options.DefaultRepository,
		Agents:            options.Agents,
		DefaultAgent:      options.DefaultAgent,
	}
	for _, repo := range options.Repositories {
		view.Repositories = append(view.Repositories, SessionLaunchRepository{Name: repo.Name, Repository: repo.Remote})
	}
	return view, nil
}

// HiveSessionManager adapts Hive's session service to the read and lifecycle
// operations the desktop's session list drives. It is separate from
// HiveSessionLauncher because launching is a dispatch action and managing is
// not: the launcher is what an output command reaches for, and widening it
// would hand every action executor a delete.
type HiveSessionManager struct {
	sessions           SessionManagement
	statuses           sessionStatusSource
	git                sessionGit
	statusPollInterval time.Duration
}

// sessionGit is the read-only slice of the vendored git executor a session's
// status needs. Narrowed here rather than taking git.Git whole so the seam
// cannot grow a Checkout or a ResetHard: nothing about reporting status should
// be able to move a worktree.
type sessionGit interface {
	Branch(ctx context.Context, dir string) (string, error)
	IsClean(ctx context.Context, dir string) (bool, error)
	HasUnpushedCommits(ctx context.Context, dir string) (bool, error)
	DiffStats(ctx context.Context, dir string) (additions, deletions int, err error)
}

// Compile-time proof the narrowed seam still matches the vendored executor.
var _ sessionGit = (git.Git)(nil)

func NewHiveSessionManager(sessions SessionManagement, statuses sessionStatusSource, gitExec sessionGit, statusPollInterval time.Duration) *HiveSessionManager {
	return &HiveSessionManager{sessions: sessions, statuses: statuses, git: gitExec, statusPollInterval: statusPollInterval}
}

// ListSessions returns every session, recycled and corrupted included: an
// unattachable session still has to be manageable, which is the whole point of
// listing it.
func (m *HiveSessionManager) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	sessions, err := m.sessions.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list hive sessions: %w", err)
	}
	out := make([]SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionSummaryOf(s))
	}
	return out, nil
}

// SessionStatuses projects Hive's terminal detection without exposing pane
// content or vendored status types beyond this anti-corruption layer.
func (m *HiveSessionManager) SessionStatuses(ctx context.Context) (SessionStatusSnapshot, error) {
	snapshot := SessionStatusSnapshot{
		Items:        []SessionStatus{},
		PollInterval: m.statusPollInterval,
	}
	if m.statuses == nil || !m.statuses.Available() {
		return snapshot, nil
	}

	sessions, err := m.sessions.ListSessions(ctx)
	if err != nil {
		return SessionStatusSnapshot{}, fmt.Errorf("list hive sessions for status: %w", err)
	}
	active := make([]*session.Session, 0, len(sessions))
	for i := range sessions {
		if sessions[i].State == session.StateActive {
			active = append(active, &sessions[i])
		}
	}
	statuses := m.statuses.FetchBatch(ctx, active, nil)
	for _, s := range active {
		status, ok := statuses[s.ID]
		if !ok {
			continue
		}
		item := SessionStatus{SessionID: s.ID, Running: status.Running, Windows: []SessionWindowStatus{}}
		if len(status.Windows) == 0 && status.WindowID != "" {
			item.Windows = append(item.Windows, SessionWindowStatus{
				WindowID: status.WindowID,
				Status:   string(status.Status),
				Tool:     status.Tool,
			})
		}
		for _, window := range status.Windows {
			item.Windows = append(item.Windows, SessionWindowStatus{
				WindowID: window.WindowID,
				Status:   string(window.Status),
				Tool:     window.Tool,
			})
		}
		snapshot.Items = append(snapshot.Items, item)
	}
	return snapshot, nil
}

// RunningSessions reports which of ids currently have a live tmux session. It
// is the narrow counterpart to SessionStatuses: an inbox item asks about the
// one or two sessions it spawned, and a full sweep would pay a tmux round trip
// for every active session in the install to answer that.
//
// An unavailable status source is data, not a failure — nothing is reported
// running, which is what "we cannot see tmux from here" honestly looks like.
func (m *HiveSessionManager) RunningSessions(ctx context.Context, ids []string) (map[string]bool, error) {
	running := map[string]bool{}
	if len(ids) == 0 || m.statuses == nil || !m.statuses.Available() {
		return running, nil
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	sessions, err := m.sessions.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list hive sessions for status: %w", err)
	}
	subset := make([]*session.Session, 0, len(ids))
	for i := range sessions {
		if _, ok := wanted[sessions[i].ID]; !ok || sessions[i].State != session.StateActive {
			continue
		}
		subset = append(subset, &sessions[i])
	}
	for id, status := range m.statuses.FetchBatch(ctx, subset, nil) {
		running[id] = status.Running
	}
	return running, nil
}

func (m *HiveSessionManager) SessionDetail(ctx context.Context, id string) (SessionDetail, error) {
	s, err := m.sessions.GetSession(ctx, id)
	if err != nil {
		return SessionDetail{}, fmt.Errorf("get hive session: %w", err)
	}
	return SessionDetail{
		ID:             s.ID,
		Name:           s.Name,
		Slug:           s.Slug,
		Repo:           s.Remote,
		State:          string(s.State),
		Path:           s.Path,
		CloneStrategy:  s.CloneStrategy,
		WorktreeBranch: s.GetMeta(session.MetaWorktreeBranch),
		Tags:           s.Tags,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}, nil
}

// SessionRisk reports what a delete or recycle of id would cost. Hive reports
// no risk for a non-active session, which is correct — there is no live clone
// left to hold unsaved work.
func (m *HiveSessionManager) SessionRisk(ctx context.Context, id string) (SessionRisk, error) {
	s, err := m.sessions.GetSession(ctx, id)
	if err != nil {
		return SessionRisk{}, fmt.Errorf("get hive session: %w", err)
	}
	risk, err := m.sessions.CheckSessionRisk(ctx, id)
	if err != nil {
		return SessionRisk{}, fmt.Errorf("check hive session risk: %w", err)
	}
	return SessionRisk{
		UncommittedChanges: risk.UncommittedChanges,
		UnpushedCommits:    risk.UnpushedCommits,
		RecycleDeletes:     s.CloneStrategy == session.CloneStrategyWorktree,
	}, nil
}

// SessionGitStatus reads one session's checkout: its branch, whether it is
// dirty, whether it has commits the remote does not, and the line delta
// against the default branch.
//
// Unlike SessionRisk it reports failures instead of assuming the risky answer.
// Hive treats an IsClean error as dirty because a delete confirmation must
// over-warn; a status badge that silently claims "dirty" on a git that never
// ran would be a lie the user acts on. Owner and Repo come from the remote so
// the caller can look the branch's pull request up without parsing it again.
func (m *HiveSessionManager) SessionGitStatus(ctx context.Context, id string) (SessionGitStatus, error) {
	s, err := m.sessions.GetSession(ctx, id)
	if err != nil {
		return SessionGitStatus{}, fmt.Errorf("get hive session: %w", err)
	}
	if s.State != session.StateActive || s.Path == "" {
		return SessionGitStatus{}, nil
	}
	if m.git == nil {
		return SessionGitStatus{Error: "git is unavailable"}, nil
	}

	owner, repo := gitHubCoordinates(s.Remote)
	status := SessionGitStatus{Path: s.Path, Owner: owner, Repo: repo}

	branch, err := m.git.Branch(ctx, s.Path)
	if err != nil {
		// Branch is the cheapest read and the one every other read needs a
		// working checkout for, so its failure stands for the whole status.
		return SessionGitStatus{Path: s.Path, Owner: owner, Repo: repo, Error: err.Error()}, nil
	}
	status.Branch = branch
	status.Resolved = true

	if clean, err := m.git.IsClean(ctx, s.Path); err == nil {
		status.Dirty = !clean
	} else {
		status.Error = err.Error()
	}
	if unpushed, err := m.git.HasUnpushedCommits(ctx, s.Path); err == nil {
		status.Unpushed = unpushed
	} else if status.Error == "" {
		// A worktree with no upstream and no origin/<default> reaches here on
		// every read; it is not worth displacing a real error above.
		status.Error = err.Error()
	}
	if additions, deletions, err := m.git.DiffStats(ctx, s.Path); err == nil {
		status.Additions, status.Deletions = additions, deletions
	} else if status.Error == "" {
		status.Error = err.Error()
	}
	return status, nil
}

// gitHubCoordinates reads owner and repo off a remote, and answers empty for
// one hosted anywhere but github.com. git.ExtractOwnerRepo is host-agnostic on
// purpose — every forge uses the same path shape — so the host check is what
// keeps a Gitea session from being looked up against GitHub's API.
func gitHubCoordinates(remote string) (owner, repo string) {
	if git.ExtractHost(remote) != "github.com" {
		return "", ""
	}
	return git.ExtractOwnerRepo(remote)
}

// SpawnTmuxSession creates the tmux session for a session hive already holds,
// from hive's own spawn configuration for the remote — the windows, working
// directory and commands hive would have used itself. A session tmux already
// has is left alone, so this is idempotent.
//
// It spawns detached: the desktop attaches over control mode, and an attaching
// spawn would hand the session to whatever terminal launched the app — or fail
// for a launcher that has none.
func (m *HiveSessionManager) SpawnTmuxSession(ctx context.Context, name, path, repo string) error {
	if err := m.sessions.OpenTmuxSession(ctx, name, path, repo, "", true); err != nil {
		return fmt.Errorf("open hive tmux session: %w", err)
	}
	return nil
}

func (m *HiveSessionManager) RenameSession(ctx context.Context, id, name string) error {
	if err := m.sessions.RenameSession(ctx, id, name); err != nil {
		return fmt.Errorf("rename hive session: %w", err)
	}
	return nil
}

func (m *HiveSessionManager) DeleteSession(ctx context.Context, id string) error {
	if err := m.sessions.DeleteSession(ctx, id); err != nil {
		return fmt.Errorf("delete hive session: %w", err)
	}
	return nil
}

// RecycleSession discards the recycle commands' output. The commands are the
// user's own (hive's recycle_commands), and the job records whether they
// succeeded; streaming their stdout would need a job log to stream into.
func (m *HiveSessionManager) RecycleSession(ctx context.Context, id string) error {
	if err := m.sessions.RecycleSession(ctx, id, io.Discard); err != nil {
		return fmt.Errorf("recycle hive session: %w", err)
	}
	return nil
}

// PruneSessions deletes every recycled and corrupted session. Hive's Prune also
// has a mode that only trims each pool back to max_recycled; that is a config
// reconciliation, not something a menu entry can honestly name, so the desktop
// exposes the unambiguous one.
func (m *HiveSessionManager) PruneSessions(ctx context.Context) (int, error) {
	count, err := m.sessions.Prune(ctx, true)
	if err != nil {
		return count, fmt.Errorf("prune hive sessions: %w", err)
	}
	return count, nil
}

func sessionSummaryOf(s session.Session) SessionSummary {
	return SessionSummary{
		ID:    s.ID,
		Name:  s.Name,
		Slug:  s.Slug,
		Repo:  s.Remote,
		State: string(s.State),
	}
}

// SlugifySessionName converts a display name to the slug Hive uses for
// tmux session names and directory paths. It wraps the vendored
// session.Slugify so that launch_session_executor.go — not itself an ACL
// seam — never imports internal/hivecore directly; an upstream rename here
// breaks this one file instead of spreading to a non-seam caller.
func SlugifySessionName(name string) string {
	return session.Slugify(name)
}

// ValidateSessionName validates name against Hive's session naming rules.
// See SlugifySessionName for why this wraps the vendored session.ValidateName
// instead of letting callers import internal/hivecore/core/session directly.
func ValidateSessionName(name string) error {
	return session.ValidateName(name)
}

type DurableMessageService interface {
	Publish(context.Context, messaging.Message, []string) (messaging.PublishResult, error)
}
type HiveMessagePublisher struct{ messages DurableMessageService }

func NewHiveMessagePublisher(messages DurableMessageService) *HiveMessagePublisher {
	return &HiveMessagePublisher{messages: messages}
}

func (p *HiveMessagePublisher) PublishMessage(ctx context.Context, payload, topic string) (string, error) {
	if p.messages == nil {
		return "", fmt.Errorf("publish message: hive message service is unavailable")
	}
	result, err := p.messages.Publish(ctx, messaging.Message{Payload: payload, Sender: "hive-desktop", SessionID: ""}, []string{topic})
	if err != nil {
		return "", fmt.Errorf("publish message: %w", err)
	}
	if len(result.Topics) != 1 || result.Topics[0] != topic {
		return "", fmt.Errorf("publish message: expected exact topic %q, got %v", topic, result.Topics)
	}
	return result.Topics[0], nil
}
