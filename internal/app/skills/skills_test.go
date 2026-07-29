package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleSkill() Skill {
	return Skill{
		ID:          "flows",
		Name:        "hive-flows",
		Title:       "Flows",
		Description: `Author or edit a flow: the "graph" of sources and destinations.`,
		Body:        "# Flows\n\nEdit /home/u/.config/hive/desktop/flows/<id>.yaml.\n",
	}
}

func newInstaller(t *testing.T) (*Installer, string) {
	t.Helper()
	dir := t.TempDir()
	in, err := NewInstaller(filepath.Join(dir, "state", "skills.json"))
	require.NoError(t, err)
	return in, filepath.Join(dir, "target")
}

func TestRenderProducesValidFrontmatter(t *testing.T) {
	target, ok := TargetByID("claude")
	require.True(t, ok)

	rel, err := target.RelPath(sampleSkill())
	require.NoError(t, err)
	assert.Equal(t, "hive-flows/SKILL.md", rel)

	content, err := target.Render(sampleSkill())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(content, "---\nname: hive-flows\n"), "frontmatter opens with the slug:\n%s", content)
	// The description carries a colon and a quote; both must be escaped so the
	// YAML frontmatter still parses.
	assert.Contains(t, content, `description: "Author or edit a flow: the \"graph\" of sources and destinations."`)
	assert.Contains(t, content, "# Flows")
	assert.True(t, strings.HasSuffix(content, "\n"))
}

func TestEveryTargetRenders(t *testing.T) {
	for _, target := range Targets() {
		content, err := target.Render(sampleSkill())
		require.NoErrorf(t, err, "target %q", target.ID)
		assert.Containsf(t, content, "name: hive-flows", "target %q", target.ID)
		assert.NotEmptyf(t, target.DefaultDir, "target %q has no default dir", target.ID)
	}
}

func TestInstallWritesFileAndIndexes(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")

	got, err := in.Install(sampleSkill(), target, dir)
	require.NoError(t, err)
	assert.Equal(t, StateUpToDate, got.State)
	assert.Equal(t, filepath.Join(dir, "hive-flows", "SKILL.md"), got.Path)

	data, err := os.ReadFile(got.Path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "name: hive-flows")

	// The index round-trips: a fresh installer over the same file sees it.
	reopened, err := NewInstaller(in.index.path)
	require.NoError(t, err)
	status, err := reopened.Status(sampleSkill(), target, dir)
	require.NoError(t, err)
	assert.Equal(t, StateUpToDate, status.State)
}

func TestStatusNotInstalledNamesProspectivePath(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("codex")

	status, err := in.Status(sampleSkill(), target, dir)
	require.NoError(t, err)
	assert.Equal(t, StateNotInstalled, status.State)
	assert.Equal(t, filepath.Join(dir, "hive-flows", "SKILL.md"), status.Path)
}

func TestSyncRewritesAppChangedSkill(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")
	require.NoError(t, mustInstall(t, in, sampleSkill(), target, dir))

	// The app now renders a different body for the same id (a new node type, say).
	changed := sampleSkill()
	changed.Body = "# Flows\n\nBrand new guidance.\n"

	res, err := in.Sync(map[string]Skill{changed.ID: changed}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Updated)
	assert.Equal(t, 0, res.Modified)

	data, err := os.ReadFile(filepath.Join(dir, "hive-flows", "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "Brand new guidance.")

	// A second sync with the same content is a no-op.
	res, err = in.Sync(map[string]Skill{changed.ID: changed}, nil)
	require.NoError(t, err)
	assert.Equal(t, SyncResult{}, res)
}

func TestSyncNeverClobbersUserEdits(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")
	require.NoError(t, mustInstall(t, in, sampleSkill(), target, dir))

	path := filepath.Join(dir, "hive-flows", "SKILL.md")
	require.NoError(t, os.WriteFile(path, []byte("hand edited by the user\n"), 0o644))

	res, err := in.Sync(map[string]Skill{sampleSkill().ID: sampleSkill()}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Modified)
	assert.Equal(t, 0, res.Updated)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hand edited by the user\n", string(data), "user edit must survive sync")

	status, err := in.Status(sampleSkill(), target, dir)
	require.NoError(t, err)
	assert.Equal(t, StateModified, status.State)
}

func TestSyncRestoresMissingFile(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")
	require.NoError(t, mustInstall(t, in, sampleSkill(), target, dir))

	path := filepath.Join(dir, "hive-flows", "SKILL.md")
	require.NoError(t, os.Remove(path))

	res, err := in.Sync(map[string]Skill{sampleSkill().ID: sampleSkill()}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Restored)
	assert.FileExists(t, path)
}

func TestStatusTargetDirChangeNeedsSyncAtNewPath(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")
	require.NoError(t, mustInstall(t, in, sampleSkill(), target, dir))

	newDir := filepath.Join(t.TempDir(), "new-target")
	status, err := in.Status(sampleSkill(), target, newDir)
	require.NoError(t, err)
	assert.Equal(t, StateMissing, status.State)
	assert.Equal(t, filepath.Join(newDir, "hive-flows", "SKILL.md"), status.Path)
}

func TestSyncTargetDirChangeInstallsNewPathAndPreservesEditedOldFile(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")
	require.NoError(t, mustInstall(t, in, sampleSkill(), target, dir))

	oldPath := filepath.Join(dir, "hive-flows", "SKILL.md")
	require.NoError(t, os.WriteFile(oldPath, []byte("mine now\n"), 0o644))
	newDir := filepath.Join(t.TempDir(), "new-target")

	res, err := in.SyncInDirs(map[string]Skill{sampleSkill().ID: sampleSkill()}, func(Target) string { return newDir }, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Restored)
	assert.FileExists(t, filepath.Join(newDir, "hive-flows", "SKILL.md"))
	assert.FileExists(t, oldPath, "user-edited old file is preserved")

	status, err := in.Status(sampleSkill(), target, newDir)
	require.NoError(t, err)
	assert.Equal(t, StateUpToDate, status.State)
}

func TestSyncSkipsUnknownSkill(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")
	require.NoError(t, mustInstall(t, in, sampleSkill(), target, dir))

	// The catalog no longer contains this skill id.
	res, err := in.Sync(map[string]Skill{}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Skipped)
}

func TestSyncSkipsDisabledTarget(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")
	require.NoError(t, mustInstall(t, in, sampleSkill(), target, dir))

	changed := sampleSkill()
	changed.Body = "# Flows\n\nDifferent.\n"

	// claude is disabled, so its install goes dormant rather than being rewritten.
	res, err := in.Sync(map[string]Skill{changed.ID: changed}, func(id string) bool { return id != "claude" })
	require.NoError(t, err)
	assert.Equal(t, 0, res.Updated)
	assert.Equal(t, 1, res.Skipped)

	data, err := os.ReadFile(filepath.Join(dir, "hive-flows", "SKILL.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "Different.")
}

func TestRemoveDeletesCleanFileAndDir(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")
	require.NoError(t, mustInstall(t, in, sampleSkill(), target, dir))

	require.NoError(t, in.Remove(sampleSkill(), target))
	assert.NoFileExists(t, filepath.Join(dir, "hive-flows", "SKILL.md"))
	assert.NoDirExists(t, filepath.Join(dir, "hive-flows"))

	status, err := in.Status(sampleSkill(), target, dir)
	require.NoError(t, err)
	assert.Equal(t, StateNotInstalled, status.State)
}

func TestRemoveKeepsUserEditedFile(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")
	require.NoError(t, mustInstall(t, in, sampleSkill(), target, dir))

	path := filepath.Join(dir, "hive-flows", "SKILL.md")
	require.NoError(t, os.WriteFile(path, []byte("mine now\n"), 0o644))

	require.NoError(t, in.Remove(sampleSkill(), target))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "mine now\n", string(data), "a user-edited file must not be deleted by uninstall")
}

func TestRemoveReturnsReadErrorAndKeepsIndex(t *testing.T) {
	in, dir := newInstaller(t)
	target, _ := TargetByID("claude")
	require.NoError(t, mustInstall(t, in, sampleSkill(), target, dir))

	key := entryKey{sampleSkill().ID, target.ID}
	entry := in.index.entries[key]
	entry.Path = t.TempDir()
	in.index.entries[key] = entry

	err := in.Remove(sampleSkill(), target)
	require.Error(t, err)
	_, ok := in.index.entries[key]
	assert.True(t, ok, "failed removal must stay indexed")
}

func TestValidateSkill(t *testing.T) {
	valid := sampleSkill()
	require.NoError(t, ValidateSkill(valid))

	cases := map[string]func(Skill) Skill{
		"empty name":      func(s Skill) Skill { s.Name = ""; return s },
		"uppercase name":  func(s Skill) Skill { s.Name = "Hive-Flows"; return s },
		"reserved word":   func(s Skill) Skill { s.Name = "claude-helper"; return s },
		"trailing hyphen": func(s Skill) Skill { s.Name = "hive-flows-"; return s },
		"no description":  func(s Skill) Skill { s.Description = "  "; return s },
		"empty body":      func(s Skill) Skill { s.Body = ""; return s },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, ValidateSkill(mutate(sampleSkill())))
		})
	}
}

func mustInstall(t *testing.T, in *Installer, s Skill, target Target, dir string) error {
	t.Helper()
	_, err := in.Install(s, target, dir)
	return err
}
