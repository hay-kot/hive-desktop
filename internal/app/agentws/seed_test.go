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
	assert.Equal(t, []string{"hive-http-api"}, ws.Skills)

	_, err = os.Stat(filepath.Join(root, "hive", "AGENTS.md"))
	require.NoError(t, err)
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
