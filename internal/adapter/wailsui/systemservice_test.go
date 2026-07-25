package wailsui

import (
	"path/filepath"
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/stretchr/testify/require"
)

func TestSystemServiceInfo(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv("HIVE_DATA_DIR", dataRoot)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	info := NewSystemService("dev", "HEAD", "now").Info()

	require.Equal(t, dataRoot, info.DataDir.Path)
	require.Equal(t, settings.ConfigDir(), info.ConfigDir.Path)
	require.Equal(t, filepath.Join(dataRoot, "desktop", "desktop.log"), info.LogFile.Path)
	require.Equal(t, filepath.Join(dataRoot, "desktop", "desktop-pipeline.db"), info.Database.Path)
	// The data/config directories are on defaults here, so nothing is overridden.
	require.False(t, info.DataDir.Overridden)
	require.False(t, info.ConfigDir.Overridden)
}

func TestSystemServiceInfoReflectsOverride(t *testing.T) {
	t.Setenv("HIVE_DATA_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	require.NoError(t, settings.SaveBootstrap(settings.Bootstrap{DataDir: "/somewhere/data"}))

	info := NewSystemService("dev", "HEAD", "now").Info()
	require.True(t, info.DataDir.Overridden)
	require.False(t, info.ConfigDir.Overridden)
}

func TestSystemServiceSetDataDirPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	target := t.TempDir()

	require.NoError(t, NewSystemService("dev", "HEAD", "now").SetDataDir(target))

	b, err := settings.LoadBootstrap()
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(target), b.DataDir)
}

func TestSystemServiceSetDataDirRejectsRelative(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	require.Error(t, NewSystemService("dev", "HEAD", "now").SetDataDir("relative/path"))
	require.Error(t, NewSystemService("dev", "HEAD", "now").SetDataDir(""))
}

func TestSystemServiceClearConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	require.NoError(t, settings.SaveBootstrap(settings.Bootstrap{DataDir: "/d", ConfigDir: "/c"}))

	require.NoError(t, NewSystemService("dev", "HEAD", "now").ClearConfigDir())

	b, err := settings.LoadBootstrap()
	require.NoError(t, err)
	require.Equal(t, "/d", b.DataDir)
	require.Empty(t, b.ConfigDir)
}

func TestSystemServiceCheckAllowed(t *testing.T) {
	t.Setenv("HIVE_DATA_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := NewSystemService("dev", "HEAD", "now")

	require.NoError(t, s.checkAllowed(settings.DataDir()))
	require.NoError(t, s.checkAllowed(settings.LogFile()))
	require.Error(t, s.checkAllowed("/etc/passwd"))
}
