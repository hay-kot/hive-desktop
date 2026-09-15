package flow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/configwatch"
)

func TestAuthorityTopologyAndFiltering(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"one.yaml", "two.yml", "one.ui.yml", "one.sidebar.yaml", "one.tmp.yaml"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("version: 1\n"), 0o600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "nested.yaml"), []byte("version: 1\n"), 0o600))
	authority := NewAuthority(dir)
	topology, err := authority.Topology(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{dir}, topology.Directories)
	assert.Equal(t, []configwatch.AuthorityFile{
		{Key: "one.ui.yml", Path: filepath.Join(dir, "one.ui.yml")},
		{Key: "one.yaml", Path: filepath.Join(dir, "one.yaml")},
		{Key: "two.yml", Path: filepath.Join(dir, "two.yml")},
	}, topology.Files)
	assert.True(t, authority.Match(configwatch.Change{Path: filepath.Join(dir, "one.yaml"), Operation: configwatch.Write}).Dirty)
	assert.True(t, authority.Match(configwatch.Change{Path: filepath.Join(dir, "one.ui.yml"), Operation: configwatch.Write}).Dirty)
	assert.False(t, authority.Match(configwatch.Change{Path: filepath.Join(dir, "one.sidebar.yaml"), Operation: configwatch.Write}).Dirty)
	assert.False(t, authority.Match(configwatch.Change{Path: filepath.Join(dir, "one.tmp.yaml"), Operation: configwatch.Write}).Dirty)
	assert.False(t, authority.Match(configwatch.Change{Path: filepath.Join(dir, "nested", "one.yaml"), Operation: configwatch.Write}).Dirty)
}

func TestAuthorityTopologyMissingRoot(t *testing.T) {
	_, err := NewAuthority(filepath.Join(t.TempDir(), "missing")).Topology(t.Context())
	require.Error(t, err)
}
