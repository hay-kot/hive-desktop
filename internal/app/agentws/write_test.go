package agentws

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteManifestPreservesCommentsAndUntouchedKeys(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	require.NoError(t, os.Mkdir(dir, 0o700))
	// Current-version, because MigrateRoot runs at startup before anything
	// loads a manifest: WriteManifest only stamps a version onto a file it
	// creates, so an editable file is always already at Current.
	original := `# hand-authored: do not lose me
version: 2
name: Demo
# the agent that runs here
agent: claude
autonomy: ask
mcps:
  - hass-mcp # lights and sensors
skills:
  - pdf-tools
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFileName), []byte(original), 0o600))

	require.NoError(t, WriteManifest(root, "demo", "Renamed", "codex", AutonomyAuto, []string{"hass-mcp"}))

	raw, err := os.ReadFile(filepath.Join(dir, manifestFileName))
	require.NoError(t, err)
	text := string(raw)
	assert.Contains(t, text, "# hand-authored: do not lose me")
	assert.Contains(t, text, "# the agent that runs here")
	assert.Contains(t, text, "pdf-tools")

	ws, err := parseWorkspace([]byte(text))
	require.NoError(t, err)
	assert.Equal(t, "Renamed", ws.Name)
	assert.Equal(t, "codex", ws.Agent)
	assert.Equal(t, AutonomyAuto, ws.Autonomy)
	assert.Equal(t, []string{"hass-mcp"}, ws.MCPs)
}

func TestWriteManifestOwnsTheMCPsKey(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	require.NoError(t, os.Mkdir(dir, 0o700))
	original := "version: 2\nname: Demo\nagent: claude\nautonomy: ask\nmcps:\n  - playwright\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFileName), []byte(original), 0o600))

	require.NoError(t, WriteManifest(root, "demo", "Demo", "claude", AutonomyAsk, []string{"playwright", "home-assistant"}))
	raw, err := os.ReadFile(filepath.Join(dir, manifestFileName))
	require.NoError(t, err)
	ws, err := parseWorkspace(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"playwright", "home-assistant"}, ws.MCPs)

	require.NoError(t, WriteManifest(root, "demo", "Demo", "claude", AutonomyAsk, nil))
	raw, err = os.ReadFile(filepath.Join(dir, manifestFileName))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "mcps", "an empty set removes the key rather than writing mcps: []")
	ws, err = parseWorkspace(raw)
	require.NoError(t, err)
	assert.Empty(t, ws.MCPs)
}

func TestCreateWorkspaceWritesALoadableManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, CreateWorkspace(root, "fresh", "Fresh", "claude", AutonomyAsk, []string{"playwright"}))

	raw, err := os.ReadFile(filepath.Join(root, "fresh", manifestFileName))
	require.NoError(t, err)
	ws, err := parseWorkspace(raw)
	require.NoError(t, err)
	assert.Equal(t, "Fresh", ws.Name)
	assert.Equal(t, "claude", ws.Agent)
	assert.Equal(t, AutonomyAsk, ws.Autonomy)
	assert.Equal(t, []string{"playwright"}, ws.MCPs)

	err = CreateWorkspace(root, "fresh", "Fresh", "claude", AutonomyAsk, nil)
	require.ErrorIs(t, err, os.ErrExist)
}

func TestCreateWorkspaceScaffoldsAgentsMD(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, CreateWorkspace(root, "fresh", "Paperless", "claude", AutonomyAsk, nil))

	raw, err := os.ReadFile(filepath.Join(root, "fresh", "AGENTS.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "# Paperless")
	assert.Contains(t, string(raw), "agent workspace")

	// The scaffold is authored from the moment it lands: nothing regenerates
	// it, so a Generate over the workspace copies it to CLAUDE.md verbatim.
	result, err := Generate(GenerateInput{Dir: filepath.Join(root, "fresh")})
	require.NoError(t, err)
	assert.Empty(t, result.Problems)
	claude, err := os.ReadFile(filepath.Join(root, "fresh", "CLAUDE.md"))
	require.NoError(t, err)
	assert.Equal(t, string(raw), string(claude))
}
