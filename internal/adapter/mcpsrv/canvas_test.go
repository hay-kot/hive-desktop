package mcpsrv_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/adapter/mcpsrv"
	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/mcpcatalog"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// testCanvasSession is testSession's canvas-server twin: same app, same
// in-memory transport, the hive-canvas tool table under test instead.
func testCanvasSession(t *testing.T) (*app.App, *mcp.ClientSession) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, "feed")

	core, err := app.New(t.Context(), app.Config{
		Settings: settings.DefaultSettings(),
		MockMode: settings.MockMode(),
		Logger:   zerolog.Nop(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })

	srv := mcpsrv.NewCanvas(core, zerolog.Nop(), mcpsrv.Options{Version: "test"}).Server()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	_, err = srv.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).
		Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return core, session
}

func seedAgentSession(t *testing.T, core *app.App, workspace, name string) int64 {
	t.Helper()
	rec, err := core.Store.CreateAgentWorkspaceSession(t.Context(), store.AgentWorkspaceSession{
		Workspace: workspace, Name: name, Agent: "claude", CreatedAt: 1, LastOpenedAt: 1,
	})
	require.NoError(t, err)
	return rec.ID
}

type canvasView struct {
	Workspace string `json:"workspace"`
	Session   int64  `json:"session"`
	Blocks    []struct {
		ID    string `json:"id"`
		Kind  string `json:"kind"`
		Title string `json:"title"`
		Body  string `json:"body"`
		URL   string `json:"url"`
	} `json:"blocks"`
}

func TestCanvasToolsListDeclaresEveryToolWithAnObjectInputSchema(t *testing.T) {
	_, session := testCanvasSession(t)

	res, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)

	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		assert.NotEmpty(t, tool.Description, "tool %s has no description", tool.Name)

		require.NotNil(t, tool.InputSchema, "tool %s has no input schema", tool.Name)
		schema, ok := tool.InputSchema.(map[string]any)
		require.True(t, ok, "tool %s input schema is not an object", tool.Name)
		assert.Equal(t, "object", schema["type"], "tool %s input schema is not type object", tool.Name)
	}

	assert.ElementsMatch(t, []string{"put_block", "remove_block", "clear_canvas", "read_canvas"}, names)
}

func TestCanvasRoundTrip(t *testing.T) {
	core, session := testCanvasSession(t)
	id := seedAgentSession(t, core, "demo", "chat")

	var got canvasView
	call(t, session, "read_canvas", map[string]any{"session": id}, &got)
	assert.Equal(t, "demo", got.Workspace)
	assert.Empty(t, got.Blocks, "a never-written canvas answers empty, not an error")

	call(t, session, "put_block", map[string]any{
		"session": id, "id": "status", "kind": "markdown", "title": "Progress", "body": "working…",
	}, &got)
	call(t, session, "put_block", map[string]any{
		"session": id, "id": "pr", "kind": "link", "title": "The PR", "url": "https://example.com/pr/1",
	}, &got)
	require.Len(t, got.Blocks, 2)

	// Same id revises in place: position and count hold, content changes.
	call(t, session, "put_block", map[string]any{
		"session": id, "id": "status", "kind": "markdown", "body": "done",
	}, &got)
	require.Len(t, got.Blocks, 2)
	assert.Equal(t, "status", got.Blocks[0].ID)
	assert.Equal(t, "done", got.Blocks[0].Body)

	call(t, session, "remove_block", map[string]any{"session": id, "id": "pr"}, &got)
	require.Len(t, got.Blocks, 1)

	call(t, session, "clear_canvas", map[string]any{"session": id}, &got)
	assert.Empty(t, got.Blocks)

	call(t, session, "read_canvas", map[string]any{"session": id}, &got)
	assert.Empty(t, got.Blocks)
}

func TestCanvasToolErrors(t *testing.T) {
	core, session := testCanvasSession(t)
	id := seedAgentSession(t, core, "demo", "chat")

	text := callErr(t, session, "read_canvas", map[string]any{"session": 999})
	assert.Contains(t, text, "not_found")

	text = callErr(t, session, "remove_block", map[string]any{"session": id, "id": "ghost"})
	assert.Contains(t, text, "not_found")

	text = callErr(t, session, "put_block", map[string]any{"session": id, "id": "a", "kind": "html", "body": "x"})
	assert.Contains(t, text, "invalid")

	text = callErr(t, session, "put_block", map[string]any{
		"session": id, "id": "a", "kind": "link", "title": "x", "url": "javascript:alert(1)",
	})
	assert.Contains(t, text, "invalid")
}

// TestCanvasCatalogueAgreement pins the adapter's mount paths to the
// RuntimePaths mcpcatalog declares — the literals live in core so the
// registry stays adapter-free, and this is what keeps them from drifting.
func TestCanvasCatalogueAgreement(t *testing.T) {
	desktop, ok := mcpcatalog.Lookup("hive-desktop")
	require.True(t, ok)
	assert.Equal(t, mcpsrv.PathPrefix, desktop.RuntimePath)

	canvasEntry, ok := mcpcatalog.Lookup("hive-canvas")
	require.True(t, ok)
	assert.Equal(t, mcpsrv.CanvasPathPrefix, canvasEntry.RuntimePath)
}

// The canvas mount is a second exact-match mux entry beside /mcp; each server
// answers only its own tools, so the two mounts must not shadow each other.
func TestCanvasMountedBesideTheDesktopServer(t *testing.T) {
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, "feed")
	t.Setenv(settings.EnvHTTPEnabled, "true")
	t.Setenv(settings.EnvHTTPPort, "0")

	cfg, err := settings.NewStore(filepath.Join(root, "config", "settings.yaml")).Effective()
	require.NoError(t, err)
	core, err := app.New(t.Context(), app.Config{Settings: cfg, MockMode: cfg.MockMode(), Logger: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })

	require.True(t, core.MountAPI(mcpsrv.PathPrefix,
		mcpsrv.New(core, zerolog.Nop(), mcpsrv.Options{Version: "test"}).Handler()))
	require.True(t, core.MountAPI(mcpsrv.CanvasPathPrefix,
		mcpsrv.NewCanvas(core, zerolog.Nop(), mcpsrv.Options{Version: "test"}).Handler()))
	require.NoError(t, core.Start(t.Context()))

	id := seedAgentSession(t, core, "demo", "chat")

	running, port := core.Webhooks.Endpoint(t.Context())
	require.True(t, running)

	canvasSession, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).
		Connect(t.Context(), &mcp.StreamableClientTransport{
			Endpoint:             fmt.Sprintf("http://127.0.0.1:%d%s", port, mcpsrv.CanvasPathPrefix),
			DisableStandaloneSSE: true,
		}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = canvasSession.Close() })

	var got canvasView
	call(t, canvasSession, "read_canvas", map[string]any{"session": id}, &got)
	assert.Equal(t, "demo", got.Workspace)

	// The desktop server still answers at its own path with its own table.
	desktopSession, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).
		Connect(t.Context(), &mcp.StreamableClientTransport{
			Endpoint:             fmt.Sprintf("http://127.0.0.1:%d%s", port, mcpsrv.PathPrefix),
			DisableStandaloneSSE: true,
		}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = desktopSession.Close() })

	tools, err := desktopSession.ListTools(t.Context(), nil)
	require.NoError(t, err)
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	assert.Contains(t, names, "get_status")
	assert.NotContains(t, names, "put_block", "the canvas table must not leak onto the app-control server")
}
