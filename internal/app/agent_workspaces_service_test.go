package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/execenv"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/app/tmuxcc"
)

// newTestAgentWorkspacesService builds a service over root with a real
// tmuxcc.Manager pointed at a private tmux server (requireTmux/privateTmux,
// terminals_service_test.go — a session's liveness is a real tmux fact, and a
// faked one would only prove the fake), a real sqlite-backed store.DB, and a
// real SkillsService (newTestSkillsService). commands stands in for
// agentCommands(hiveCfg) — the caller picks which agent keys are "configured"
// and what they run.
func newTestAgentWorkspacesService(t *testing.T, root string, commands map[string]string) *AgentWorkspacesService {
	t.Helper()
	privateTmux(t)

	db, err := store.Open(t.Context(), t.TempDir(), store.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	manager := tmuxcc.NewManager(t.Context(), tmuxcc.ManagerOptions{Logger: zerolog.Nop()})
	t.Cleanup(func() { _ = manager.Stop(context.WithoutCancel(t.Context())) })

	awStore := agentws.NewStore(root)
	require.NoError(t, awStore.Reload())

	return newAgentWorkspacesService(awStore, manager, db, newTestSkillsService(t), commands, "", nil, nil)
}

// liveAgentSessionCount is the test-side equivalent of the service's own
// liveSessionCount, used to assert how many agentws-* tmux sessions a call
// left running.
func liveAgentSessionCount(t *testing.T, svc *AgentWorkspacesService) int {
	t.Helper()
	names, err := svc.terminals.SessionNames(t.Context(), agentSessionPrefix)
	require.NoError(t, err)
	return len(names)
}

func writeAgentWorkspaceManifest(t *testing.T, root, dir, body string) string {
	t.Helper()
	wsDir := filepath.Join(root, dir)
	require.NoError(t, os.MkdirAll(wsDir, 0o700))
	path := filepath.Join(wsDir, "agent-workspace.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return wsDir
}

func writeMCPLibrary(t *testing.T, root, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "mcps.yaml"), []byte(body), 0o600))
}

// fakeAgentBinary writes a script that ignores every argument it is invoked
// with and execs body -- standing in for a real agent CLI, which would
// otherwise be required to exercise a live, addressable terminal. Because the
// script never references its own $@, the autonomy/MCP/session flags
// agentws.Resolve appends have nowhere to land and cannot break it.
func fakeAgentBinary(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-agent")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexec "+body+"\n"), 0o755))
	return path
}

func TestOpenResolvesMCPsAndSkills(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeMCPLibrary(t, root, "version: 1\nservers:\n  my-user-mcp:\n    command: \"true\"\n")
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n"+
		"mcps:\n  - playwright\n  - my-user-mcp\nskills:\n  - hive-http-api\n")

	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": "true"})

	result, err := svc.Open(t.Context(), "demo")
	require.NoError(t, err)
	assert.Empty(t, result.MissingMCPs)
	assert.Equal(t, "demo", result.Workspace.Dir)
	assert.Equal(t, "Demo", result.Workspace.Name)
	assert.Equal(t, "claude", result.Workspace.Agent)
	assert.ElementsMatch(t, []string{"playwright", "my-user-mcp"}, result.Workspace.MCPs)
	assert.Empty(t, result.Sessions)

	mcpJSON, err := os.ReadFile(filepath.Join(root, "demo", ".mcp.json"))
	require.NoError(t, err)
	assert.Contains(t, string(mcpJSON), "playwright")
	assert.Contains(t, string(mcpJSON), "my-user-mcp")

	codexTOML, err := os.ReadFile(filepath.Join(root, "demo", ".codex", "config.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(codexTOML), "mcp_servers.playwright")
	assert.Contains(t, string(codexTOML), "mcp_servers.my-user-mcp")

	skillFile, err := os.ReadFile(filepath.Join(root, "demo", ".claude", "skills", "hive-http-api", "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(skillFile), "name: hive-http-api")
}

func TestOpenOmitsAndReportsAnUnknownMCP(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\nmcps:\n  - playwright\n  - ghost\n")

	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": "true"})

	result, err := svc.Open(t.Context(), "demo")
	require.NoError(t, err, "an unknown mcp id does not fail the open")
	assert.Equal(t, []string{"ghost"}, result.MissingMCPs)

	mcpJSON, err := os.ReadFile(filepath.Join(root, "demo", ".mcp.json"))
	require.NoError(t, err)
	assert.Contains(t, string(mcpJSON), "playwright")
	assert.NotContains(t, string(mcpJSON), "ghost")
}

func TestOpenRejectsAWorkspaceOutsideTheRoot(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": "true"})

	for _, bad := range []string{"../escape", "/etc/passwd", "sub/dir", ".", ""} {
		_, err := svc.Open(t.Context(), bad)
		require.Error(t, err, "dir %q", bad)
		assert.Equal(t, KindInvalid, KindOf(err), "dir %q", bad)
	}

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries, "a rejected workspace argument must write nothing")
}

func TestResumeFallsBackToFreshLaunch(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: codex\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"codex": fakeAgentBinary(t, "cat")})

	started, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s1", Cols: 80, Rows: 24})
	require.NoError(t, err)
	require.NotEmpty(t, started.TerminalID)
	assert.True(t, started.ResumeAttempted, "codex has no resume form, but a fresh start always ResumeAttempted=true")

	closed, err := svc.CloseSession(t.Context(), started.ID)
	require.NoError(t, err)
	require.True(t, closed)

	resumed, err := svc.ResumeSession(t.Context(), started.ID, 80, 24)
	require.NoError(t, err)
	assert.False(t, resumed.ResumeAttempted, "codex cannot resume, so this launch could not even try")
	assert.NotEmpty(t, resumed.Notice)
	assert.NotEmpty(t, resumed.TerminalID, "a fresh relaunch still produces a live session")
}

func TestResumeReattachesTheSameTerminal(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": fakeAgentBinary(t, "cat")})

	started, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s1", Cols: 80, Rows: 24})
	require.NoError(t, err)
	require.NotEmpty(t, started.TerminalID)

	resumed, err := svc.ResumeSession(t.Context(), started.ID, 80, 24)
	require.NoError(t, err)
	assert.Equal(t, started.TerminalID, resumed.TerminalID, "a resume within one run reattaches rather than spawning beside it")
	assert.True(t, resumed.ResumeAttempted)
	assert.Positive(t, resumed.Cols, "an attach must report tmux's granted grid, or the pane opens at the wrong size")
	assert.Positive(t, resumed.Rows)
	assert.Equal(t, 1, liveAgentSessionCount(t, svc), "reattaching must not spawn a second terminal")
}

// TestResumeOfANeverMessagedClaudeSessionRelaunchesFresh is the reported bug:
// start a chat, send nothing, kill it, restart it. claude persists a
// conversation file only on the first message, so passing --resume for the
// untouched id would die in the pane on "No conversation found with session
// ID". The relaunch must go fresh instead — and silently, because a fresh
// launch IS the continuation of an empty conversation.
func TestResumeOfANeverMessagedClaudeSessionRelaunchesFresh(t *testing.T) {
	isolateConfig(t)
	claudeCfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeCfg)
	require.NoError(t, os.MkdirAll(filepath.Join(claudeCfg, "projects", "-elsewhere"), 0o700))

	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": fakeAgentBinary(t, "cat")})

	started, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s1", Cols: 80, Rows: 24})
	require.NoError(t, err)
	before, ok, err := svc.db.GetAgentWorkspaceSession(t.Context(), started.ID)
	require.NoError(t, err)
	require.True(t, ok)

	closed, err := svc.CloseSession(t.Context(), started.ID)
	require.NoError(t, err)
	require.True(t, closed)

	resumed, err := svc.ResumeSession(t.Context(), started.ID, 80, 24)
	require.NoError(t, err)
	assert.False(t, resumed.ResumeAttempted, "nothing was persisted, so this launch must not pass --resume")
	assert.Empty(t, resumed.Notice, "an empty conversation relaunching fresh is a continuation, not a loss to announce")
	assert.NotEmpty(t, resumed.TerminalID)

	after, ok, err := svc.db.GetAgentWorkspaceSession(t.Context(), started.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.NotEqual(t, before.AgentSessionID, after.AgentSessionID,
		"the fresh launch mints a fresh id — reusing one the agent might hold would wedge on 'already in use'")
}

// The counterpart: once the conversation file exists, a relaunch resumes it
// under the same id.
func TestResumeOfAMessagedClaudeSessionResumesById(t *testing.T) {
	isolateConfig(t)
	claudeCfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeCfg)

	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": fakeAgentBinary(t, "cat")})

	started, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s1", Cols: 80, Rows: 24})
	require.NoError(t, err)
	rec, ok, err := svc.db.GetAgentWorkspaceSession(t.Context(), started.ID)
	require.NoError(t, err)
	require.True(t, ok)

	projectDir := filepath.Join(claudeCfg, "projects", "-demo")
	require.NoError(t, os.MkdirAll(projectDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, rec.AgentSessionID+".jsonl"), []byte("{}\n"), 0o600))

	closed, err := svc.CloseSession(t.Context(), started.ID)
	require.NoError(t, err)
	require.True(t, closed)

	resumed, err := svc.ResumeSession(t.Context(), started.ID, 80, 24)
	require.NoError(t, err)
	assert.True(t, resumed.ResumeAttempted)
	assert.Empty(t, resumed.Notice)

	after, ok, err := svc.db.GetAgentWorkspaceSession(t.Context(), started.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, rec.AgentSessionID, after.AgentSessionID, "a real resume keeps addressing the same conversation")
}

func TestCloseSessionEndsTheTerminalAndKeepsTheRecord(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": fakeAgentBinary(t, "cat")})

	started, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s1", Cols: 80, Rows: 24})
	require.NoError(t, err)
	require.NotEmpty(t, started.TerminalID)

	closed, err := svc.CloseSession(t.Context(), started.ID)
	require.NoError(t, err)
	assert.True(t, closed)

	result, err := svc.Open(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	assert.Empty(t, result.Sessions[0].TerminalID, "the PTY is gone")
	assert.Equal(t, "s1", result.Sessions[0].Name, "the record still lists")
}

func TestDeleteEndsLiveTerminals(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": fakeAgentBinary(t, "cat")})

	s1, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s1", Cols: 80, Rows: 24})
	require.NoError(t, err)
	s2, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s2", Cols: 80, Rows: 24})
	require.NoError(t, err)
	require.Equal(t, 2, liveAgentSessionCount(t, svc))

	require.NoError(t, svc.DeleteSession(t.Context(), s1.ID))
	_, ok, err := svc.db.GetAgentWorkspaceSession(t.Context(), s1.ID)
	require.NoError(t, err)
	assert.False(t, ok, "the record is gone too")
	assert.Equal(t, 1, liveAgentSessionCount(t, svc))

	require.NoError(t, svc.DeleteWorkspace(t.Context(), "demo"))
	_, ok, err = svc.db.GetAgentWorkspaceSession(t.Context(), s2.ID)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, 0, liveAgentSessionCount(t, svc), "every live terminal the workspace held is gone")
}

func TestStartSessionReportsAnImmediateExit(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": "this-binary-does-not-exist-anywhere-12345"})

	started, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s1", Cols: 80, Rows: 24})
	require.NoError(t, err, "a command not on PATH is a live shell that exits 127, not an exec error")
	assert.Empty(t, started.TerminalID, "a dead terminal must not be reported as live")
	assert.NotEmpty(t, started.Notice)
	assert.Equal(t, 0, liveAgentSessionCount(t, svc), "tmux already ended the session when its command exited")
}

func TestLaunchRefusesAnUnknownAgent(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()

	t.Run("AbsentFromHiveConfig", func(t *testing.T) {
		writeAgentWorkspaceManifest(t, root, "unknown-to-hive", "version: 1\nname: Demo\nagent: mystery-agent\nautonomy: ask\n")
		svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": "true"})

		_, err := svc.StartSession(t.Context(), StartSession{Workspace: "unknown-to-hive", Name: "s1", Cols: 80, Rows: 24})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mystery-agent")
		assert.Equal(t, KindInvalid, KindOf(err))
	})

	// "pi" is present in the caller's config but has no entry in agentws's
	// launch table at all -- the fail-closed path a fourth, unimplemented
	// agent takes.
	t.Run("AbsentFromLaunchTable", func(t *testing.T) {
		writeAgentWorkspaceManifest(t, root, "unknown-to-agentws", "version: 1\nname: Demo\nagent: pi\nautonomy: ask\n")
		svc := newTestAgentWorkspacesService(t, root, map[string]string{"pi": "true"})

		_, err := svc.StartSession(t.Context(), StartSession{Workspace: "unknown-to-agentws", Name: "s1", Cols: 80, Rows: 24})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pi")
		assert.Equal(t, KindInvalid, KindOf(err))
	})
}

func TestTwoSessionsMayShareAName(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": fakeAgentBinary(t, "cat")})

	first, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "dup", Cols: 80, Rows: 24})
	require.NoError(t, err)
	second, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "dup", Cols: 80, Rows: 24})
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, second.ID, "the row id is the identity, not the name")
	assert.NotEqual(t, first.TerminalID, second.TerminalID)

	result, err := svc.Open(t.Context(), "demo")
	require.NoError(t, err)
	assert.Len(t, result.Sessions, 2)
}

func TestSessionsListsWithoutRegeneratingArtifacts(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": fakeAgentBinary(t, "cat")})

	started, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s1", Cols: 80, Rows: 24})
	require.NoError(t, err)

	sessions, err := svc.Sessions(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, started.ID, sessions[0].ID)
	assert.Equal(t, started.TerminalID, sessions[0].TerminalID)

	_, err = os.Stat(filepath.Join(root, "demo", ".mcp.json"))
	assert.True(t, os.IsNotExist(err), "Sessions must not regenerate the workspace's disposable artifacts")

	_, err = svc.Sessions(t.Context(), "../escape")
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err))
}

func TestDeleteWorkspaceRemovesRecordsAndLeavesTheDirectory(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": "true"})

	_, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s1", Cols: 80, Rows: 24})
	require.NoError(t, err)

	require.NoError(t, svc.DeleteWorkspace(t.Context(), "demo"))

	sessions, err := svc.db.ListAgentWorkspaceSessions(t.Context(), "demo")
	require.NoError(t, err)
	assert.Empty(t, sessions)

	assert.DirExists(t, filepath.Join(root, "demo"))
	assert.FileExists(t, filepath.Join(root, "demo", "agent-workspace.yaml"))
}

func TestAllSessionsSpansEveryWorkspaceInStableCreationOrder(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo-a", "version: 1\nname: Demo A\nagent: claude\nautonomy: ask\n")
	writeAgentWorkspaceManifest(t, root, "demo-b", "version: 1\nname: Demo B\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": fakeAgentBinary(t, "cat")})

	a1, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo-a", Name: "a1", Cols: 80, Rows: 24})
	require.NoError(t, err)
	b1, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo-b", Name: "b1", Cols: 80, Rows: 24})
	require.NoError(t, err)

	// Resuming a1 touches last_opened_at but must not move its row: the list
	// keeps creation order so the sidebar never reshuffles under the pointer.
	_, err = svc.ResumeSession(t.Context(), a1.ID, 80, 24)
	require.NoError(t, err)

	all, err := svc.AllSessions(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, b1.ID, all[0].ID)
	assert.Equal(t, "demo-b", all[0].Workspace)
	assert.Equal(t, a1.ID, all[1].ID)
	assert.Equal(t, "demo-a", all[1].Workspace)
}

func TestRenameSession(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": fakeAgentBinary(t, "cat")})

	started, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "New Chat", Cols: 80, Rows: 24})
	require.NoError(t, err)

	require.NoError(t, svc.RenameSession(t.Context(), started.ID, "  fix the flaky test  "))
	sessions, err := svc.Sessions(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "fix the flaky test", sessions[0].Name, "the stored name is trimmed")
	assert.Equal(t, started.TerminalID, sessions[0].TerminalID, "the live terminal is untouched by a rename")

	err = svc.RenameSession(t.Context(), started.ID, "   ")
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err))

	err = svc.RenameSession(t.Context(), 999999, "ghost")
	require.Error(t, err)
	assert.Equal(t, KindNotFound, KindOf(err))
}

func TestCreateAndUpdateWorkspace(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": "true", "codex": "true"})

	created, err := svc.CreateWorkspace(t.Context(), WorkspaceEdit{Dir: "demo", Name: "Demo", Agent: "claude", Autonomy: "ask"})
	require.NoError(t, err)
	assert.Equal(t, "demo", created.Dir)
	assert.Equal(t, "Demo", created.Name)
	assert.Equal(t, "claude", created.Agent)
	assert.Equal(t, "ask", created.Autonomy)
	assert.FileExists(t, filepath.Join(root, "demo", "agent-workspace.yaml"))

	_, err = svc.CreateWorkspace(t.Context(), WorkspaceEdit{Dir: "demo", Name: "Demo", Agent: "claude", Autonomy: "ask"})
	require.ErrorContains(t, err, "already exists")

	updated, err := svc.UpdateWorkspace(t.Context(), WorkspaceEdit{Dir: "demo", Name: "Renamed", Agent: "codex", Autonomy: "auto"})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.Equal(t, "codex", updated.Agent)
	assert.Equal(t, "auto", updated.Autonomy)

	list, err := svc.List(t.Context())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Renamed", list[0].Name)

	_, err = svc.UpdateWorkspace(t.Context(), WorkspaceEdit{Dir: "demo", Name: "X", Agent: "no-such-agent", Autonomy: "ask"})
	require.ErrorContains(t, err, "not configured")
	_, err = svc.UpdateWorkspace(t.Context(), WorkspaceEdit{Dir: "missing", Name: "X", Agent: "claude", Autonomy: "ask"})
	require.ErrorContains(t, err, "not found")

	assert.Equal(t, []string{"claude", "codex"}, svc.Agents(t.Context()))
}

func TestWorkspaceEditOwnsTheMCPList(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": "true"})

	created, err := svc.CreateWorkspace(t.Context(), WorkspaceEdit{Dir: "demo", Name: "Demo", Agent: "claude", Autonomy: "ask", MCPs: []string{"playwright"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"playwright"}, created.MCPs)
	assert.FileExists(t, filepath.Join(root, "demo", "AGENTS.md"), "a created workspace starts with prose to shape")

	updated, err := svc.UpdateWorkspace(t.Context(), WorkspaceEdit{Dir: "demo", Name: "Demo", Agent: "claude", Autonomy: "ask"})
	require.NoError(t, err)
	assert.Empty(t, updated.MCPs)

	_, err = svc.UpdateWorkspace(t.Context(), WorkspaceEdit{Dir: "demo", Name: "Demo", Agent: "claude", Autonomy: "ask", MCPs: []string{"playwright", "playwright"}})
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err))
}

func TestImportAndRemoveMCPServers(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": "true"})

	added, err := svc.ImportMCPServers(t.Context(), `{"mcpServers": {"paperless": {"command": "uvx", "args": ["paperless-mcp"]}}}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"paperless"}, added)

	catalogue := svc.MCPCatalogue(t.Context())
	byID := make(map[string]MCPCatalogueItem, len(catalogue))
	for _, item := range catalogue {
		byID[item.ID] = item
	}
	require.Contains(t, byID, "paperless")
	assert.Equal(t, "uvx paperless-mcp", byID["paperless"].Command)
	assert.False(t, byID["paperless"].Shipped)
	require.Contains(t, byID, "playwright", "the shipped catalogue is part of the merged view")
	assert.True(t, byID["playwright"].Shipped)

	_, err = svc.ImportMCPServers(t.Context(), `{"paperless": {"command": "uvx"}}`)
	require.Error(t, err)
	assert.Equal(t, KindConflict, KindOf(err))

	require.NoError(t, svc.RemoveMCPServer(t.Context(), "paperless"))
	err = svc.RemoveMCPServer(t.Context(), "paperless")
	require.Error(t, err)
	assert.Equal(t, KindNotFound, KindOf(err))

	err = svc.RemoveMCPServer(t.Context(), "playwright")
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err), "a shipped entry is disabled per workspace, never removed")
}

func TestOpenWorkspaceInEditor(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": "true"})

	err := svc.OpenWorkspaceInEditor(t.Context(), "demo")
	require.Error(t, err, "no editor configured")
	assert.Equal(t, KindInvalid, KindOf(err))

	err = svc.RevealWorkspace(t.Context(), "ghost")
	require.Error(t, err)
	assert.Equal(t, KindNotFound, KindOf(err))

	fake := fakeAgentBinary(t, "true")
	svc.execEnv = execenv.NewResolver(execenv.Options{Shell: "/bin/sh", Probe: func(context.Context, string) (string, error) {
		return "/usr/bin:/bin", nil
	}})
	svc.editorCommand = func(context.Context) (string, error) { return fake, nil }
	command, title := svc.Editor(t.Context())
	assert.Equal(t, fake, command)
	assert.Equal(t, fake, title, "a command outside the known catalogue labels itself")
	require.NoError(t, svc.OpenWorkspaceInEditor(t.Context(), "demo"))
}

func TestResizeSessionResizesTheLiveTerminal(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeAgentWorkspaceManifest(t, root, "demo", "version: 1\nname: Demo\nagent: claude\nautonomy: ask\n")
	svc := newTestAgentWorkspacesService(t, root, map[string]string{"claude": fakeAgentBinary(t, "cat")})

	started, err := svc.StartSession(t.Context(), StartSession{Workspace: "demo", Name: "s1", Cols: 80, Rows: 24})
	require.NoError(t, err)
	require.NotEmpty(t, started.TerminalID)

	require.NoError(t, svc.ResizeSession(t.Context(), started.ID, 120, 30))

	client, ok := svc.terminals.Client(started.TerminalID)
	require.True(t, ok)
	// The %layout-change answering the vote is asynchronous; the window
	// settles shortly after. Width is the assertable half — tmux may keep a
	// row for its status line, so height is its call.
	require.Eventually(t, func() bool {
		windows := client.Windows()
		return len(windows) > 0 && windows[0].Width == 120
	}, 3*time.Second, 25*time.Millisecond, "the size vote must reach the tmux window")

	err = svc.ResizeSession(t.Context(), started.ID+999, 80, 24)
	require.Error(t, err)
}
