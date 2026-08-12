package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/colonyops/hive/pkg/osopen"
	"github.com/google/uuid"

	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/execenv"
	"github.com/hay-kot/hive-desktop/internal/app/mcpcatalog"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/app/tmuxcc"
)

const (
	// agentSessionPrefix names every tmux session an agent workspace session
	// owns (sessionName below). It is also the service-level concurrency
	// cap's counting key: the cap moved here from ptyterm's own
	// maxConcurrentSessions when sessions stopped being ptyterm terminals.
	agentSessionPrefix = "agentws-"
	// maxConcurrentAgentSessions bounds live agentws-* tmux sessions across
	// every workspace. Each session is an agent CLI spawning its own copy of
	// every enabled MCP server, so with no idle reaping and no cap this is a
	// fork bomb with a progress bar (ADR ptyterm-terminals-are-caller-addressed point 4, carried over from
	// ptyterm to this service now that sessions outlive the app on purpose).
	maxConcurrentAgentSessions = 8

	// earlyExitWindow is how long a just-created tmux session gets to report
	// that its initial command already exited before StartSession/
	// ResumeSession report it as live. tmux ends a session when its initial
	// pane's command exits (remain-on-exit is not set), so a command not on
	// PATH or an agent CLI that fails to start reads as the session simply
	// vanishing -- usually within milliseconds, well inside this window even
	// accounting for a slow CI machine sourcing shell startup files. A real
	// agent CLI never exits this fast, so the window never misclassifies a
	// live session as dead.
	earlyExitWindow       = 500 * time.Millisecond
	earlyExitPollInterval = 25 * time.Millisecond
)

// AgentWorkspacesService opens agent workspaces and drives the sessions run
// inside them: an agent CLI in a tmux session named agentws-<record id>,
// resolved through the launch table in agentws (autonomy flags, MCP wiring,
// session/resume args) and addressed by a durable store.AgentWorkspaceSession
// record. Sessions are tmux's, not this process's -- they outlive App.Close
// by design, which is what makes reopening a codex session (no resume form)
// a real reattach instead of a fresh relaunch.
type AgentWorkspacesService struct {
	store     *agentws.Store
	terminals *tmuxcc.Manager
	db        *store.DB
	skills    *SkillsService
	// commands is agentCommands' result (app.go): hive's configured agent
	// profiles projected onto their bare command, with Flags dropped at the
	// seam (ADR a-workspace-declares-its-own-authority). An agent key absent here is unknown to hive at all;
	// present here but absent from agentws's launch table is the second,
	// distinct refusal.
	commands map[string]string
	// rootProblem carries EnsureRoot's error, verbatim, when the configured
	// root could not be created or opened at startup -- empty otherwise.
	rootProblem string
	// execEnv resolves the PATH and environment the editor launch runs with
	// (ADR subprocess-environment) — a desktop launch's own environment cannot find a CLI a
	// package manager installed.
	execEnv *execenv.Resolver
	// editorCommand reads the configured editor from settings on every call,
	// so a settings change applies without restarting. Empty means none
	// configured.
	editorCommand func(context.Context) (string, error)
	// mcpEndpoint reads this run's own MCP endpoint URL, empty when the
	// loopback server is down. Read per call rather than captured, because the
	// listener's port is not known when this service is built and can change
	// if it rebinds.
	mcpEndpoint func(context.Context) string
}

func newAgentWorkspacesService(store *agentws.Store, terminals *tmuxcc.Manager, db *store.DB, skills *SkillsService, commands map[string]string, rootProblem string, execEnv *execenv.Resolver, editorCommand func(context.Context) (string, error), mcpEndpoint func(context.Context) string) *AgentWorkspacesService {
	return &AgentWorkspacesService{store: store, terminals: terminals, db: db, skills: skills, commands: commands, rootProblem: rootProblem, execEnv: execEnv, editorCommand: editorCommand, mcpEndpoint: mcpEndpoint}
}

// catalogue is the merged catalogue with this install's own entry resolved.
// mcpcatalog ships hive-desktop carrying no URL, because the loopback port is
// allocated at startup (Descriptor.RuntimeURL), so the live endpoint is
// substituted here — and an entry that cannot be resolved reports why rather
// than rendering an address nothing answers, the same posture problemFor takes
// for a command that is not on PATH.
func (s *AgentWorkspacesService) catalogue(ctx context.Context) []agentws.CatalogueEntry {
	entries := agentws.Catalogue(ctx, s.store.Library().Library, s.lookPath())
	endpoint := ""
	if s.mcpEndpoint != nil {
		endpoint = s.mcpEndpoint(ctx)
	}
	for i, entry := range entries {
		descriptor, ok := mcpcatalog.Lookup(entry.ID)
		// Shipped is false for a user entry shadowing this id, and the user's
		// own URL is then what they asked for.
		if !ok || !entry.Shipped || !descriptor.RuntimeURL {
			continue
		}
		if endpoint == "" {
			entries[i].Problem = "the local HTTP server is not running; set http.enabled in settings.yaml"
			continue
		}
		entries[i].Server.URL = endpoint
	}
	return entries
}

// lookPath is what the catalogue validates a stdio command against: the PATH
// a session launches with (ADR subprocess-environment), not the launchd one a
// desktop process inherits. Only a test builds this service without a
// resolver, and nil leaves it on this process's own PATH.
func (s *AgentWorkspacesService) lookPath() func(context.Context, string) (string, error) {
	if s.execEnv == nil {
		return nil
	}
	return s.execEnv.LookPath
}

// WorkspaceView is one row of the area's list. Autonomy is on it because a
// workspace that can actuate the physical world says so where it is opened,
// not where it was configured (spec §7.2).
type WorkspaceView struct {
	Dir      string   `json:"dir"`
	Name     string   `json:"name"`
	Agent    string   `json:"agent"`
	Autonomy string   `json:"autonomy"`
	MCPs     []string `json:"mcps"`
	Skills   []string `json:"skills"`
	Problem  string   `json:"problem"`
	// Notice mirrors SessionView.Notice's MCP explanation, shown on the
	// workspace row itself: an agent whose wiring cannot bound its tool set to
	// what the workspace declares says so before any session is even started
	// (spec §7.2, ADR a-workspace-declares-its-own-authority).
	Notice string `json:"notice"`
}

// SessionView is one row of a workspace's session list.
type SessionView struct {
	ID           int64  `json:"id"`
	Workspace    string `json:"workspace"`
	Name         string `json:"name"`
	Agent        string `json:"agent"`
	LastOpenedAt int64  `json:"lastOpenedAt"`
	// Slug is the tmux session name (agentws-<id>) this session is addressed by
	// whether or not it is running, so a caller can key a row, a route or an
	// attach pool on it without deriving the name itself. TerminalID, not this,
	// is what reports liveness.
	Slug string `json:"slug"`
	// TerminalID is the tmux session name (agentws-<id>) a live session rides,
	// addressed on the same tmux stream terminal mode uses (ADR terminal-transport); empty
	// when nothing is running.
	TerminalID string `json:"terminalId"`
	// WindowID is TerminalID's active tmux window at the moment this session
	// was attached -- the tmux wire frames output and input by window id,
	// unlike the flat ptyterm wire sessions rode before. It is set only by a
	// call that attaches (StartSession, ResumeSession); a listing read
	// (Sessions, Open) leaves it empty even for a live session, since nothing
	// there attaches.
	WindowID string `json:"windowId"`
	// Cols and Rows are tmux's own size for that window at attach — whichever
	// attached client tmux's window-size option picked, not necessarily the
	// caller's cols/rows vote. The pane must open its grid at this size or the
	// TUI's cursor-addressed output tears; 0 means tmux has not reported one.
	// Set only by a call that attaches, like WindowID.
	Cols int `json:"cols"`
	Rows int `json:"rows"`
	// ResumeAttempted is false when this launch could not even try to resume —
	// the agent has no resume form, or it provably persisted nothing under the
	// session's id to resume. It is deliberately not named Resumed: Hive sees
	// only pane content, so a pruned agent history would report a successful
	// resume while the agent prints its own error in the pane, which is the
	// "pretending it was" spec §6.2 forbids.
	ResumeAttempted bool `json:"resumeAttempted"`
	// Notice carries a fresh-launch, unbounded-MCP or missing-MCP explanation.
	Notice string `json:"notice"`
}

// SessionActivityItem is one live session's detected activity, keyed by
// session id so the caller does not have to re-derive a tmux session name.
type SessionActivityItem struct {
	ID int64 `json:"id"`
	// Status is one of dispatch.AgentActivityStatus's values: ready, active,
	// or approval -- approval is the highest-urgency state.
	Status string `json:"status"`
}

// StartSession is one new session's launch.
type StartSession struct {
	Workspace string
	Name      string
	Cols      int
	Rows      int
}

// OpenResult is what opening a workspace reports back to the UI.
type OpenResult struct {
	Workspace   WorkspaceView
	Sessions    []SessionView
	MissingMCPs []string
	// MissingPackages names skill packages the workspace enables that
	// skills.yml does not define.
	MissingPackages []string
}

// Available reports tmux availability -- the same axis terminal mode reports
// (TerminalsService.Available), since a session is now a tmux session rather
// than a ptyterm one.
func (s *AgentWorkspacesService) Available(ctx context.Context) error {
	return terminalError(s.terminals.Available(ctx),
		"agent workspaces need tmux 3.2 or newer. Hive searches PATH and the usual install prefixes; set paths.tmux in settings.yaml if yours is elsewhere.")
}

// RootProblem reports why the configured root could not be created or
// opened at startup, or "" when it is fine. Spec §14: a root on an unmounted
// volume or a signed-out iCloud Drive is reported rather than silently
// replaced with a second, empty root elsewhere -- the UI is what points at
// the setting to fix.
func (s *AgentWorkspacesService) RootProblem(context.Context) string {
	return s.rootProblem
}

// List returns every recognized workspace, valid or not — a broken manifest
// carries its last-good content plus Problem, rather than vanishing.
func (s *AgentWorkspacesService) List(context.Context) ([]WorkspaceView, error) {
	statuses := s.store.Statuses()
	views := make([]WorkspaceView, 0, len(statuses))
	for _, st := range statuses {
		views = append(views, workspaceView(st))
	}
	return views, nil
}

// Open regenerates the workspace's disposable artifacts and returns its
// sessions. It is the only entry point that writes into a workspace, and the
// resolution chain Generate itself stays pure over: mcps: [...] resolves
// through the store's Catalogue, skills: [...] through skills.yml's packages
// (ADR skill-packages-are-the-unit-a-workspace-enables).
func (s *AgentWorkspacesService) Open(ctx context.Context, dir string) (OpenResult, error) {
	if !validWorkspaceDir(dir) {
		return OpenResult{}, Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", dir)
	}
	st, ok := s.workspaceStatus(dir)
	if !ok {
		return OpenResult{}, Errorf(KindNotFound, "workspace %q not found", dir)
	}
	if !st.Valid {
		return OpenResult{}, Wrap(st.Err, KindInvalid, "workspace %q", dir)
	}
	ws := st.Workspace

	rendered, missingPackages, err := s.resolveSkills(ctx, ws)
	if err != nil {
		return OpenResult{}, err
	}

	workspaceDir := filepath.Join(s.store.Root(), dir)
	genResult, err := agentws.Generate(agentws.GenerateInput{
		Dir:       workspaceDir,
		Workspace: ws,
		Servers:   s.resolveServers(ctx, ws),
		Skills:    rendered,
	})
	if err != nil {
		return OpenResult{}, Wrap(err, KindInternal, "generating workspace %q", dir)
	}

	records, err := s.db.ListAgentWorkspaceSessions(ctx, dir)
	if err != nil {
		return OpenResult{}, Wrap(err, KindInternal, "listing sessions for workspace %q", dir)
	}
	sessions := make([]SessionView, 0, len(records))
	for _, rec := range records {
		sessions = append(sessions, s.sessionView(ctx, rec))
	}

	view := workspaceView(st)
	if len(genResult.Problems) > 0 {
		view.Problem = strings.Join(genResult.Problems, "; ")
	}

	return OpenResult{
		Workspace: view, Sessions: sessions,
		MissingMCPs: genResult.MissingMCPs, MissingPackages: missingPackages,
	}, nil
}

// Sessions lists a workspace's session rows without regenerating its
// disposable artifacts — the read the Agents area uses to refresh a session
// list after starting, resuming, closing, or deleting one, without paying
// Open's generation cost.
func (s *AgentWorkspacesService) Sessions(ctx context.Context, dir string) ([]SessionView, error) {
	if !validWorkspaceDir(dir) {
		return nil, Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", dir)
	}
	records, err := s.db.ListAgentWorkspaceSessions(ctx, dir)
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing sessions for workspace %q", dir)
	}
	views := make([]SessionView, 0, len(records))
	for _, rec := range records {
		views = append(views, s.sessionView(ctx, rec))
	}
	return views, nil
}

// SessionActivity captures each live session's tmux pane and classifies it
// with dispatch.ClassifyAgentScreen: capture-pane through terminal.Detector,
// the same path SessionStatuses/FetchBatch already run for hive's own
// sessions (hc-ou4o02zx) and the input the detector was tuned against. An
// empty dir spans every workspace — the sidebar shows every session at once,
// so its indicators poll the whole set the way the Code view's do; the work
// is bounded by maxConcurrentAgentSessions either way. A session with no
// live tmux session is omitted rather than reported dead -- a row's
// TerminalID already carries that.
func (s *AgentWorkspacesService) SessionActivity(ctx context.Context, dir string) ([]SessionActivityItem, error) {
	var records []store.AgentWorkspaceSession
	var err error
	if dir == "" {
		records, err = s.db.ListAllAgentWorkspaceSessions(ctx)
		if err != nil {
			return nil, Wrap(err, KindInternal, "listing all sessions")
		}
	} else {
		if !validWorkspaceDir(dir) {
			return nil, Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", dir)
		}
		records, err = s.db.ListAgentWorkspaceSessions(ctx, dir)
		if err != nil {
			return nil, Wrap(err, KindInternal, "listing sessions for workspace %q", dir)
		}
	}
	items := make([]SessionActivityItem, 0, len(records))
	for _, rec := range records {
		screen, err := s.terminals.CapturePane(ctx, sessionName(rec.ID))
		if err != nil {
			// Not running, or a transient tmux error -- omitted rather than
			// reported, the same tolerance sessionView extends to liveness.
			continue
		}
		items = append(items, SessionActivityItem{ID: rec.ID, Status: string(dispatch.ClassifyAgentScreen(rec.Agent, screen))})
	}
	return items, nil
}

// StartSession launches a new, named session in workspace.
func (s *AgentWorkspacesService) StartSession(ctx context.Context, req StartSession) (SessionView, error) {
	if !validWorkspaceDir(req.Workspace) {
		return SessionView{}, Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", req.Workspace)
	}
	st, ok := s.workspaceStatus(req.Workspace)
	if !ok || !st.Valid {
		return SessionView{}, Errorf(KindNotFound, "workspace %q not found", req.Workspace)
	}
	ws := st.Workspace

	command, ok := s.commands[ws.Agent]
	if !ok {
		return SessionView{}, Errorf(KindInvalid, "agent %q is not configured", ws.Agent)
	}

	workspaceDir := filepath.Join(s.store.Root(), req.Workspace)
	agentSessionID := uuid.NewString()
	line, err := agentws.Resolve(command, resolvedFor(ws, workspaceDir), agentSessionID, false)
	if err != nil {
		return SessionView{}, agentLaunchError(err, ws.Agent)
	}

	now := time.Now().UnixMilli()
	rec, err := s.db.CreateAgentWorkspaceSession(ctx, store.AgentWorkspaceSession{
		Workspace: req.Workspace, Name: req.Name, Agent: ws.Agent, AgentSessionID: agentSessionID,
		CreatedAt: now, LastOpenedAt: now,
	})
	if err != nil {
		return SessionView{}, Wrap(err, KindInternal, "creating session %q", req.Name)
	}

	return s.launchTerminal(ctx, rec, workspaceDir, line, req.Cols, req.Rows, true, "")
}

// ResumeSession reattaches a session's live tmux session if it still has one
// -- the persistence win: it works identically for codex, which has no
// resume form of its own, because the process itself never stopped -- or
// relaunches it, resuming the agent's own conversation when it has a resume
// form and saying so when it does not (spec §6.2). A session the agent never
// persisted a conversation for (closed before its first message) also
// relaunches fresh, silently, instead of dying on the agent's own
// unknown-session error.
func (s *AgentWorkspacesService) ResumeSession(ctx context.Context, id int64, cols, rows int) (SessionView, error) {
	rec, ok, err := s.db.GetAgentWorkspaceSession(ctx, id)
	if err != nil {
		return SessionView{}, Wrap(err, KindInternal, "loading session %d", id)
	}
	if !ok {
		return SessionView{}, Errorf(KindNotFound, "session %d not found", id)
	}

	name := sessionName(rec.ID)
	alive, err := s.terminals.HasSession(ctx, name)
	if err != nil {
		return SessionView{}, terminalError(err, "checking session %q", rec.Name)
	}
	if alive {
		window, err := s.attach(ctx, name, cols, rows)
		if err != nil {
			return SessionView{}, err
		}
		if err := s.db.TouchAgentWorkspaceSession(ctx, rec.ID, time.Now().UnixMilli()); err != nil {
			return SessionView{}, Wrap(err, KindInternal, "recording session %q as opened", rec.Name)
		}
		return SessionView{
			ID: rec.ID, Workspace: rec.Workspace, Name: rec.Name, Agent: rec.Agent,
			LastOpenedAt: rec.LastOpenedAt, Slug: name, TerminalID: name, WindowID: window.ID,
			Cols: window.Width, Rows: window.Height,
			ResumeAttempted: agentws.SupportsResume(rec.Agent),
		}, nil
	}

	st, ok := s.workspaceStatus(rec.Workspace)
	if !ok || !st.Valid {
		return SessionView{}, Errorf(KindNotFound, "workspace %q not found", rec.Workspace)
	}
	ws := st.Workspace

	command, ok := s.commands[ws.Agent]
	if !ok {
		return SessionView{}, Errorf(KindInvalid, "agent %q is not configured", ws.Agent)
	}

	resumeAttempted := agentws.SupportsResume(ws.Agent)
	var resumeNotice string
	switch {
	case !resumeAttempted:
		resumeNotice = "the previous conversation could not be resumed; this is a fresh session"
	case !agentws.HasConversation(ws.Agent, rec.AgentSessionID):
		// The agent persisted nothing under this id — the session ended before
		// its first message — so its resume form would die in the pane ("No
		// conversation found with session ID"). A fresh launch IS the
		// continuation of an empty conversation, so no notice either.
		resumeAttempted = false
	}
	// A fresh relaunch mints a fresh id rather than reusing the record's: if
	// the conversation probe were ever wrong, relaunching under an id the
	// agent already holds would wedge every future resume on "session id
	// already in use", while abandoning it merely orphans a recoverable file.
	sessionID := rec.AgentSessionID
	if !resumeAttempted {
		sessionID = uuid.NewString()
	}

	workspaceDir := filepath.Join(s.store.Root(), rec.Workspace)
	line, err := agentws.Resolve(command, resolvedFor(ws, workspaceDir), sessionID, resumeAttempted)
	if err != nil {
		return SessionView{}, agentLaunchError(err, ws.Agent)
	}

	if sessionID != rec.AgentSessionID {
		if err := s.db.SetAgentWorkspaceSessionAgentID(ctx, rec.ID, sessionID); err != nil {
			return SessionView{}, Wrap(err, KindInternal, "recording session %q", rec.Name)
		}
		rec.AgentSessionID = sessionID
	}

	return s.launchTerminal(ctx, rec, workspaceDir, line, cols, rows, resumeAttempted, resumeNotice)
}

// CloseSession ends a session's live tmux session and reports whether there
// was one running. The record is untouched, so it still lists afterward.
func (s *AgentWorkspacesService) CloseSession(ctx context.Context, id int64) (bool, error) {
	rec, ok, err := s.db.GetAgentWorkspaceSession(ctx, id)
	if err != nil {
		return false, Wrap(err, KindInternal, "loading session %d", id)
	}
	if !ok {
		return false, Errorf(KindNotFound, "session %d not found", id)
	}
	closed, err := s.terminals.KillSession(ctx, sessionName(rec.ID))
	if err != nil {
		return false, terminalError(err, "closing session %q", rec.Name)
	}
	return closed, nil
}

// RenameSession sets a session's display name. Purely a record edit: the
// tmux session name derives from the id, so a live terminal keeps running
// under the same name.
func (s *AgentWorkspacesService) RenameSession(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return Errorf(KindInvalid, "a session needs a name")
	}
	_, ok, err := s.db.GetAgentWorkspaceSession(ctx, id)
	if err != nil {
		return Wrap(err, KindInternal, "loading session %d", id)
	}
	if !ok {
		return Errorf(KindNotFound, "session %d not found", id)
	}
	return Wrap(s.db.RenameAgentWorkspaceSession(ctx, id, name), KindInternal, "renaming session %d", id)
}

// DeleteSession ends any live tmux session, then removes the record. The
// session is killed first: its name derives from the record id, so deleting
// the record around a live one would orphan a running agent no UI could
// address again until it happened to be found by name.
func (s *AgentWorkspacesService) DeleteSession(ctx context.Context, id int64) error {
	rec, ok, err := s.db.GetAgentWorkspaceSession(ctx, id)
	if err != nil {
		return Wrap(err, KindInternal, "loading session %d", id)
	}
	if !ok {
		return Errorf(KindNotFound, "session %d not found", id)
	}
	if _, err := s.terminals.KillSession(ctx, sessionName(rec.ID)); err != nil {
		return terminalError(err, "closing session %q", rec.Name)
	}
	if err := s.db.DeleteAgentWorkspaceSession(ctx, id); err != nil {
		return Wrap(err, KindInternal, "deleting session %q", rec.Name)
	}
	return nil
}

// DeleteWorkspace ends every live tmux session the workspace's sessions hold,
// then removes the session records — never the directory, which is the
// user's and possibly under version control (spec §14).
func (s *AgentWorkspacesService) DeleteWorkspace(ctx context.Context, dir string) error {
	if !validWorkspaceDir(dir) {
		return Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", dir)
	}
	records, err := s.db.ListAgentWorkspaceSessions(ctx, dir)
	if err != nil {
		return Wrap(err, KindInternal, "listing sessions for workspace %q", dir)
	}
	for _, rec := range records {
		if _, err := s.terminals.KillSession(ctx, sessionName(rec.ID)); err != nil {
			return terminalError(err, "closing session %q", rec.Name)
		}
	}
	if err := s.db.DeleteAgentWorkspaceSessionsByWorkspace(ctx, dir); err != nil {
		return Wrap(err, KindInternal, "deleting sessions for workspace %q", dir)
	}
	return nil
}

// Agents lists the agent keys this build can launch, sorted — the choices a
// workspace editor offers for its agent field.
func (s *AgentWorkspacesService) Agents(context.Context) []string {
	agents := make([]string, 0, len(s.commands))
	for agent := range s.commands {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	return agents
}

// AutonomyFlags reports, per agent, the CLI flags each autonomy posture
// launches with — the launch table projected for the editor, so a posture
// shows the authority it actually grants (--dangerously-skip-permissions is
// something to read, not a euphemism to hide; ADR a-workspace-declares-its-own-authority §5's posture applied
// to autonomy). A posture absent from an agent's map is one the launch would
// refuse, which the editor disables.
func (s *AgentWorkspacesService) AutonomyFlags(context.Context) map[string]map[string][]string {
	table := agentws.AutonomyFlags()
	out := make(map[string]map[string][]string, len(table))
	for agent, postures := range table {
		m := make(map[string][]string, len(postures))
		for posture, flags := range postures {
			m[string(posture)] = flags
		}
		out[agent] = m
	}
	return out
}

// WorkspaceEdit names the manifest fields the in-app editor writes. Anything
// else the manifest says is untouched — WriteManifest edits the document in
// place, so comments and keys the editor does not own stay the user's.
type WorkspaceEdit struct {
	Dir      string
	Name     string
	Agent    string
	Autonomy string
	MCPs     []string
	Skills   []string
}

func (e WorkspaceEdit) manifest() agentws.ManifestEdit {
	return agentws.ManifestEdit{
		Name: strings.TrimSpace(e.Name), Agent: e.Agent, Autonomy: agentws.Autonomy(e.Autonomy),
		MCPs: e.MCPs, Skills: e.Skills,
	}
}

func (s *AgentWorkspacesService) validateEdit(req WorkspaceEdit) error {
	if !validWorkspaceDir(req.Dir) {
		return Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", req.Dir)
	}
	if strings.TrimSpace(req.Name) == "" {
		return Errorf(KindInvalid, "a workspace needs a name")
	}
	if _, ok := s.commands[req.Agent]; !ok {
		return Errorf(KindInvalid, "agent %q is not configured in this build", req.Agent)
	}
	if !agentws.Autonomy(req.Autonomy).IsValid() {
		return Errorf(KindInvalid, "autonomy %q is not valid (expected %s)", req.Autonomy, strings.Join(agentws.AutonomyNames(), ", "))
	}
	for _, list := range []struct {
		label  string
		values []string
	}{
		{"mcp", req.MCPs},
		{"skill package", req.Skills},
	} {
		seen := make(map[string]bool, len(list.values))
		for _, id := range list.values {
			if id == "" {
				return Errorf(KindInvalid, "%ss entries must not be empty", list.label)
			}
			if seen[id] {
				return Errorf(KindInvalid, "duplicate %s %q", list.label, id)
			}
			seen[id] = true
		}
	}
	return nil
}

// CreateWorkspace makes a directory under the root with a fresh manifest and
// an AGENTS.md scaffold, and returns its view.
func (s *AgentWorkspacesService) CreateWorkspace(ctx context.Context, req WorkspaceEdit) (WorkspaceView, error) {
	if err := s.validateEdit(req); err != nil {
		return WorkspaceView{}, err
	}
	if err := agentws.CreateWorkspace(s.store.Root(), req.Dir, req.manifest()); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return WorkspaceView{}, Errorf(KindConflict, "workspace %q already exists", req.Dir)
		}
		return WorkspaceView{}, Wrap(err, KindInternal, "creating workspace %q", req.Dir)
	}
	return s.reloadedView(ctx, req.Dir)
}

// UpdateWorkspace rewrites the editable fields of an existing workspace's
// manifest in place — comments and keys the editor does not own survive.
func (s *AgentWorkspacesService) UpdateWorkspace(ctx context.Context, req WorkspaceEdit) (WorkspaceView, error) {
	if err := s.validateEdit(req); err != nil {
		return WorkspaceView{}, err
	}
	if _, ok := s.workspaceStatus(req.Dir); !ok {
		return WorkspaceView{}, Errorf(KindNotFound, "workspace %q not found", req.Dir)
	}
	if err := agentws.WriteManifest(s.store.Root(), req.Dir, req.manifest()); err != nil {
		return WorkspaceView{}, Wrap(err, KindInvalid, "updating workspace %q", req.Dir)
	}
	return s.reloadedView(ctx, req.Dir)
}

// MCPCatalogueItem is one row of the merged MCP catalogue as the UI shows it:
// a shipped entry, a user mcps.yaml entry, or a user entry shadowing a
// shipped one.
type MCPCatalogueItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Shipped     bool   `json:"shipped"`
	Stability   string `json:"stability"`
	Shadows     string `json:"shadows"`
	Transport   string `json:"transport"`
	// Command is the resolved invocation — the command line for stdio, the
	// URL for http/sse. Nothing is enabled whose command the user could not
	// read first (ADR a-workspace-declares-its-own-authority §5).
	Command string `json:"command"`
	Problem string `json:"problem"`
}

// MCPCatalogue lists the merged MCP catalogue — the choices a workspace
// editor offers for its mcps: list.
func (s *AgentWorkspacesService) MCPCatalogue(ctx context.Context) []MCPCatalogueItem {
	entries := s.catalogue(ctx)
	items := make([]MCPCatalogueItem, 0, len(entries))
	for _, e := range entries {
		title := e.Title
		if title == "" {
			title = e.ID
		}
		items = append(items, MCPCatalogueItem{
			ID: e.ID, Title: title, Description: e.Description,
			Shipped: e.Shipped, Stability: string(e.Stability), Shadows: e.Shadows,
			Transport: string(e.Server.Transport),
			Command:   resolvedCommandLine(e.Server),
			Problem:   e.Problem,
		})
	}
	return items
}

// SkillPackageItem is one row of the package catalogue as the UI shows it: a
// package from skills.yml and the skills it currently resolves to. Members
// are what the patterns select right now, not a stored list — adding a
// matching skill changes every workspace that enabled the package.
type SkillPackageItem struct {
	Name        string               `json:"name"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Members     []SkillPackageMember `json:"members"`
}

// SkillPackageMember is one skill a package selects, with where it came from.
type SkillPackageMember struct {
	Slug    string `json:"slug"`
	Shipped bool   `json:"shipped"`
}

// SkillPackages lists the packages a workspace editor offers for its skills:
// list, plus the problem skills.yml itself has, if any. A source that cannot
// be read contributes no names rather than failing the listing: an open is
// where that is reported, because that is the call whose result changes.
func (s *AgentWorkspacesService) SkillPackages(ctx context.Context) ([]SkillPackageItem, string) {
	shipped, _ := s.shippedSkills(ctx)
	shared, _ := s.sharedSkills()

	library := s.store.SkillLibrary()
	problem := ""
	if !library.Valid && library.Err != nil {
		problem = library.Err.Error()
	}

	entries := agentws.SkillPackageCatalogue(library.Library, agentws.SkillNames(shipped, shared))
	items := make([]SkillPackageItem, 0, len(entries))
	for _, e := range entries {
		members := make([]SkillPackageMember, 0, len(e.Members))
		for _, m := range e.Members {
			members = append(members, SkillPackageMember{Slug: m.Slug, Shipped: m.Shipped})
		}
		items = append(items, SkillPackageItem{
			Name: e.Name, Title: e.Title, Description: e.Description, Members: members,
		})
	}
	return items, problem
}

// RevealSkillPackages opens skills.yml in the OS file manager's default
// handler — packages are defined by editing that file, which is the authoring
// act this release offers. A missing file is seeded first, so the action
// always lands somewhere real.
func (s *AgentWorkspacesService) RevealSkillPackages(context.Context) error {
	if err := agentws.SeedDefaultsIfMissing(s.store.Root()); err != nil {
		return Wrap(err, KindInternal, "seeding skills.yml")
	}
	path := agentws.SkillLibraryPath(s.store.Root())
	return Wrap(osopen.Open(path), KindInternal, "opening %s", path)
}

// RevealSharedSkills opens the shared skills directory in the OS file
// manager — where a skill a package can select is authored, since it is a
// SKILL.md a user writes. The directory is created if missing: startup seeds
// it, but a user who deleted it must still be able to open it and drop a
// skill back in.
func (s *AgentWorkspacesService) RevealSharedSkills(context.Context) error {
	dir := agentws.SharedSkillsDir(s.store.Root())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Wrap(err, KindInternal, "creating %s", dir)
	}
	return Wrap(osopen.Open(dir), KindInternal, "opening %s", dir)
}

// shippedSkills is the catalogue's shipped half: every listed prompt as an
// installable skill. It renders against an empty prompts.Input, so a prompt
// that needs caller-supplied context the workspace editor does not have (the
// keybindings catalog) is absent from the catalogue rather than offered and
// unresolvable.
func (s *AgentWorkspacesService) shippedSkills(ctx context.Context) ([]agentws.ShippedSkill, error) {
	if s.skills == nil {
		return nil, nil
	}
	return s.skills.ShippedSkills(ctx)
}

func (s *AgentWorkspacesService) sharedSkills() ([]agentws.SharedSkill, error) {
	return agentws.LoadSharedSkills(agentws.SharedSkillsDir(s.store.Root()))
}

// ImportMCPServers parses pasted MCP JSON (claude's mcpServers wrapper or a
// bare id-to-server map) into mcps.yaml and returns the added ids, sorted. An
// id already declared in the library is a conflict — the file is where an
// existing entry is changed — while shadowing a shipped id is allowed, the
// same rule mcps.yaml itself follows.
func (s *AgentWorkspacesService) ImportMCPServers(_ context.Context, raw string) ([]string, error) {
	servers, err := agentws.ParseMCPImport([]byte(raw))
	if err != nil {
		return nil, Wrap(err, KindInvalid, "importing MCP servers")
	}
	if err := agentws.AddLibraryServers(s.store.Root(), servers); err != nil {
		if errors.Is(err, agentws.ErrLibraryServerExists) {
			return nil, Wrap(err, KindConflict, "importing MCP servers")
		}
		return nil, Wrap(err, KindInternal, "importing MCP servers")
	}
	if err := s.store.Reload(); err != nil {
		return nil, Wrap(err, KindInternal, "reloading the MCP catalogue")
	}
	ids := make([]string, 0, len(servers))
	for id := range servers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// RemoveMCPServer deletes a user-declared server from mcps.yaml. A shipped
// entry cannot be removed — a workspace disables it by dropping the id from
// its own mcps: list instead.
func (s *AgentWorkspacesService) RemoveMCPServer(_ context.Context, id string) error {
	found, err := agentws.RemoveLibraryServer(s.store.Root(), id)
	if err != nil {
		return Wrap(err, KindInternal, "removing MCP server %q", id)
	}
	if !found {
		if _, shipped := mcpcatalog.All()[id]; shipped {
			return Errorf(KindInvalid, "%q is a shipped MCP server; remove it from the workspace's own list instead", id)
		}
		return Errorf(KindNotFound, "MCP server %q is not declared in mcps.yaml", id)
	}
	if err := s.store.Reload(); err != nil {
		return Wrap(err, KindInternal, "reloading the MCP catalogue")
	}
	return nil
}

// Editor reports the configured "open in editor" target: the command from
// settings and the display title the UI labels the action with. An empty
// command means none is configured.
func (s *AgentWorkspacesService) Editor(ctx context.Context) (command, title string) {
	if s.editorCommand == nil {
		return "", ""
	}
	command, err := s.editorCommand(ctx)
	if err != nil || command == "" {
		return "", ""
	}
	return command, editorTitle(command)
}

// OpenWorkspaceInEditor launches the configured editor on the workspace
// directory, detached — the editor outlives the request and Hive never waits
// on it.
func (s *AgentWorkspacesService) OpenWorkspaceInEditor(ctx context.Context, dir string) error {
	workspaceDir, err := s.knownWorkspaceDir(dir)
	if err != nil {
		return err
	}
	command, _ := s.Editor(ctx)
	if command == "" {
		return Errorf(KindInvalid, "no editor is configured; choose one in Settings › General")
	}
	path, err := s.execEnv.LookPath(ctx, command)
	if err != nil {
		return Errorf(KindInvalid, "editor %q was not found on PATH; choose another in Settings › General", command)
	}
	// WithoutCancel: the editor must outlive the request that launched it —
	// a request-scoped context would kill it the moment the response is sent.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), path, workspaceDir)
	cmd.Env = s.execEnv.Environ(ctx)
	if err := cmd.Start(); err != nil {
		return Wrap(err, KindInternal, "launching %s", command)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// RevealWorkspace opens the workspace directory in the OS file manager.
func (s *AgentWorkspacesService) RevealWorkspace(_ context.Context, dir string) error {
	workspaceDir, err := s.knownWorkspaceDir(dir)
	if err != nil {
		return err
	}
	return Wrap(osopen.Open(workspaceDir), KindInternal, "opening %s", workspaceDir)
}

// knownWorkspaceDir resolves dir to its absolute path, refusing anything that
// is not a recognized workspace — the same guard checkAllowed gives
// SystemService's open/reveal, because launching a program on an arbitrary
// request-supplied path is not this API's to offer. A workspace with a broken
// manifest still resolves: opening it in an editor is exactly how it gets
// fixed.
func (s *AgentWorkspacesService) knownWorkspaceDir(dir string) (string, error) {
	if !validWorkspaceDir(dir) {
		return "", Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", dir)
	}
	if _, ok := s.workspaceStatus(dir); !ok {
		return "", Errorf(KindNotFound, "workspace %q not found", dir)
	}
	return filepath.Join(s.store.Root(), dir), nil
}

// resolvedCommandLine renders what an entry actually launches, for display: a
// shell-style command line for stdio, the URL for a remote server.
func resolvedCommandLine(server mcpcatalog.Server) string {
	if server.Transport == mcpcatalog.TransportStdio {
		return strings.Join(append([]string{server.Command}, server.Args...), " ")
	}
	return server.URL
}

// reloadedView re-reads the store after a manifest write and returns dir's
// fresh view, so the response reflects what actually landed on disk rather
// than what was asked for.
func (s *AgentWorkspacesService) reloadedView(_ context.Context, dir string) (WorkspaceView, error) {
	if err := s.store.Reload(); err != nil {
		return WorkspaceView{}, Wrap(err, KindInternal, "reloading workspaces")
	}
	st, ok := s.workspaceStatus(dir)
	if !ok {
		return WorkspaceView{}, Errorf(KindInternal, "workspace %q vanished after writing it", dir)
	}
	return workspaceView(st), nil
}

// ResizeSession votes a size for a session's attached control client — the
// same refresh-client vote the Code view's panes cast. tmux answers over the
// stream with a window 'resized' event, which is what actually sets the
// pane's grid; see AgentsMode's resize wiring.
func (s *AgentWorkspacesService) ResizeSession(ctx context.Context, id int64, cols, rows int) error {
	rec, ok, err := s.db.GetAgentWorkspaceSession(ctx, id)
	if err != nil {
		return Wrap(err, KindInternal, "loading session %d", id)
	}
	if !ok {
		return Errorf(KindNotFound, "session %d not found", id)
	}
	client, ok := s.terminals.Client(sessionName(rec.ID))
	if !ok {
		return Errorf(KindNotFound, "session %q has no attached terminal", rec.Name)
	}
	return terminalError(client.Resize(ctx, cols, rows), "resizing session %q", rec.Name)
}

// launchTerminal creates rec's tmux session fresh and attaches to it, gives it
// earlyExitWindow to report that it already died, and assembles the notice
// the UI shows — resumeNotice is the caller's own words for a resume that
// could not happen, empty when there is nothing to announce. Both
// StartSession and ResumeSession's relaunch branch always want a fresh
// session here — ResumeSession's still-alive branch attaches directly
// instead, without going through this method.
func (s *AgentWorkspacesService) launchTerminal(ctx context.Context, rec store.AgentWorkspaceSession, dir, line string, cols, rows int, resumeAttempted bool, resumeNotice string) (SessionView, error) {
	count, err := s.liveSessionCount(ctx)
	if err != nil {
		return SessionView{}, terminalError(err, "counting live agent sessions")
	}
	if count >= maxConcurrentAgentSessions {
		return SessionView{}, Errorf(KindConflict, "too many agent sessions are running (%d max); close one first", maxConcurrentAgentSessions)
	}

	name := sessionName(rec.ID)
	if err := s.terminals.NewSession(ctx, name, dir, line); err != nil {
		return SessionView{}, terminalError(err, "launching session %q", rec.Name)
	}

	if err := s.db.TouchAgentWorkspaceSession(ctx, rec.ID, time.Now().UnixMilli()); err != nil {
		return SessionView{}, Wrap(err, KindInternal, "recording session %q as opened", rec.Name)
	}

	view := SessionView{
		ID: rec.ID, Workspace: rec.Workspace, Name: rec.Name, Agent: rec.Agent,
		LastOpenedAt: rec.LastOpenedAt, Slug: name, ResumeAttempted: resumeAttempted,
	}

	if s.awaitEarlyExit(ctx, name) {
		view.Notice = "the session exited immediately; check that the agent CLI is installed and on PATH"
		return view, nil
	}

	window, err := s.attach(ctx, name, cols, rows)
	if err != nil {
		return SessionView{}, err
	}
	view.TerminalID = name
	view.WindowID = window.ID
	view.Cols, view.Rows = window.Width, window.Height

	var notices []string
	if resumeNotice != "" {
		notices = append(notices, resumeNotice)
	}
	if notice := mcpNotice(rec.Agent); notice != "" {
		notices = append(notices, notice)
	}
	view.Notice = strings.Join(notices, "; ")
	return view, nil
}

// attach opens the control client for name and returns its active window —
// id plus the size tmux granted, which the caller must surface so the pane
// opens its grid at tmux's size rather than the size it asked for.
func (s *AgentWorkspacesService) attach(ctx context.Context, name string, cols, rows int) (tmuxcc.Window, error) {
	windows, err := s.terminals.Attach(ctx, name, cols, rows)
	if err != nil {
		return tmuxcc.Window{}, terminalError(err, "attaching to session %q", name)
	}
	return firstActiveWindow(windows), nil
}

// firstActiveWindow returns windows' active entry, or the first when none is
// marked active, or a zero Window when there are none. An agent workspace
// session's own launch line never opens more than one window; a second one
// only ever comes from the agent itself running tmux new-window inside its
// own session, which this still addresses sensibly rather than erroring.
func firstActiveWindow(windows []tmuxcc.Window) tmuxcc.Window {
	for _, w := range windows {
		if w.Active {
			return w
		}
	}
	if len(windows) > 0 {
		return windows[0]
	}
	return tmuxcc.Window{}
}

// awaitEarlyExit gives a just-created tmux session earlyExitWindow to exit on
// its own. Unlike ptyterm's Exited event this carries no reason: tmux simply
// stops answering has-session once its initial pane's command has ended.
func (s *AgentWorkspacesService) awaitEarlyExit(ctx context.Context, name string) bool {
	deadline := time.Now().Add(earlyExitWindow)
	for {
		exists, err := s.terminals.HasSession(ctx, name)
		if err != nil || !exists {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(earlyExitPollInterval)
	}
}

// liveSessionCount counts live agentws-* tmux sessions across every
// workspace -- the service-level concurrency cap, since a live session is a
// tmux fact rather than something this service's own records track (a
// session record outlives the app; a tmux session need not).
func (s *AgentWorkspacesService) liveSessionCount(ctx context.Context) (int, error) {
	names, err := s.terminals.SessionNames(ctx, agentSessionPrefix)
	if err != nil {
		return 0, err
	}
	return len(names), nil
}

// resolveServers resolves a workspace's declared MCP ids through the store's
// catalogue. An id with no catalogue entry is simply omitted here; Generate
// itself reports it in MissingMCPs by comparing ws.MCPs against this map, so
// there is nothing to track twice.
func (s *AgentWorkspacesService) resolveServers(ctx context.Context, ws agentws.Workspace) map[string]mcpcatalog.Server {
	byID := make(map[string]mcpcatalog.Server, len(ws.MCPs))
	for _, entry := range s.catalogue(ctx) {
		byID[entry.ID] = entry.Server
	}
	servers := make(map[string]mcpcatalog.Server, len(ws.MCPs))
	for _, id := range ws.MCPs {
		if srv, ok := byID[id]; ok {
			servers[id] = srv
		}
	}
	return servers
}

// resolveSkills expands a workspace's enabled packages into the skills they
// select and renders each: a shared skill installs its authored file
// verbatim, a shipped one is rendered per install through
// SkillsService.RenderSkill, which takes the underlying prompt id ("mcp")
// rather than the installed slug ("hive-mcp") skillSlug mints. It also
// reports the packages skills.yml does not define — a workspace naming one
// still opens, the same way a missing MCP id does not stop a launch.
func (s *AgentWorkspacesService) resolveSkills(ctx context.Context, ws agentws.Workspace) (rendered []agentws.RenderedSkill, missingPackages []string, err error) {
	if len(ws.Skills) == 0 {
		return nil, nil, nil
	}

	// Unlike SkillCatalogue, neither read is allowed to degrade here: an
	// unreadable shared directory would silently generate a tree without
	// those skills, which reads as a successful open of a workspace that has
	// quietly lost half its capability.
	shared, err := s.sharedSkills()
	if err != nil {
		return nil, nil, Wrap(err, KindInternal, "workspace %q: reading the shared skills", ws.Dir)
	}
	shipped, err := s.shippedSkills(ctx)
	if err != nil {
		return nil, nil, Wrap(err, KindInternal, "workspace %q: reading the shipped skills", ws.Dir)
	}

	bodies := make(map[string]string, len(shared))
	for _, skill := range shared {
		bodies[skill.Slug] = skill.Body
	}

	library := s.store.SkillLibrary()
	selected, missingPackages := agentws.SelectSkills(library.Library, ws.Skills, agentws.SkillNames(shipped, shared))

	rendered = make([]agentws.RenderedSkill, 0, len(selected))
	for _, name := range selected {
		if body, ok := bodies[name.Slug]; ok {
			rendered = append(rendered, agentws.RenderedSkill{Slug: name.Slug, Body: body})
			continue
		}
		id, ok := skillIDFromSlug(name.Slug)
		if !ok {
			continue
		}
		slug, body, err := s.skills.RenderSkill(ctx, id)
		if err != nil {
			return nil, nil, Wrap(err, KindInternal, "workspace %q: rendering skill %q", ws.Dir, name.Slug)
		}
		rendered = append(rendered, agentws.RenderedSkill{Slug: slug, Body: body})
	}
	return rendered, missingPackages, nil
}

func (s *AgentWorkspacesService) workspaceStatus(dir string) (agentws.WorkspaceStatus, bool) {
	for _, st := range s.store.Statuses() {
		if st.Dir == dir {
			return st, true
		}
	}
	return agentws.WorkspaceStatus{}, false
}

// sessionView reports a session record's current, read-only state -- unlike
// launchTerminal's view, this never launches or attaches anything, so
// WindowID, ResumeAttempted and Notice stay zero-valued.
func (s *AgentWorkspacesService) sessionView(ctx context.Context, rec store.AgentWorkspaceSession) SessionView {
	name := sessionName(rec.ID)
	live := ""
	if alive, err := s.terminals.HasSession(ctx, name); err == nil && alive {
		live = name
	}
	return SessionView{
		ID: rec.ID, Workspace: rec.Workspace, Name: rec.Name, Agent: rec.Agent,
		LastOpenedAt: rec.LastOpenedAt, Slug: name, TerminalID: live,
	}
}

func workspaceView(st agentws.WorkspaceStatus) WorkspaceView {
	problem, notice := "", ""
	if !st.Valid && st.Err != nil {
		problem = st.Err.Error()
	} else if st.Valid {
		// A workspace whose manifest failed to parse has no trustworthy Agent
		// field to explain, so the MCP notice is skipped rather than shown
		// against whatever the zero value happens to be.
		notice = mcpNotice(st.Workspace.Agent)
	}
	return WorkspaceView{
		Dir: st.Dir, Name: st.Workspace.Name, Agent: st.Workspace.Agent,
		Autonomy: string(st.Workspace.Autonomy), MCPs: st.Workspace.MCPs,
		Skills: st.Workspace.Skills, Problem: problem,
		Notice: notice,
	}
}

// resolvedFor returns a copy of ws whose Dir is the absolute workspace
// directory rather than the bare directory name Workspace normally carries.
// agentws.Resolve threads w.Dir straight into MCPWiring.Args, which needs a
// path that resolves regardless of the launched process's cwd.
func resolvedFor(ws agentws.Workspace, absoluteDir string) agentws.Workspace {
	ws.Dir = absoluteDir
	return ws
}

// sessionName derives a session's tmux session name from its record id, so a
// resume within one run reattaches rather than spawning beside itself.
func sessionName(recordID int64) string {
	return fmt.Sprintf("%s%d", agentSessionPrefix, recordID)
}

// validWorkspaceDir reports whether dir resolves to a direct child of the
// workspace root: non-empty, not "." or "..", and a single path component. A
// workspace directory arrives as a string over the HTTP API (spec §8 hands
// this API to an agent), so it is validated the same way generate.go
// validates a skill slug rather than trusted.
func validWorkspaceDir(dir string) bool {
	if dir == "" || dir == "." {
		return false
	}
	return filepath.Base(dir) == dir && filepath.IsLocal(dir)
}

// mcpNotice reports the MCP-related explanation the UI shows for agent, or ""
// when its wiring bounds the tool set to exactly what the workspace declares.
func mcpNotice(agent string) string {
	bounded, ok := agentws.MCPBounded(agent)
	switch {
	case !ok:
		return "this agent has no known way to receive MCP servers; any declared servers are not passed to it"
	case !bounded:
		return "this agent's tool set is not limited to the workspace's declared MCP servers; it also loads its own global configuration"
	default:
		return ""
	}
}

// skillIDFromSlug reverses skillSlug ("hive-" + id): a workspace's skills:
// list carries the installed slug, but RenderSkill takes the underlying
// prompt id.
func skillIDFromSlug(slug string) (id string, ok bool) {
	const prefix = "hive-"
	if !strings.HasPrefix(slug, prefix) {
		return "", false
	}
	id = strings.TrimPrefix(slug, prefix)
	return id, id != ""
}

// AllSessions lists every session across every workspace, newest record
// first — the sidebar's cross-workspace read, unlike Sessions which scopes
// to one workspace.
func (s *AgentWorkspacesService) AllSessions(ctx context.Context) ([]SessionView, error) {
	records, err := s.db.ListAllAgentWorkspaceSessions(ctx)
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing all sessions")
	}
	views := make([]SessionView, 0, len(records))
	for _, rec := range records {
		views = append(views, s.sessionView(ctx, rec))
	}
	return views, nil
}

// agentLaunchError classifies an agentws launch-resolution failure by
// sentinel rather than by message (architecture.md, Errors). Every case here
// traces back to an authored file being wrong -- the workspace's agent or
// autonomy, or hive's own agent config -- so all are KindInvalid.
func agentLaunchError(err error, agent string) error {
	switch {
	case errors.Is(err, agentws.ErrUnknownAgent):
		return Wrap(err, KindInvalid, "agent %q has no launch mapping in this build", agent)
	case errors.Is(err, agentws.ErrNoAutonomyMapping):
		return Wrap(err, KindInvalid, "agent %q has no mapping for this workspace's autonomy posture", agent)
	case errors.Is(err, agentws.ErrPostureUnavailable):
		return Wrap(err, KindInvalid, "agent %q has no known MCP wiring, so this autonomy posture is unavailable", agent)
	case errors.Is(err, agentws.ErrCommandNotASingleWord):
		return Wrap(err, KindInvalid, "agent %q's configured command must be a single word", agent)
	default:
		return Wrap(err, KindInternal, "resolving the launch for agent %q", agent)
	}
}
