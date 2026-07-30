package dispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/messaging"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/session"
	"github.com/hay-kot/hive-desktop/internal/hivecore/hive"
)

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

// HiveSessionLauncher adapts Hive's session service to SessionLauncher.
type HiveSessionLauncher struct {
	sessions SessionCreator
	recorder activity.Recorder
}

func NewHiveSessionLauncher(sessions SessionCreator) *HiveSessionLauncher {
	return &HiveSessionLauncher{sessions: sessions}
}

// SetRecorder attaches an activity recorder so created sessions surface in the
// Activity view. Optional: nil (the default) records nothing.
func (l *HiveSessionLauncher) SetRecorder(r activity.Recorder) { l.recorder = r }

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
	s, err := l.sessions.CreateSession(ctx, hive.CreateOptions{Name: req.Name, Prompt: req.Prompt, Remote: remote, Source: source, AgentKey: req.Agent, Background: true, UseBatchSpawn: false})
	if err != nil {
		if errors.Is(err, session.ErrDuplicateName) {
			return SessionExecutionOutcome{}, fmt.Errorf("%w: %w", ErrDuplicateSessionName, err)
		}
		return SessionExecutionOutcome{}, fmt.Errorf("create hive session: %w", err)
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
type HiveSessionManager struct{ sessions SessionManagement }

func NewHiveSessionManager(sessions SessionManagement) *HiveSessionManager {
	return &HiveSessionManager{sessions: sessions}
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
