package agentws

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExampleYAMLIsValid keeps the agent-workspaces prompt's worked example
// honest: the bytes an agent is shown as a model document have to load
// cleanly, and the workspace's mcps: list has to resolve against the
// catalogue the example mcps.yaml supplies (spec §7.3 — nothing enabled
// whose command a user could not have read first).
func TestExampleYAMLIsValid(t *testing.T) {
	ws, err := parseWorkspace([]byte(ExampleWorkspaceYAML()))
	require.NoError(t, err)

	lib, err := parseLibrary([]byte(ExampleMCPsYAML()))
	require.NoError(t, err)

	catalogue := Catalogue(lib)
	byID := make(map[string]bool, len(catalogue))
	for _, entry := range catalogue {
		byID[entry.ID] = true
	}
	for _, id := range ws.MCPs {
		assert.Truef(t, byID[id], "example agent-workspace.yaml names mcp %q, which the example mcps.yaml/catalogue does not resolve", id)
	}
}
