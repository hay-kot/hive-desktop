package agentws

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeWorkspace(t *testing.T, root, dir, name string) string {
	t.Helper()
	wsDir := filepath.Join(root, dir)
	require.NoError(t, os.MkdirAll(wsDir, 0o700))
	path := filepath.Join(wsDir, manifestFileName)
	content := fmt.Sprintf("version: 2\nname: %s\nagent: claude\ncommand: claude\n", name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestStoreKeepsLastGoodPerWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("PerWorkspaceIsolation", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeWorkspace(t, root, "a", "Alpha")
		pathB := writeWorkspace(t, root, "b", "Bravo")

		s := NewStore(root)
		require.NoError(t, s.Reload())

		a, ok := s.Status("a")
		require.True(t, ok)
		assert.Equal(t, "Alpha", a.Workspace.Name)
		b, ok := s.Status("b")
		require.True(t, ok)
		assert.Equal(t, "Bravo", b.Workspace.Name)

		// Break B, edit A, then reload.
		require.NoError(t, os.WriteFile(pathB, []byte("version: 1\nfoo: bar\n"), 0o600))
		writeWorkspace(t, root, "a", "Alpha Prime")
		require.NoError(t, s.Reload())

		aAfter, ok := s.Status("a")
		require.True(t, ok)
		assert.Equal(t, "Alpha Prime", aAfter.Workspace.Name, "A must reload to its new bytes")

		bStatus, ok := s.Status("b")
		require.True(t, ok)
		assert.False(t, bStatus.Valid)
		require.Error(t, bStatus.Err)
		assert.Equal(t, "Bravo", bStatus.Workspace.Name, "B must keep its last-good content")

		assert.Len(t, s.Statuses(), 2, "the list must not blank when one workspace breaks")
	})

	t.Run("BrokenLibraryLeavesWorkspacesLoaded", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeWorkspace(t, root, "a", "Alpha")
		libPath := filepath.Join(root, libraryFileName)
		require.NoError(t, os.WriteFile(libPath, []byte("version: 1\nservers: {}\n"), 0o600))

		s := NewStore(root)
		require.NoError(t, s.Reload())
		require.True(t, s.Library().Valid)

		require.NoError(t, os.WriteFile(libPath, []byte("version: 1\nfoo: bar\n"), 0o600))
		require.NoError(t, s.Reload())

		lib := s.Library()
		assert.False(t, lib.Valid)
		require.Error(t, lib.Err)

		_, ok := s.Status("a")
		assert.True(t, ok, "workspaces must stay loaded when only the library breaks")
	})
}

func TestStoreRetainsLibrariesIndependentlyAndAcceptsDeletion(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestPath := writeWorkspace(t, root, "workspace", "Workspace")
	mcpPath := filepath.Join(root, libraryFileName)
	skillsPath := filepath.Join(root, skillLibraryFileName)
	require.NoError(t, os.WriteFile(mcpPath, []byte("version: 1\nservers:\n  local: {command: npx}\n"), 0o600))
	require.NoError(t, os.WriteFile(skillsPath, []byte("version: 1\npackages:\n  hive: {include: [\"hive-*\"]}\n"), 0o600))

	store := NewStore(root)
	require.NoError(t, store.Reload())
	require.True(t, store.Library().Valid)
	require.True(t, store.SkillLibrary().Valid)

	require.NoError(t, os.WriteFile(manifestPath, []byte("version: 1\nunknown: true\n"), 0o600))
	require.NoError(t, os.WriteFile(mcpPath, []byte("version: 1\nservers:\n  broken: {}\n"), 0o600))
	require.NoError(t, os.WriteFile(skillsPath, []byte("version: 1\npackages:\n  broken: {}\n"), 0o600))
	require.NoError(t, store.Reload())

	workspace, ok := store.Status("workspace")
	require.True(t, ok)
	assert.False(t, workspace.Valid)
	assert.Equal(t, "Workspace", workspace.Workspace.Name, "a broken manifest retains its own last-good workspace")
	assert.False(t, store.Library().Valid)
	assert.Contains(t, store.Library().Library.Servers, "local", "a broken MCP library retains its own last-good library")
	assert.False(t, store.SkillLibrary().Valid)
	assert.Contains(t, store.SkillLibrary().Library.Packages, "hive", "a broken skills library retains its own last-good library")

	require.NoError(t, os.Remove(manifestPath))
	require.NoError(t, os.Remove(mcpPath))
	require.NoError(t, os.Remove(skillsPath))
	require.NoError(t, store.Reload())
	_, ok = store.Status("workspace")
	assert.False(t, ok, "a deleted manifest is an explicit deletion")
	assert.True(t, store.Library().Valid)
	assert.Empty(t, store.Library().Library.Servers)
	assert.True(t, store.SkillLibrary().Valid)
	assert.Empty(t, store.SkillLibrary().Library.Packages)
}

func TestStoreIgnoresDirectoriesWithoutAManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "empty"), 0o700))
	writeWorkspace(t, root, "a", "Alpha")

	s := NewStore(root)
	require.NoError(t, s.Reload())

	_, ok := s.Status("empty")
	assert.False(t, ok)
	assert.Len(t, s.Statuses(), 1)
}

func TestStoreReportsMalformedEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "stray.txt"), []byte("hello"), 0o600))

	weirdDir := filepath.Join(root, "weird")
	require.NoError(t, os.MkdirAll(filepath.Join(weirdDir, manifestFileName), 0o700))

	s := NewStore(root)
	require.NoError(t, s.Reload())

	_, ok := s.Status("stray.txt")
	assert.False(t, ok, "a regular file at root must not be read as a workspace")

	weird, ok := s.Status("weird")
	require.True(t, ok, "a directory-shaped manifest must still surface as a workspace entry")
	assert.False(t, weird.Valid)
	require.Error(t, weird.Err)
}

func TestStoreRetainsLastGoodWhenAuthoredFilesBecomeDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestPath := writeWorkspace(t, root, "workspace", "Workspace")
	libraryPath := filepath.Join(root, libraryFileName)
	skillsPath := filepath.Join(root, skillLibraryFileName)
	require.NoError(t, os.WriteFile(libraryPath, []byte("version: 1\nservers:\n  local: {command: npx}\n"), 0o600))
	require.NoError(t, os.WriteFile(skillsPath, []byte("version: 1\npackages:\n  hive: {include: [\"hive-*\"]}\n"), 0o600))

	store := NewStore(root)
	require.NoError(t, store.Reload())

	require.NoError(t, os.Remove(manifestPath))
	require.NoError(t, os.Mkdir(manifestPath, 0o700))
	require.NoError(t, os.Remove(libraryPath))
	require.NoError(t, os.Mkdir(libraryPath, 0o700))
	require.NoError(t, os.Remove(skillsPath))
	require.NoError(t, os.Mkdir(skillsPath, 0o700))
	require.NoError(t, store.Reload())

	workspace, ok := store.Status("workspace")
	require.True(t, ok)
	assert.False(t, workspace.Valid)
	assert.Equal(t, "Workspace", workspace.Workspace.Name)
	assert.False(t, store.Library().Valid)
	assert.Contains(t, store.Library().Library.Servers, "local")
	assert.False(t, store.SkillLibrary().Valid)
	assert.Contains(t, store.SkillLibrary().Library.Packages, "hive")
}

func TestStoreReloadOnMissingRootRetainsLastGoodState(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "workspaces")
	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, libraryFileName), []byte("version: 1\nservers: {}\n"), 0o600))
	s := NewStore(root)
	require.NoError(t, s.Reload())

	require.NoError(t, os.Rename(root, root+".gone"))
	require.Error(t, s.Reload())
	assert.True(t, s.Library().Valid)
}
