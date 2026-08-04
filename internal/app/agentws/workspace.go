package agentws

import (
	"fmt"
	"slices"
	"strings"
)

// Autonomy is how much authority a workspace grants its agent.
//
// ENUM(ask, auto, full)
type Autonomy string

// Workspace is one agent-workspace.yaml. Dir is the directory name under the
// root — the identity a session record stores, so a record survives a machine
// whose root is somewhere else.
//
// It is read-only. Nothing in M1 or M2 writes a workspace manifest; see
// Migration Notes for what a future writer must use instead of yaml.Marshal.
type Workspace struct {
	Dir string `yaml:"-"`

	Version  int      `yaml:"version"`
	Name     string   `yaml:"name"`
	Agent    string   `yaml:"agent"`
	Autonomy Autonomy `yaml:"autonomy"`
	MCPs     []string `yaml:"mcps,omitempty"`
	Skills   []string `yaml:"skills,omitempty"`
}

// Validate checks the fields Workspace owns directly. autonomy is no longer
// required: the M2 approval indicator (hc-ou4o02zx) makes "ask" legible, so an
// omitted autonomy defaults to it at load (parseWorkspace) rather than
// failing here — Validate only rejects a non-empty value that names no known
// posture. It does not check Agent against the set of agents this build can
// actually launch: that mapping lives at the dispatch/launch-table seam
// (spec §14's unknown-agent case), not here.
func (w Workspace) Validate() error {
	if w.Name == "" {
		return fmt.Errorf("agent-workspace.yaml: name is required")
	}
	if w.Agent == "" {
		return fmt.Errorf("agent-workspace.yaml: agent is required")
	}
	if w.Autonomy != "" && !w.Autonomy.IsValid() {
		return fmt.Errorf("agent-workspace.yaml: autonomy %q is not valid (expected %s)", w.Autonomy, strings.Join(AutonomyNames(), ", "))
	}
	if dup := firstDuplicate(w.MCPs); dup != "" {
		return fmt.Errorf("agent-workspace.yaml: duplicate mcp %q", dup)
	}
	if dup := firstDuplicate(w.Skills); dup != "" {
		return fmt.Errorf("agent-workspace.yaml: duplicate skill %q", dup)
	}
	if slices.Contains(w.MCPs, "") {
		return fmt.Errorf("agent-workspace.yaml: mcps entries must not be empty")
	}
	if slices.Contains(w.Skills, "") {
		return fmt.Errorf("agent-workspace.yaml: skills entries must not be empty")
	}
	return nil
}

// firstDuplicate returns the first value that repeats in ids, or "" when
// every value is unique.
func firstDuplicate(ids []string) string {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return id
		}
		seen[id] = true
	}
	return ""
}
