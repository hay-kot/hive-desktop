package agentws

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findEntry(t *testing.T, entries []CatalogueEntry, id string) CatalogueEntry {
	t.Helper()
	for _, e := range entries {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no catalogue entry %q", id)
	return CatalogueEntry{}
}

func TestCatalogueUserEntryShadowsShipped(t *testing.T) {
	t.Parallel()

	lib := Library{Version: 1, Servers: map[string]MCPServer{
		"playwright": {Command: "/opt/homebrew/bin/custom-playwright"},
	}}

	entries := Catalogue(lib)
	entry := findEntry(t, entries, "playwright")

	assert.False(t, entry.Shipped, "the user entry, not the shipped one, must win")
	assert.Equal(t, "playwright", entry.Shadows)
	assert.Equal(t, "/opt/homebrew/bin/custom-playwright", entry.Server.Command)

	// Only one "playwright" row — the shipped entry must not also appear.
	count := 0
	for _, e := range entries {
		if e.ID == "playwright" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestCatalogueUnshadowedShippedEntryStillAppears(t *testing.T) {
	t.Parallel()

	entries := Catalogue(Library{Version: 1})
	entry := findEntry(t, entries, "playwright")
	assert.True(t, entry.Shipped)
	assert.Empty(t, entry.Shadows)
}

func TestCatalogueFlagsACommandNotOnPATH(t *testing.T) {
	t.Parallel()

	// os.Args[0] is the compiled test binary: guaranteed to exist and be
	// executable without depending on PATH content.
	real := os.Args[0]

	lib := Library{Version: 1, Servers: map[string]MCPServer{
		"ghost": {Command: "definitely-not-a-real-binary-xyz"},
		"real":  {Command: real},
	}}

	entries := Catalogue(lib)
	ghost := findEntry(t, entries, "ghost")
	realEntry := findEntry(t, entries, "real")

	assert.NotEmpty(t, ghost.Problem)
	assert.Empty(t, realEntry.Problem)
}

func TestCatalogueDoesNotFlagNonStdioServers(t *testing.T) {
	t.Parallel()

	lib := Library{Version: 1, Servers: map[string]MCPServer{
		"remote": {Type: "http", URL: "http://example.com/mcp"},
	}}
	entries := Catalogue(lib)
	entry := findEntry(t, entries, "remote")
	require.Empty(t, entry.Problem)
}
