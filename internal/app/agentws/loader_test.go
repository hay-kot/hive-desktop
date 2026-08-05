package agentws

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/hay-kot/hive-desktop/internal/app/configmigrate"
)

// TestConfigRoundTrip proves parse -> validate -> serialise for both files
// (spec §13): a Workspace/Library value, marshalled and re-parsed, comes back
// equal.
func TestConfigRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("Workspace", func(t *testing.T) {
		t.Parallel()

		w := Workspace{
			Version:  configmigrate.AgentWorkspaceSet.Current,
			Name:     "Home Assistant",
			Agent:    "claude",
			Autonomy: AutonomyAsk,
			MCPs:     []string{"home-assistant"},
			Skills:   []string{"hive-mcp"},
		}
		require.NoError(t, w.Validate())

		data, err := yaml.Marshal(w)
		require.NoError(t, err)

		parsed, err := parseWorkspace(data)
		require.NoError(t, err)
		parsed.Dir = "" // yaml:"-"; parseWorkspace never sets it
		assert.Equal(t, w, parsed)
	})

	t.Run("Library", func(t *testing.T) {
		t.Parallel()

		l := Library{
			Version: configmigrate.MCPLibrarySet.Current,
			Servers: map[string]MCPServer{
				"home-assistant": {
					Title:   "Home Assistant",
					Type:    "http",
					URL:     "http://homeassistant.local:8123/mcp",
					Headers: map[string]string{"Authorization": "Bearer token"},
				},
				"local-tool": {
					Command: "npx",
					Args:    []string{"-y", "@example/mcp"},
					Env:     map[string]string{"TOKEN": "abc"},
				},
			},
		}
		require.NoError(t, l.Validate())

		data, err := yaml.Marshal(l)
		require.NoError(t, err)

		parsed, err := parseLibrary(data)
		require.NoError(t, err)
		assert.Equal(t, l, parsed)
	})
}

func TestStrictDecodeRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	t.Run("Workspace", func(t *testing.T) {
		t.Parallel()
		_, err := parseWorkspace([]byte("version: 2\nname: X\nagent: claude\nautonomy: ask\nfoo: bar\n"))
		require.Error(t, err)
	})

	t.Run("Library", func(t *testing.T) {
		t.Parallel()
		_, err := parseLibrary([]byte("version: 1\nservers: {}\nfoo: bar\n"))
		require.Error(t, err)
	})
}

func TestLoadWorkspaceSetsDirFromPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "homeassistant")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, manifestFileName)
	require.NoError(t, os.WriteFile(path, []byte("version: 2\nname: X\nagent: claude\nautonomy: ask\n"), 0o600))

	w, err := LoadWorkspace(path)
	require.NoError(t, err)
	assert.Equal(t, "homeassistant", w.Dir)
}

// TestAutonomyDefaultsToAsk asserts hc-ou4o02zx §4: a manifest that omits
// autonomy loads as AutonomyAsk rather than failing.
func TestAutonomyDefaultsToAsk(t *testing.T) {
	t.Parallel()

	w, err := parseWorkspace([]byte("version: 2\nname: X\nagent: claude\n"))
	require.NoError(t, err)
	assert.Equal(t, AutonomyAsk, w.Autonomy)
}

func TestLoadWorkspaceMissingFileWrapsNotExist(t *testing.T) {
	t.Parallel()

	_, err := LoadWorkspace(filepath.Join(t.TempDir(), "nope", manifestFileName))
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoadLibraryMissingFileWrapsNotExist(t *testing.T) {
	t.Parallel()

	_, err := LoadLibrary(filepath.Join(t.TempDir(), libraryFileName))
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}
