package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/configwatch"
)

func TestAuthorityTopologyAndMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 1\n"), 0o600))

	authority := NewAuthority(path)
	topology, err := authority.Topology(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{dir}, topology.Directories)
	assert.Equal(t, []configwatch.AuthorityFile{{Key: "settings.yaml", Path: path}}, topology.Files)
	assert.True(t, authority.Match(configwatch.Change{Path: path, Operation: configwatch.Write}).Dirty)
	assert.True(t, authority.Match(configwatch.Change{Path: dir, Operation: configwatch.Remove}).RefreshWatches)
}

func TestAuthorityTopologyMissingRoot(t *testing.T) {
	_, err := NewAuthority(filepath.Join(t.TempDir(), "missing", "settings.yaml")).Topology(t.Context())
	require.Error(t, err)
}
