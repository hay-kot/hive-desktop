package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// newTestAgentWorkspacesService builds a service over root with a real
// ptyterm.Manager (a plain /bin/sh, matching ptyterm's own tests), a real
// sqlite-backed store.DB, and a real SkillsService (newTestSkillsService).
// commands stands in for agentCommands(hiveCfg) — the caller picks which
// agent keys are "configured" and what they run.
func newTestAgentWorkspacesService(t *testing.T, root string, commands map[string]string) *AgentWorkspacesService {
	t.Helper()

	db, err := store.Open(t.Context(), t.TempDir(), store.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	manager := ptyterm.NewManager(ptyterm.ManagerOptions{Shell: []string{"/bin/sh"}})
	t.Cleanup(func() { _ = manager.Stop(t.Context()) })

	awStore := agentws.NewStore(root)
	require.NoError(t, awStore.Reload())

	return newAgentWorkspacesService(awStore, manager, db, newTestSkillsService(t), commands, "")
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
	assert.Len(t, svc.terminals.List(), 1, "reattaching must not spawn a second terminal")
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
	require.Len(t, svc.terminals.List(), 2)

	require.NoError(t, svc.DeleteSession(t.Context(), s1.ID))
	_, ok, err := svc.db.GetAgentWorkspaceSession(t.Context(), s1.ID)
	require.NoError(t, err)
	assert.False(t, ok, "the record is gone too")
	assert.Len(t, svc.terminals.List(), 1)

	require.NoError(t, svc.DeleteWorkspace(t.Context(), "demo"))
	_, ok, err = svc.db.GetAgentWorkspaceSession(t.Context(), s2.ID)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, svc.terminals.List(), "every live terminal the workspace held is gone")
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
	assert.Empty(t, svc.terminals.List(), "ptyterm has already forgotten the exited terminal")
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
