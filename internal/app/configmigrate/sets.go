package configmigrate

// Baselines are 1: a legacy flows/actions file already carries `version: 1`,
// and a legacy settings.yaml with no `version:` key is treated as v1 (it is the
// v1 schema). Setting Baseline == Current means every current file is a pure
// no-op on this build — no rewrite, no backup, no watcher reload loop — until a
// real breaking change bumps Current and adds the corresponding step.
//
// These are package vars, not consts, so a test can inject a higher Current and
// a step to exercise the whole loader path at Current == Baseline (restore with
// t.Cleanup).
var (
	SettingsSet = Set{Name: "settings", Baseline: 1, Current: 1, AllowMissingVersion: true}
	FlowSet     = Set{Name: "flow", Baseline: 1, Current: 1}
	ActionsSet  = Set{Name: "actions", Baseline: 1, Current: 1}
	// MCPLibrarySet and AgentWorkspaceSet cover mcps.yaml and
	// agent-workspace.yaml (internal/app/agentws). Both are inert at
	// Baseline == Current == 1, same as every other set until a real
	// breaking change bumps Current.
	MCPLibrarySet     = Set{Name: "mcps", Baseline: 1, Current: 1}
	AgentWorkspaceSet = Set{Name: "agent-workspace", Baseline: 1, Current: 1}
)
