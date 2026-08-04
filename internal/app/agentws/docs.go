package agentws

import (
	"embed"
	"fmt"
)

// examplesFS holds the canonical worked example: an agent-workspace.yaml
// naming both a shipped catalogue entry and an mcps.yaml entry, plus the
// mcps.yaml it references. internal/app/prompts embeds both in the
// hive-agent-workspaces authoring prompt, following the actions.ExampleYAML()
// contract (internal/app/actions/docs.go) — TestExampleYAMLIsValid parses and
// validates these bytes, so a prompt example that no longer loads fails the
// build rather than misleading an agent.
//
//go:embed examples/agent-workspace.yaml examples/mcps.yaml
var examplesFS embed.FS

// ExampleWorkspaceYAML is the canonical agent-workspace.yaml document.
func ExampleWorkspaceYAML() string {
	return mustReadExample("agent-workspace.yaml")
}

// ExampleMCPsYAML is the canonical mcps.yaml document the example workspace
// references.
func ExampleMCPsYAML() string {
	return mustReadExample("mcps.yaml")
}

func mustReadExample(name string) string {
	data, err := examplesFS.ReadFile("examples/" + name)
	if err != nil {
		// Unreachable: the file is embedded at compile time.
		panic(fmt.Sprintf("agentws: reading embedded example %q: %v", name, err))
	}
	return string(data)
}
