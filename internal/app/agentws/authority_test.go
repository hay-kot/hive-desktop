package agentws

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/configwatch"
)

func TestAuthorityFiltering(t *testing.T) {
	root := t.TempDir()
	authority := NewAuthority(root)
	assert.True(t, authority.Match(configwatch.Change{Path: filepath.Join(root, skillLibraryFileName), Operation: configwatch.Write}).Dirty)
	assert.True(t, authority.Match(configwatch.Change{Path: filepath.Join(root, "workspace", manifestFileName), Operation: configwatch.Write}).Dirty)
	assert.False(t, authority.Match(configwatch.Change{Path: filepath.Join(root, "workspace", "docs", "note.md"), Operation: configwatch.Write}).Dirty)
	match := authority.Match(configwatch.Change{Path: filepath.Join(root, "workspace"), Operation: configwatch.Create})
	assert.True(t, match.Dirty)
	assert.True(t, match.RefreshWatches)
	assert.False(t, authority.Match(configwatch.Change{Path: filepath.Join(root, ".shared", manifestFileName), Operation: configwatch.Write}).Dirty)
}

func TestAuthorityTopology(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, libraryFileName), []byte("servers: {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, skillLibraryFileName), []byte("skills: {}\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, sharedDirName, "skills"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, sharedDirName, manifestFileName), []byte("version: 1\n"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "workspace"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "workspace", manifestFileName), []byte("version: 1\n"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "empty"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workspace", "docs"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "workspace", "docs", "note.md"), []byte("note"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "workspace", "canvas.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "generated.json"), []byte("{}"), 0o600))

	topology, err := NewAuthority(root).Topology(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{root, filepath.Join(root, "empty"), filepath.Join(root, "workspace")}, topology.Directories)
	assert.Equal(t, []configwatch.AuthorityFile{
		{Key: libraryFileName, Path: filepath.Join(root, libraryFileName)},
		{Key: skillLibraryFileName, Path: filepath.Join(root, skillLibraryFileName)},
		{Key: "workspace/" + manifestFileName, Path: filepath.Join(root, "workspace", manifestFileName)},
	}, topology.Files)
}

func TestAuthorityTopologyMissingRoot(t *testing.T) {
	_, err := NewAuthority(filepath.Join(t.TempDir(), "missing")).Topology(t.Context())
	require.Error(t, err)
}

func TestAuthorityTopologyRejectsAuthoredFilesOccupiedByDirectories(t *testing.T) {
	for _, name := range []string{libraryFileName, skillLibraryFileName} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(root, name), 0o700))
			_, err := NewAuthority(root).Topology(t.Context())
			require.Error(t, err)
		})
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(workspace, manifestFileName), 0o700))
	_, err := NewAuthority(root).Topology(t.Context())
	require.Error(t, err)
}
