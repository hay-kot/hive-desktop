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
	original := `# hand-authored: do not lose me
version: 1
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

	require.NoError(t, WriteManifest(root, "demo", "Renamed", "codex", AutonomyAuto))

	raw, err := os.ReadFile(filepath.Join(dir, manifestFileName))
	require.NoError(t, err)
	text := string(raw)
	assert.Contains(t, text, "# hand-authored: do not lose me")
	assert.Contains(t, text, "# the agent that runs here")
	assert.Contains(t, text, "# lights and sensors")
	assert.Contains(t, text, "pdf-tools")

	ws, err := parseWorkspace([]byte(text))
	require.NoError(t, err)
	assert.Equal(t, "Renamed", ws.Name)
	assert.Equal(t, "codex", ws.Agent)
	assert.Equal(t, AutonomyAuto, ws.Autonomy)
	assert.Equal(t, []string{"hass-mcp"}, ws.MCPs)
}

func TestCreateWorkspaceWritesALoadableManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, CreateWorkspace(root, "fresh", "Fresh", "claude", AutonomyAsk))

	raw, err := os.ReadFile(filepath.Join(root, "fresh", manifestFileName))
	require.NoError(t, err)
	ws, err := parseWorkspace(raw)
	require.NoError(t, err)
	assert.Equal(t, "Fresh", ws.Name)
	assert.Equal(t, "claude", ws.Agent)
	assert.Equal(t, AutonomyAsk, ws.Autonomy)

	err = CreateWorkspace(root, "fresh", "Fresh", "claude", AutonomyAsk)
	require.ErrorIs(t, err, os.ErrExist)
}
