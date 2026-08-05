package agentws

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedDefaultsIfMissing(t *testing.T) {
	t.Parallel()

	t.Run("SeedsMCPsYAMLAndSharedSkills", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		installed, err := SeedDefaultsIfMissing(root)
		require.NoError(t, err)
		assert.True(t, installed)

		lib, err := LoadLibrary(filepath.Join(root, libraryFileName))
		require.NoError(t, err)
		assert.Equal(t, 1, lib.Version)
		assert.Empty(t, lib.Servers)

		info, err := os.Stat(filepath.Join(root, ".shared", "skills"))
		require.NoError(t, err)
		assert.True(t, info.IsDir())

		_, err = os.Stat(filepath.Join(root, ".shared", "prompts"))
		assert.True(t, os.IsNotExist(err), ".shared/prompts/ must not be created")
	})

	t.Run("NeverReplacesAnExistingFile", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		require.NoError(t, os.MkdirAll(root, 0o700))
		path := filepath.Join(root, libraryFileName)
		require.NoError(t, os.WriteFile(path, []byte("not even valid yaml: ["), 0o600))

		installed, err := SeedDefaultsIfMissing(root)
		require.NoError(t, err)
		assert.False(t, installed)

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "not even valid yaml: [", string(data))
	})
}

func TestSeedCreatesTheHiveWorkspaceOnRootCreation(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "workspaces")
	created, err := EnsureRoot(root)
	require.NoError(t, err)
	require.True(t, created)

	require.NoError(t, SeedHiveWorkspace(root))

	manifestPath := filepath.Join(root, "hive", manifestFileName)
	ws, err := LoadWorkspace(manifestPath)
	require.NoError(t, err)
	require.NoError(t, ws.Validate())
	assert.Equal(t, AutonomyAsk, ws.Autonomy)
	assert.Equal(t, []string{"hive-mcp"}, ws.Skills)

	_, err = os.Stat(filepath.Join(root, "hive", "AGENTS.md"))
	require.NoError(t, err)
}

func TestSyncHiveWorkspaceSkills(t *testing.T) {
	t.Parallel()

	seededRoot := func(t *testing.T) string {
		t.Helper()
		root := filepath.Join(t.TempDir(), "workspaces")
		created, err := EnsureRoot(root)
		require.NoError(t, err)
		require.True(t, created)
		require.NoError(t, SeedHiveWorkspace(root))
		return root
	}

	t.Run("RewritesTheSkillsListToTheGivenSet", func(t *testing.T) {
		t.Parallel()

		root := seededRoot(t)
		changed, err := SyncHiveWorkspaceSkills(root, []string{"hive-mcp", "hive-flows", "hive-settings"})
		require.NoError(t, err)
		assert.True(t, changed)

		ws, err := LoadWorkspace(filepath.Join(root, "hive", manifestFileName))
		require.NoError(t, err)
		assert.Equal(t, []string{"hive-mcp", "hive-flows", "hive-settings"}, ws.Skills)

		changedAgain, err := SyncHiveWorkspaceSkills(root, []string{"hive-mcp", "hive-flows", "hive-settings"})
		require.NoError(t, err)
		assert.False(t, changedAgain, "an already-current manifest is not rewritten")
	})

	t.Run("PreservesUserEditsOutsideTheSkillsList", func(t *testing.T) {
		t.Parallel()

		root := seededRoot(t)
		manifest := filepath.Join(root, "hive", manifestFileName)
		require.NoError(t, os.WriteFile(manifest, []byte("version: 2\nname: Hive\n# my note\nagent: codex\nautonomy: full\nmcps:\n  - playwright\nskills:\n  - hive-mcp\n"), 0o600))

		_, err := SyncHiveWorkspaceSkills(root, []string{"hive-mcp", "hive-flows"})
		require.NoError(t, err)

		data, err := os.ReadFile(manifest)
		require.NoError(t, err)
		assert.Contains(t, string(data), "# my note")
		ws, err := LoadWorkspace(manifest)
		require.NoError(t, err)
		assert.Equal(t, "codex", ws.Agent)
		assert.Equal(t, AutonomyFull, ws.Autonomy)
		assert.Equal(t, []string{"playwright"}, ws.MCPs)
		assert.Equal(t, []string{"hive-mcp", "hive-flows"}, ws.Skills)
	})

	t.Run("LeavesADeletedWorkspaceDeleted", func(t *testing.T) {
		t.Parallel()

		root := seededRoot(t)
		require.NoError(t, os.RemoveAll(filepath.Join(root, "hive")))

		changed, err := SyncHiveWorkspaceSkills(root, []string{"hive-mcp"})
		require.NoError(t, err)
		assert.False(t, changed)
		_, statErr := os.Stat(filepath.Join(root, "hive"))
		assert.True(t, os.IsNotExist(statErr), "sync must not resurrect a deleted hive workspace")
	})
}

func TestSeedDoesNotRecreateADeletedHiveWorkspace(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "workspaces")
	created, err := EnsureRoot(root)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, SeedHiveWorkspace(root))

	require.NoError(t, os.RemoveAll(filepath.Join(root, "hive")))

	// A relaunch against the same, already-existing root: EnsureRoot reports
	// created == false, which is the signal app.go's openAgentWorkspaces uses
	// to skip SeedHiveWorkspace — deleting the workspace must leave it
	// deleted (spec §14).
	createdAgain, err := EnsureRoot(root)
	require.NoError(t, err)
	require.False(t, createdAgain)

	_, statErr := os.Stat(filepath.Join(root, "hive"))
	assert.True(t, os.IsNotExist(statErr), "hive workspace must stay deleted across a relaunch that finds an existing root")
}
