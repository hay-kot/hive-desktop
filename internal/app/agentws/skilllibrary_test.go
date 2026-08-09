package agentws

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSkillLibraryReadsSlugsAndDescriptions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lib := SkillsLibraryDir(root)
	writeFile(t, filepath.Join(lib, "release-notes", skillFileName),
		"---\nname: release-notes\ndescription: \"Draft release notes: from the merged PRs.\"\n---\n# Release notes\n")
	writeFile(t, filepath.Join(lib, "bare", skillFileName), "# Bare\n\nNo frontmatter here.\n")

	skills, err := LoadSkillLibrary(lib)
	require.NoError(t, err)
	require.Len(t, skills, 2)

	assert.Equal(t, "bare", skills[0].Slug, "entries are sorted by slug")
	assert.Empty(t, skills[0].Description, "a file with no frontmatter still loads, without a description")
	assert.Equal(t, "# Bare\n\nNo frontmatter here.\n", skills[0].Body)

	assert.Equal(t, "release-notes", skills[1].Slug)
	assert.Equal(t, "Draft release notes: from the merged PRs.", skills[1].Description)
	assert.Contains(t, skills[1].Body, "---\nname: release-notes", "the body installs verbatim, frontmatter included")
}

// TestLoadSkillLibraryTolerates covers what a directory a user drops files
// into actually contains: entries that are not installable skills are skipped
// rather than failing the read the whole catalogue depends on.
func TestLoadSkillLibraryTolerates(t *testing.T) {
	t.Parallel()

	t.Run("MissingDirectoryIsAnEmptyLibrary", func(t *testing.T) {
		t.Parallel()
		skills, err := LoadSkillLibrary(SkillsLibraryDir(t.TempDir()))
		require.NoError(t, err)
		assert.Empty(t, skills)
	})

	t.Run("SkipsEntriesThatCannotBeInstalled", func(t *testing.T) {
		t.Parallel()
		lib := SkillsLibraryDir(t.TempDir())
		require.NoError(t, os.MkdirAll(filepath.Join(lib, "no-skill-file"), 0o700))
		writeFile(t, filepath.Join(lib, "loose.md"), "# not a skill directory\n")
		writeFile(t, filepath.Join(lib, "good", skillFileName), "# Good\n")

		skills, err := LoadSkillLibrary(lib)
		require.NoError(t, err)
		require.Len(t, skills, 1)
		assert.Equal(t, "good", skills[0].Slug)
	})
}

// TestSkillCatalogueMerges is the shipped/library merge, including the shadow
// rule mcps.yaml follows against the shipped MCP registry.
func TestSkillCatalogueMerges(t *testing.T) {
	t.Parallel()

	shipped := []ShippedSkill{
		{Slug: "hive-mcp", Title: "Hive MCP", Description: "Drive this install."},
		{Slug: "hive-flows", Title: "Flows", Description: "Author flows."},
	}
	library := []LibrarySkill{
		{Slug: "hive-mcp", Description: "My own take.", Body: "# Mine\n"},
		{Slug: "release-notes", Description: "Draft release notes.", Body: "# Release notes\n"},
	}

	entries := SkillCatalogue(shipped, library)
	require.Len(t, entries, 3)
	assert.Equal(t, []string{"hive-flows", "hive-mcp", "release-notes"},
		[]string{entries[0].Slug, entries[1].Slug, entries[2].Slug}, "sorted by slug")

	assert.True(t, entries[0].Shipped)
	assert.Equal(t, "Flows", entries[0].Title)
	assert.Empty(t, entries[0].Shadows)

	shadowed := entries[1]
	assert.False(t, shadowed.Shipped, "a library slug replaces the shipped entry of the same name")
	assert.Equal(t, "hive-mcp", shadowed.Shadows)
	assert.Equal(t, "My own take.", shadowed.Description)

	assert.False(t, entries[2].Shipped)
	assert.Empty(t, entries[2].Shadows)
	assert.Equal(t, "release-notes", entries[2].Title, "a library entry is titled by its slug")
}
