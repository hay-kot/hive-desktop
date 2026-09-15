package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/configwatch"
)

func TestAuthorityFiltering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.yml")
	authority := NewAuthority(path)
	assert.True(t, authority.Match(configwatch.Change{Path: path, Operation: configwatch.Write}).Dirty)
	assert.False(t, authority.Match(configwatch.Change{Path: filepath.Join(filepath.Dir(path), "other.yml"), Operation: configwatch.Write}).Dirty)
	match := authority.Match(configwatch.Change{Path: filepath.Dir(path), Operation: configwatch.Remove})
	assert.True(t, match.Dirty)
	assert.True(t, match.RefreshWatches)
}

func TestAuthorityTopology(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "missing")
	path := filepath.Join(parent, "actions.yml")
	authority := NewAuthority(path)
	_, err := authority.Topology(t.Context())
	require.Error(t, err)

	require.NoError(t, os.Mkdir(parent, 0o700))
	topology, err := authority.Topology(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{parent}, topology.Directories)
	assert.Equal(t, []configwatch.AuthorityFile{{Key: "actions.yml", Path: path}}, topology.Files)
}
