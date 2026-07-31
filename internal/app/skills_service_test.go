package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/skills"
)

func newTestSkillsService(t *testing.T) *SkillsService {
	t.Helper()
	b, err := settings.LoadBootstrap()
	require.NoError(t, err)
	paths := settings.ResolvePaths(b, "")
	store := settings.NewStore(paths.SettingsPath)
	promptsSvc := newPromptsService(paths, store, newWebhookService(store, nil, nil, nil, nil))
	installer, err := skills.NewInstaller(filepath.Join(t.TempDir(), "skills.json"))
	require.NoError(t, err)
	return newSkillsService(promptsSvc, installer, store, "", zerolog.Nop())
}

func targetInfo(t *testing.T, catalog SkillsCatalog, id string) SkillTarget {
	t.Helper()
	return findTarget(t, catalog, id)
}

func TestSkillsCatalogListsPromptsAsSkills(t *testing.T) {
	isolateConfig(t)
	svc := newTestSkillsService(t)

	catalog, err := svc.Catalog(t.Context(), testCatalogInput())
	require.NoError(t, err)
	require.NotEmpty(t, catalog.Skills)
	assert.Len(t, catalog.Targets, len(skills.Targets()))
	assert.True(t, catalog.AutoUpdate, "auto-update defaults on")

	var flowsFound bool
	for _, sk := range catalog.Skills {
		if sk.ID == "flows" {
			flowsFound = true
			assert.Equal(t, "hive-flows", sk.Name)
			assert.NotEmpty(t, sk.Text)
		}
	}
	assert.True(t, flowsFound)
	// Nothing is installed on a fresh config, so every agent reads as off.
	for _, target := range catalog.Targets {
		assert.Zero(t, target.Installed, "target %q", target.ID)
	}
}

func TestSkillsInstallAndUninstallTarget(t *testing.T) {
	isolateConfig(t)
	svc := newTestSkillsService(t)
	dir := t.TempDir()

	// Point the target at a temp dir so the test never writes into the real ~.
	_, err := svc.SetTargetDir(t.Context(), testCatalogInput(), "claude", dir)
	require.NoError(t, err)

	// Turning an agent on installs every skill.
	res, err := svc.InstallTarget(t.Context(), testCatalogInput(), "claude")
	require.NoError(t, err)
	assert.Equal(t, len(res.Catalog.Skills), res.Count)
	assert.Positive(t, res.Count)
	assert.FileExists(t, filepath.Join(dir, "hive-flows", "SKILL.md"))
	assert.FileExists(t, filepath.Join(dir, "hive-actions", "SKILL.md"))
	assert.Equal(t, res.Count, targetInfo(t, res.Catalog, "claude").Installed)
	assert.False(t, targetInfo(t, res.Catalog, "claude").NeedsSync)

	// Turning it off removes them all.
	rm, err := svc.UninstallTarget(t.Context(), testCatalogInput(), "claude")
	require.NoError(t, err)
	assert.Equal(t, res.Count, rm.Count)
	assert.Zero(t, rm.Kept)
	assert.NoFileExists(t, filepath.Join(dir, "hive-flows", "SKILL.md"))
	assert.Zero(t, targetInfo(t, rm.Catalog, "claude").Installed)
}

func TestSkillsUninstallKeepsUserEditedFile(t *testing.T) {
	isolateConfig(t)
	svc := newTestSkillsService(t)
	dir := t.TempDir()

	_, err := svc.SetTargetDir(t.Context(), testCatalogInput(), "claude", dir)
	require.NoError(t, err)
	_, err = svc.InstallTarget(t.Context(), testCatalogInput(), "claude")
	require.NoError(t, err)

	// The user edits one installed file.
	edited := filepath.Join(dir, "hive-flows", "SKILL.md")
	require.NoError(t, os.WriteFile(edited, []byte("mine now\n"), 0o644))

	rm, err := svc.UninstallTarget(t.Context(), testCatalogInput(), "claude")
	require.NoError(t, err)
	assert.Equal(t, 1, rm.Kept, "the edited file is kept")
	data, err := os.ReadFile(edited)
	require.NoError(t, err)
	assert.Equal(t, "mine now\n", string(data), "an edited file survives uninstall")
	assert.Zero(t, targetInfo(t, rm.Catalog, "claude").Installed, "the index is still cleared")
}

func TestSkillsSetAutoUpdatePersists(t *testing.T) {
	isolateConfig(t)
	svc := newTestSkillsService(t)

	catalog, err := svc.SetAutoUpdate(t.Context(), testCatalogInput(), false)
	require.NoError(t, err)
	assert.False(t, catalog.AutoUpdate)

	catalog, err = svc.Catalog(t.Context(), testCatalogInput())
	require.NoError(t, err)
	assert.False(t, catalog.AutoUpdate, "toggle must survive a reload")
}

func TestSkillsSetTargetDirClearsToDefault(t *testing.T) {
	isolateConfig(t)
	svc := newTestSkillsService(t)
	dir := t.TempDir()

	catalog, err := svc.SetTargetDir(t.Context(), testCatalogInput(), "codex", dir)
	require.NoError(t, err)
	target := findTarget(t, catalog, "codex")
	assert.Equal(t, dir, target.Dir)
	assert.False(t, target.Default)

	catalog, err = svc.SetTargetDir(t.Context(), testCatalogInput(), "codex", "")
	require.NoError(t, err)
	target = findTarget(t, catalog, "codex")
	assert.True(t, target.Default)
	assert.Equal(t, "~/.codex/skills", target.Dir)
}

func TestSkillsSetTargetDirSyncsInstalledTarget(t *testing.T) {
	isolateConfig(t)
	svc := newTestSkillsService(t)
	oldDir := t.TempDir()
	newDir := t.TempDir()

	_, err := svc.SetTargetDir(t.Context(), testCatalogInput(), "claude", oldDir)
	require.NoError(t, err)
	_, err = svc.InstallTarget(t.Context(), testCatalogInput(), "claude")
	require.NoError(t, err)

	catalog, err := svc.SetTargetDir(t.Context(), testCatalogInput(), "claude", newDir)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(newDir, "hive-flows", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(oldDir, "hive-flows", "SKILL.md"), "clean old install is removed after resync")
	assert.False(t, targetInfo(t, catalog, "claude").NeedsSync)
}

func TestSkillsSyncMaintainsInstalledAgentsOnly(t *testing.T) {
	isolateConfig(t)
	svc := newTestSkillsService(t)
	dir := t.TempDir()

	_, err := svc.SetTargetDir(t.Context(), testCatalogInput(), "claude", dir)
	require.NoError(t, err)

	// A sync with nothing installed anywhere is a no-op — no surprise installs.
	empty, err := svc.Sync(t.Context(), testCatalogInput())
	require.NoError(t, err)
	assert.Zero(t, empty.Installed)
	for _, target := range empty.Catalog.Targets {
		assert.Zero(t, target.Installed, "target %q", target.ID)
	}

	// Install claude, then delete one of its files behind the app's back.
	_, err = svc.InstallTarget(t.Context(), testCatalogInput(), "claude")
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "hive-flows", "SKILL.md")))

	// Sync restores the missing file for the installed agent.
	res, err := svc.Sync(t.Context(), testCatalogInput())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Restored)
	assert.FileExists(t, filepath.Join(dir, "hive-flows", "SKILL.md"))
	assert.False(t, targetInfo(t, res.Catalog, "claude").NeedsSync)
}

func TestSkillsUnknownTargetRejected(t *testing.T) {
	isolateConfig(t)
	svc := newTestSkillsService(t)

	_, err := svc.InstallTarget(t.Context(), testCatalogInput(), "nope")
	require.Error(t, err)
	assert.Equal(t, KindNotFound, KindOf(err))
}

func TestSkillsSyncInstalledIsNoopWhenEmpty(t *testing.T) {
	isolateConfig(t)
	svc := newTestSkillsService(t)

	res, err := svc.SyncInstalled(t.Context())
	require.NoError(t, err)
	assert.Equal(t, skills.SyncResult{}, res)
}

func findTarget(t *testing.T, catalog SkillsCatalog, id string) SkillTarget {
	t.Helper()
	for _, target := range catalog.Targets {
		if target.ID == id {
			return target
		}
	}
	t.Fatalf("target %q not found", id)
	return SkillTarget{}
}
