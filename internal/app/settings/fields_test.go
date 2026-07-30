package settings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffReportsChangedFieldsByDottedPath(t *testing.T) {
	before := DefaultSettings()
	after := DefaultSettings()
	after.Polling.Interval = Duration(9 * time.Minute)
	after.Experimental.Terminal = true
	after.Development.Debug.PauseIngest = Duration(time.Second)

	assert.Equal(t, []FieldChange{
		{Field: "polling.interval", From: "5m0s", To: "9m0s"},
		{Field: "experimental.terminal", From: "false", To: "true"},
		{Field: "development.debug.pause_ingest", From: "0s", To: "1s"},
	}, Diff(before, after))
}

func TestDiffIsEmptyForEqualSnapshots(t *testing.T) {
	assert.Empty(t, Diff(DefaultSettings(), DefaultSettings()))
}

// An omitted keybindings section and a hand-written `keybindings: {}` are the
// same configuration; reporting a change between them would announce an edit
// nobody made and wake every consumer for nothing.
func TestDiffTreatsAnAbsentMapAsEmpty(t *testing.T) {
	before := DefaultSettings()
	after := DefaultSettings()
	after.Keybindings = map[string][]string{}
	require.Nil(t, before.Keybindings)

	assert.Empty(t, Diff(before, after))

	after.Keybindings = map[string][]string{"feed.next": {"j"}}
	assert.Equal(t, []FieldChange{{Field: "keybindings", From: "", To: "map[feed.next:[j]]"}}, Diff(before, after))
}

// FieldNames is what lets a consumer prove it has classified the whole schema,
// so it has to name every leaf — including a nested development section.
func TestFieldNamesCoversTheSchema(t *testing.T) {
	names := FieldNames()

	assert.Subset(t, names, []string{
		"version",
		"polling.interval",
		"updates.enabled",
		"appearance.terminal_font_size",
		"http.port",
		"keybindings",
		"skills.targets",
		"paths.tmux",
		"experimental.terminal",
		"development.debug.pause_commit",
	})
	assert.NotContains(t, names, "overrides", "environment provenance is process-local, not a field")

	seen := map[string]bool{}
	for _, name := range names {
		require.False(t, seen[name], "duplicate field name %q", name)
		seen[name] = true
	}
}
