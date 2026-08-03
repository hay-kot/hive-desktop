package mcpcatalog

import (
	"maps"
	"sort"
)

// registry is the shipped set, keyed by Descriptor.Type. Never init()
// self-registration — gochecknoinits is enabled, and an explicit map is the
// only form where the shipped set can be read off one file.
//
// It ships exactly one entry. A shipped entry is an endorsement (spec §7.3)
// and a release to revise, so the set starts at one; mcps.yaml is the escape
// hatch that makes that tolerable until a second entry is worth pinning here.
var registry = map[string]Descriptor{
	"playwright": {
		Type:        "playwright",
		Title:       "Playwright",
		Description: "Browser automation: navigate, click, fill forms, and read page content through a real, controllable browser.",
		Icon:        "globe",
		Stability:   StabilityStable,
		Server: Server{
			Transport: TransportStdio,
			Command:   "npx",
			// -y suppresses npx's interactive install confirmation, which
			// would otherwise stall a non-interactive agent launch the first
			// time this package version is fetched. @latest is the documented
			// invocation (microsoft/playwright-mcp); Hive does not vendor a
			// pinned version.
			Args: []string{"-y", "@playwright/mcp@latest"},
		},
	},
}

// Types returns every registered type in sorted order. Sorting keeps
// generated output byte-stable across runs — Go map iteration is randomized.
func Types() []string {
	types := make([]string, 0, len(registry))
	for mcpType := range registry {
		types = append(types, mcpType)
	}
	sort.Strings(types)
	return types
}

// Lookup returns the descriptor registered for an MCP type.
func Lookup(mcpType string) (Descriptor, bool) {
	d, ok := registry[mcpType]
	return d, ok
}

// All returns every descriptor, keyed by type. The returned map is a copy, so
// a caller ranging over it cannot mutate the registry.
func All() map[string]Descriptor {
	out := make(map[string]Descriptor, len(registry))
	maps.Copy(out, registry)
	return out
}
