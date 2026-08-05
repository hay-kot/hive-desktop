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
	// MCPLibrarySet covers mcps.yaml (internal/app/agentws) and is inert at
	// Baseline == Current == 1: the shipped hive-desktop entry lives in the Go
	// registry, so no user library needed rewriting for it.
	MCPLibrarySet = Set{Name: "mcps", Baseline: 1, Current: 1}
	// AgentWorkspaceSet covers agent-workspace.yaml. Version 2 renames the
	// skill slug the MCP cut-over retired (ADR 0073).
	AgentWorkspaceSet = Set{Name: "agent-workspace", Baseline: 1, Current: 2, Migrations: []Migration{
		{To: 2, Migrate: renameHTTPAPISkill},
	}}
)

// renameHTTPAPISkill rewrites the retired hive-http-api skill slug to hive-mcp.
//
// The prompt behind it became the MCP prompt when the agent-facing HTTP API was
// deleted (ADR 0073), and a workspace's skills: list carries the installed slug.
// Without this, opening a workspace that declared the old one fails outright —
// resolveSkills refuses a slug no prompt id backs, so one stale entry takes the
// whole workspace down rather than degrading.
func renameHTTPAPISkill(doc map[string]any) error {
	const (
		old = "hive-http-api"
		new = "hive-mcp"
	)
	skills, ok := doc["skills"].([]any)
	if !ok {
		return nil
	}
	seen := make(map[string]bool, len(skills))
	out := make([]any, 0, len(skills))
	for _, entry := range skills {
		slug, ok := entry.(string)
		if !ok {
			out = append(out, entry)
			continue
		}
		if slug == old {
			slug = new
		}
		// A manifest already naming both would otherwise end up declaring
		// hive-mcp twice, which the workspace validator rejects as a duplicate.
		if seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, slug)
	}
	doc["skills"] = out
	return nil
}
