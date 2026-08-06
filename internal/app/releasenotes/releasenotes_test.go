package releasenotes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChangelogParses is the gate that keeps a malformed entry from shipping:
// every committed file must have a well-formed header whose version matches
// its filename.
func TestChangelogParses(t *testing.T) {
	entries, err := Load()
	require.NoError(t, err)
	require.NotEmpty(t, entries, "the embedded changelog must not be empty")

	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		assert.False(t, seen[entry.Version], "duplicate entry for %s", entry.Version)
		seen[entry.Version] = true
		assert.False(t, entry.Date.IsZero(), "%s has no date", entry.Version)
		assert.NotEmpty(t, entry.Body, "%s has an empty body", entry.Version)
	}
}

func TestChangelogIsOrderedNewestFirst(t *testing.T) {
	entries, err := Load()
	require.NoError(t, err)

	for i := 1; i < len(entries); i++ {
		assert.Positive(t, compareEntries(entries[i-1], entries[i]),
			"%s should sort above %s", entries[i-1].Version, entries[i].Version)
	}
}

func entries(versions ...string) Entries {
	out := make(Entries, 0, len(versions))
	for _, v := range versions {
		channel, _ := Channel(v)
		out = append(out, Entry{Version: v, Channel: channel, Body: "notes for " + v})
	}
	return out
}

func versionsOf(e Entries) []string {
	out := make([]string, 0, len(e))
	for _, entry := range e {
		out = append(out, entry.Version)
	}
	return out
}

// A release reaches the channels the publish cascade sends it to and no
// others: a stable user must never be shown the dev builds that preceded
// their upgrade.
func TestVisibleInFollowsThePublishCascade(t *testing.T) {
	all := entries("1.2.0", "1.2.0-beta.1", "1.2.0-dev.3", "1.2.0-dev.2")

	assert.Equal(t, []string{"1.2.0"}, versionsOf(all.VisibleIn("stable")))
	assert.Equal(t, []string{"1.2.0", "1.2.0-beta.1"}, versionsOf(all.VisibleIn("beta")))
	assert.Equal(t,
		[]string{"1.2.0", "1.2.0-beta.1", "1.2.0-dev.3", "1.2.0-dev.2"},
		versionsOf(all.VisibleIn("dev")))
}

func TestBetweenReturnsEveryReleaseCrossed(t *testing.T) {
	all := entries("1.2.0-dev.4", "1.2.0-dev.3", "1.2.0-dev.2", "1.2.0-dev.1")

	assert.Equal(t, []string{"1.2.0-dev.4", "1.2.0-dev.3"},
		versionsOf(all.Between("1.2.0-dev.2", "1.2.0-dev.4", "dev")),
		"a user who skipped a build sees both releases they crossed")

	assert.Empty(t, all.Between("1.2.0-dev.4", "1.2.0-dev.4", "dev"),
		"the version already acknowledged is not shown again")
	assert.Empty(t, all.Between("1.2.0-dev.4", "1.2.0-dev.2", "dev"),
		"a downgrade shows nothing")
}

// A stable user upgrading past a run of prereleases sees only the stable
// release, not the dev builds that were published between the two.
func TestBetweenExcludesPrereleasesForStable(t *testing.T) {
	all := entries("1.3.0", "1.3.0-beta.1", "1.2.1-dev.2", "1.2.1-dev.1", "1.2.0")

	assert.Equal(t, []string{"1.3.0"}, versionsOf(all.Between("1.2.0", "1.3.0", "stable")))
	assert.Equal(t, []string{"1.3.0", "1.3.0-beta.1"}, versionsOf(all.Between("1.2.0", "1.3.0", "beta")))
}

func TestBetweenIgnoresUnpublishableVersions(t *testing.T) {
	all := entries("1.2.0-dev.2", "1.2.0-dev.1")

	assert.Empty(t, all.Between("1.2.0-dev.1", "dev", "dev"), "a source build has no notes")
	assert.Equal(t, []string{"1.2.0-dev.2", "1.2.0-dev.1"},
		versionsOf(all.Between("", "1.2.0-dev.2", "dev")),
		"no recorded version means no lower bound")
}

// Promotion runs dev -> beta -> stable within a base version, which is the
// reverse of how semver orders the words "beta" and "dev".
func TestOrderingFollowsThePromotionPath(t *testing.T) {
	assert.True(t, IsNewer("1.2.0-beta.1", "1.2.0-dev.9"))
	assert.True(t, IsNewer("1.2.0", "1.2.0-beta.9"))
	assert.True(t, IsNewer("1.2.1-dev.1", "1.2.0"))
	assert.False(t, IsNewer("1.2.0-dev.9", "1.2.0-beta.1"))
	assert.False(t, IsNewer("dev", "1.2.0"), "a source build never counts as an upgrade")
	assert.True(t, IsNewer("1.2.0", "(devel)"), "nothing recorded means anything published is newer")
}

func TestChannelRejectsUnpublishedVersions(t *testing.T) {
	for _, version := range []string{"dev", "(devel)", "v0.0.0-20260101000000-abcdef123456", "1.2.0-rc.1", ""} {
		_, ok := Channel(version)
		assert.False(t, ok, "%q must not resolve to a channel", version)
	}

	for version, want := range map[string]string{
		"1.2.0":          "stable",
		"v1.2.0":         "stable",
		"desktop-v1.2.0": "stable",
		"1.2.0-beta.2":   "beta",
		"1.2.0-dev.7":    "dev",
	} {
		channel, ok := Channel(version)
		require.True(t, ok, "%q should resolve", version)
		assert.Equal(t, want, channel, "%q", version)
	}
}

func TestFind(t *testing.T) {
	all := entries("1.2.0-dev.2", "1.2.0-dev.1")

	entry, ok := all.Find("v1.2.0-dev.2")
	require.True(t, ok, "the v prefix is accepted")
	assert.Equal(t, "1.2.0-dev.2", entry.Version)

	_, ok = all.Find("9.9.9")
	assert.False(t, ok)
}

func TestParseEntryRejectsMismatchedFilename(t *testing.T) {
	_, err := parseEntry("1.2.0.md", []byte("---\nversion: 1.2.1\ndate: 2026-01-01\n---\n\nnotes\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match its filename")
}

func TestParseEntryRequiresAValidHeader(t *testing.T) {
	for name, raw := range map[string]string{
		"no frontmatter": "just a body\n",
		"unterminated":   "---\nversion: 1.2.0\n",
		"bad date":       "---\nversion: 1.2.0\ndate: 06/01/2026\n---\n\nnotes\n",
		"unpublishable":  "---\nversion: 1.2.0-rc.1\ndate: 2026-01-01\n---\n\nnotes\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseEntry("1.2.0.md", []byte(raw))
			assert.Error(t, err)
		})
	}
}

func TestParseEntryReadsSummaryAndBody(t *testing.T) {
	entry, err := parseEntry("1.2.0.md", []byte(
		"---\nversion: 1.2.0\ndate: 2026-01-02\nsummary: A short line.\n---\n\n## Added\n\n- a thing\n"))
	require.NoError(t, err)

	assert.Equal(t, "1.2.0", entry.Version)
	assert.Equal(t, "stable", entry.Channel)
	assert.Equal(t, "A short line.", entry.Summary)
	assert.Equal(t, "## Added\n\n- a thing", entry.Body)
	assert.Equal(t, 2026, entry.Date.Year())
}
