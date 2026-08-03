package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/mcpcatalog"
	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// earlyExitWindow is how long a just-launched terminal gets to report that
// its process already exited before StartSession/ResumeSession report it as
// live. ptyterm always runs the resolved line through a login shell
// (ptyterm.Manager.argv), so a command that is not on PATH is not an exec
// failure this service can see directly: the shell starts, prints "command
// not found", and exits 127 -- usually within a few milliseconds, well
// inside this window even accounting for a slow CI machine sourcing shell
// startup files. A real agent CLI never exits this fast, so the window never
// misclassifies a live session as dead.
const earlyExitWindow = 500 * time.Millisecond

// AgentWorkspacesService opens agent workspaces and drives the sessions run
// inside them: an agent CLI on a Hive-owned PTY, resolved through the launch
// table in agentws (autonomy flags, MCP wiring, session/resume args) and
// addressed by a durable store.AgentWorkspaceSession record.
type AgentWorkspacesService struct {
	store     *agentws.Store
	terminals *ptyterm.Manager
	db        *store.DB
	skills    *SkillsService
	// commands is agentCommands' result (app.go): hive's configured agent
	// profiles projected onto their bare command, with Flags dropped at the
	// seam (ADR 0061). An agent key absent here is unknown to hive at all;
	// present here but absent from agentws's launch table is the second,
	// distinct refusal.
	commands map[string]string
	// rootProblem carries EnsureRoot's error, verbatim, when the configured
	// root could not be created or opened at startup -- empty otherwise.
	rootProblem string
}

func newAgentWorkspacesService(store *agentws.Store, terminals *ptyterm.Manager, db *store.DB, skills *SkillsService, commands map[string]string, rootProblem string) *AgentWorkspacesService {
	return &AgentWorkspacesService{store: store, terminals: terminals, db: db, skills: skills, commands: commands, rootProblem: rootProblem}
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
	Problem  string   `json:"problem"`
	// Notice mirrors SessionView.Notice's MCP explanation, shown on the
	// workspace row itself: an agent whose wiring cannot bound its tool set to
	// what the workspace declares says so before any session is even started
	// (spec §7.2, ADR 0061).
	Notice string `json:"notice"`
}

// SessionView is one row of a workspace's session list.
type SessionView struct {
	ID           int64  `json:"id"`
	Workspace    string `json:"workspace"`
	Name         string `json:"name"`
	Agent        string `json:"agent"`
	LastOpenedAt int64  `json:"lastOpenedAt"`
	// TerminalID addresses the live PTY, empty when nothing is running.
	TerminalID string `json:"terminalId"`
	// ResumeAttempted is false when this launch could not even try to resume —
	// the agent has no resume form. It is deliberately not named Resumed: Hive
	// sees only PTY bytes, so a pruned agent history would report a successful
	// resume while the agent prints its own error in the pane, which is the
	// "pretending it was" spec §6.2 forbids.
	ResumeAttempted bool `json:"resumeAttempted"`
	// Notice carries a fresh-launch, unbounded-MCP or missing-MCP explanation.
	Notice string `json:"notice"`
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
}

// Available reports build and platform support for the PTYs sessions run on.
func (s *AgentWorkspacesService) Available(ctx context.Context) error {
	return popupError(s.terminals.Available(ctx), "agent workspaces need macOS or Linux and a desktop build.")
}

// Root returns the configured workspace root.
func (s *AgentWorkspacesService) Root(context.Context) string {
	return s.store.Root()
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
// through the store's Catalogue, skills: [...] through SkillsService.
// RenderSkill.
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

	rendered, err := s.resolveSkills(ctx, ws)
	if err != nil {
		return OpenResult{}, err
	}

	workspaceDir := filepath.Join(s.store.Root(), dir)
	genResult, err := agentws.Generate(agentws.GenerateInput{
		Dir:       workspaceDir,
		Shared:    filepath.Join(s.store.Root(), ".shared"),
		Workspace: ws,
		Servers:   s.resolveServers(ws),
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
		sessions = append(sessions, s.sessionView(rec))
	}

	view := workspaceView(st)
	if len(genResult.Problems) > 0 {
		view.Problem = strings.Join(genResult.Problems, "; ")
	}

	return OpenResult{Workspace: view, Sessions: sessions, MissingMCPs: genResult.MissingMCPs}, nil
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
		views = append(views, s.sessionView(rec))
	}
	return views, nil
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

	return s.launchTerminal(ctx, rec, workspaceDir, line, req.Cols, req.Rows, false, true)
}

// ResumeSession reattaches a session's live terminal if it still has one, or
// relaunches it — resuming the agent's own conversation when it has a resume
// form, and saying so when it does not (spec §6.2).
func (s *AgentWorkspacesService) ResumeSession(ctx context.Context, id int64, cols, rows int) (SessionView, error) {
	rec, ok, err := s.db.GetAgentWorkspaceSession(ctx, id)
	if err != nil {
		return SessionView{}, Wrap(err, KindInternal, "loading session %d", id)
	}
	if !ok {
		return SessionView{}, Errorf(KindNotFound, "session %d not found", id)
	}

	termID := terminalID(rec.ID)
	if _, err := s.terminals.Get(termID); err == nil {
		if err := s.db.TouchAgentWorkspaceSession(ctx, rec.ID, time.Now().UnixMilli()); err != nil {
			return SessionView{}, Wrap(err, KindInternal, "recording session %q as opened", rec.Name)
		}
		return SessionView{
			ID: rec.ID, Workspace: rec.Workspace, Name: rec.Name, Agent: rec.Agent,
			LastOpenedAt: rec.LastOpenedAt, TerminalID: termID,
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

	return s.launchTerminal(ctx, rec, workspaceDir, line, cols, rows, true, resumeAttempted)
}

// CloseSession ends a session's live terminal and reports whether there was
// one running. The record is untouched, so it still lists afterward.
func (s *AgentWorkspacesService) CloseSession(ctx context.Context, id int64) (bool, error) {
	rec, ok, err := s.db.GetAgentWorkspaceSession(ctx, id)
	if err != nil {
		return false, Wrap(err, KindInternal, "loading session %d", id)
	}
	if !ok {
		return false, Errorf(KindNotFound, "session %d not found", id)
	}
	closed, err := s.terminals.Close(terminalID(rec.ID))
	if err != nil {
		return false, popupError(err, "closing session %q", rec.Name)
	}
	return closed, nil
}

// DeleteSession ends any live terminal, then removes the record. The
// terminal is closed first: the terminal id derives from the record id, so
// deleting the record around a live PTY would orphan a running agent no UI
// could address again until the app closes.
func (s *AgentWorkspacesService) DeleteSession(ctx context.Context, id int64) error {
	rec, ok, err := s.db.GetAgentWorkspaceSession(ctx, id)
	if err != nil {
		return Wrap(err, KindInternal, "loading session %d", id)
	}
	if !ok {
		return Errorf(KindNotFound, "session %d not found", id)
	}
	if _, err := s.terminals.Close(terminalID(rec.ID)); err != nil {
		return popupError(err, "closing session %q", rec.Name)
	}
	if err := s.db.DeleteAgentWorkspaceSession(ctx, id); err != nil {
		return Wrap(err, KindInternal, "deleting session %q", rec.Name)
	}
	return nil
}

// DeleteWorkspace ends every live terminal the workspace's sessions hold,
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
		if _, err := s.terminals.Close(terminalID(rec.ID)); err != nil {
			return popupError(err, "closing session %q", rec.Name)
		}
	}
	if err := s.db.DeleteAgentWorkspaceSessionsByWorkspace(ctx, dir); err != nil {
		return Wrap(err, KindInternal, "deleting sessions for workspace %q", dir)
	}
	return nil
}

// launchTerminal opens the terminal for rec, gives it earlyExitWindow to
// report that it already died, and assembles the notice the UI shows.
func (s *AgentWorkspacesService) launchTerminal(ctx context.Context, rec store.AgentWorkspaceSession, dir, line string, cols, rows int, resumeRequested, resumeAttempted bool) (SessionView, error) {
	id := terminalID(rec.ID)
	if _, err := s.terminals.Open(ctx, ptyterm.Spec{ID: id, Dir: dir, Command: line, Cols: cols, Rows: rows}); err != nil {
		return SessionView{}, popupError(err, "launching session %q", rec.Name)
	}

	if err := s.db.TouchAgentWorkspaceSession(ctx, rec.ID, time.Now().UnixMilli()); err != nil {
		return SessionView{}, Wrap(err, KindInternal, "recording session %q as opened", rec.Name)
	}

	view := SessionView{
		ID: rec.ID, Workspace: rec.Workspace, Name: rec.Name, Agent: rec.Agent,
		LastOpenedAt: rec.LastOpenedAt, TerminalID: id, ResumeAttempted: resumeAttempted,
	}

	if exited, reason := s.awaitEarlyExit(id); exited {
		view.TerminalID = ""
		view.Notice = fmt.Sprintf("the session exited immediately: %s", reason)
		return view, nil
	}

	var notices []string
	if resumeRequested && !resumeAttempted {
		notices = append(notices, "the previous conversation could not be resumed; this is a fresh session")
	}
	if notice := mcpNotice(rec.Agent); notice != "" {
		notices = append(notices, notice)
	}
	view.Notice = strings.Join(notices, "; ")
	return view, nil
}

// awaitEarlyExit gives a just-opened terminal earlyExitWindow to report that
// its process already exited, so a caller never reports a live session over
// a terminal that is already gone.
func (s *AgentWorkspacesService) awaitEarlyExit(id string) (exited bool, reason string) {
	events, unsubscribe, err := s.terminals.Subscribe(id)
	if err != nil {
		// ptyterm forgets a terminal the instant its reader drains after
		// publishing Exited (manager.go's forget), so losing this race is
		// itself proof the process exited before this call could subscribe.
		return true, "exited"
	}
	defer unsubscribe()

	deadline := time.After(earlyExitWindow)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return true, "exited"
			}
			if e, isExit := ev.(ptyterm.Exited); isExit {
				return true, e.Reason
			}
		case <-deadline:
			return false, ""
		}
	}
}

// resolveServers resolves a workspace's declared MCP ids through the store's
// catalogue. An id with no catalogue entry is simply omitted here; Generate
// itself reports it in MissingMCPs by comparing ws.MCPs against this map, so
// there is nothing to track twice.
func (s *AgentWorkspacesService) resolveServers(ws agentws.Workspace) map[string]mcpcatalog.Server {
	byID := make(map[string]mcpcatalog.Server, len(ws.MCPs))
	for _, entry := range s.store.Catalogue() {
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

// resolveSkills renders a workspace's declared skill slugs through
// SkillsService.RenderSkill, which takes the underlying prompt id
// ("http-api") rather than the installed slug ("hive-http-api") skillSlug
// mints — so a declared slug is unminted here before rendering.
func (s *AgentWorkspacesService) resolveSkills(ctx context.Context, ws agentws.Workspace) ([]agentws.RenderedSkill, error) {
	if len(ws.Skills) == 0 {
		return nil, nil
	}
	rendered := make([]agentws.RenderedSkill, 0, len(ws.Skills))
	for _, slug := range ws.Skills {
		id, ok := skillIDFromSlug(slug)
		if !ok {
			return nil, Errorf(KindInvalid, "workspace %q: unknown skill %q", ws.Dir, slug)
		}
		name, body, err := s.skills.RenderSkill(ctx, id)
		if err != nil {
			return nil, Wrap(err, KindInvalid, "workspace %q: rendering skill %q", ws.Dir, slug)
		}
		rendered = append(rendered, agentws.RenderedSkill{Slug: name, Body: body})
	}
	return rendered, nil
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
// launchTerminal's view, this never launches anything, so ResumeAttempted and
// Notice stay zero-valued.
func (s *AgentWorkspacesService) sessionView(rec store.AgentWorkspaceSession) SessionView {
	id := terminalID(rec.ID)
	live := ""
	if _, err := s.terminals.Get(id); err == nil {
		live = id
	}
	return SessionView{
		ID: rec.ID, Workspace: rec.Workspace, Name: rec.Name, Agent: rec.Agent,
		LastOpenedAt: rec.LastOpenedAt, TerminalID: live,
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
		Autonomy: string(st.Workspace.Autonomy), MCPs: st.Workspace.MCPs, Problem: problem,
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

// terminalID derives a session's ptyterm id from its record id, so a resume
// within one run reattaches rather than spawning beside itself, and so
// ptyterm's own minted namespace ("t<N>") is never touched.
func terminalID(recordID int64) string {
	return fmt.Sprintf("agentws-%d", recordID)
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
