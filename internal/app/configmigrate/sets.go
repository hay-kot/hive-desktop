package configmigrate

import (
	"fmt"
	"strings"
)

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
	// SettingsSet covers settings.yaml. Version 2 drops the `experimental`
	// section, whose two flags graduated (ADR terminal-agents-grafana-and-commands-graduate-out-of-experimental);
	// version 3 drops `skills`, the retired global installer's configuration
	// (ADR skills-are-declared-by-a-workspace).
	SettingsSet = Set{Name: "settings", Baseline: 1, Current: 3, AllowMissingVersion: true, Migrations: []Migration{
		{To: 2, Migrate: dropExperimentalSection},
		{To: 3, Migrate: dropSkillsSection},
	}}
	FlowSet    = Set{Name: "flow", Baseline: 1, Current: 1}
	ActionsSet = Set{Name: "actions", Baseline: 1, Current: 1}
	// MCPLibrarySet covers mcps.yaml (internal/app/agentws) and is inert at
	// Baseline == Current == 1: the shipped hive-desktop entry lives in the Go
	// registry, so no user library needed rewriting for it.
	MCPLibrarySet = Set{Name: "mcps", Baseline: 1, Current: 1}
	// SkillLibrarySet covers skills.yml (internal/app/agentws), the skill
	// packages a workspace enables.
	SkillLibrarySet = Set{Name: "skills", Baseline: 1, Current: 1}
	// AgentWorkspaceSet covers agent-workspace.yaml. Version 2 renames the
	// skill slug the MCP cut-over retired (ADR mcp-replaces-the-agent-facing-http-api);
	// version 3 collapses a skills: list that is exactly the shipped set onto
	// the hive package (ADR skill-packages-are-the-unit-a-workspace-enables).
	AgentWorkspaceSet = Set{Name: "agent-workspace", Baseline: 1, Current: 4, Migrations: []Migration{
		{To: 2, Migrate: renameHTTPAPISkill},
		{To: 3, Migrate: collapseShippedSkillsToHivePackage},
		{To: 4, Migrate: autonomyToCommandTemplate},
	}}
)

// dropExperimentalSection deletes the `experimental` key.
//
// The settings decoder is strict, so a file carrying the section a user opted
// into — the only files that carry it, since it marshals with omitempty —
// would fail startup outright once the struct is gone.
func dropExperimentalSection(doc map[string]any) error {
	delete(doc, "experimental")
	return nil
}

// renameHTTPAPISkill rewrites the retired hive-http-api skill slug to hive-mcp.
//
// The prompt behind it became the MCP prompt when the agent-facing HTTP API was
// deleted (ADR mcp-replaces-the-agent-facing-http-api), and a workspace's skills: list carries the installed slug.
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

// dropSkillsSection deletes the `skills` key.
//
// It configured the global skill installer — per-agent install directories
// and an auto-update toggle -- which no longer exists (ADR skills-are-declared-by-a-workspace).
// The settings decoder is strict, so every user who ever opened Settings ▸
// Skills would fail startup outright once the struct is gone.
func dropSkillsSection(doc map[string]any) error {
	delete(doc, "skills")
	return nil
}

// v2ShippedSkillSlugs is the full set of skills this build shipped when a
// workspace's skills: list still enumerated slugs, hardcoded rather than read
// from internal/app/prompts.
//
// A migration has to be deterministic against old data and the shipped set
// moves across releases, so consulting the live registry would make the
// rewrite depend on which build happened to run it.
var v2ShippedSkillSlugs = []string{
	"hive-actions",
	"hive-agent-workspaces",
	"hive-flows",
	"hive-mcp",
	"hive-settings",
	"hive-webhook-sources",
}

// collapseShippedSkillsToHivePackage rewrites a skills: list that is exactly
// the v2 shipped set to the single seeded package that selects the same set.
//
// Skills stopped being the enablement unit (ADR skill-packages-are-the-unit-a-workspace-enables),
// so an enumerated slug now reads as a package name skills.yml does not
// define. Only the identity case is safe: `hive-*` resolves to exactly these
// six either way, whereas a partial list like [hive-mcp] would be granted five
// skills it never carried. A partial list is left alone for a human to fix,
// which is what the editor's warning points at.
func collapseShippedSkillsToHivePackage(doc map[string]any) error {
	entries, ok := doc["skills"].([]any)
	if !ok || len(entries) != len(v2ShippedSkillSlugs) {
		return nil
	}
	present := make(map[string]bool, len(entries))
	for _, entry := range entries {
		slug, ok := entry.(string)
		if !ok {
			return nil
		}
		present[slug] = true
	}
	for _, slug := range v2ShippedSkillSlugs {
		if !present[slug] {
			return nil
		}
	}
	doc["skills"] = []any{"hive"}
	return nil
}

// autonomyPostureFlags is the ask/auto/full launch table exactly as version 3
// shipped it, frozen here. A migration must not read the live preset list:
// this step has to keep producing the same command for the same old manifest
// after the presets move on, or upgrading twice from two builds would give
// two different workspaces (ADR the-workspace-command-is-a-template).
var autonomyPostureFlags = map[string]map[string]string{
	"claude": {"ask": "", "auto": "--permission-mode acceptEdits", "full": "--dangerously-skip-permissions"},
	"codex":  {"ask": "", "auto": "--ask-for-approval on-request --sandbox workspace-write", "full": "--dangerously-bypass-approvals-and-sandbox"},
}

// autonomyCommandTail is version 3's claude wiring: the generated .mcp.json
// and the pinned session id. Codex received neither at launch, so it has no
// tail.
const autonomyCommandTail = " --strict-mcp-config --mcp-config {{ .MCPConfig | shq }}" +
	" {{ if .Resume }}--resume{{ else }}--session-id{{ end }} {{ .SessionID }}"

// autonomyToCommandTemplate rewrites `autonomy: ask|auto|full` into the
// `command:` template that posture used to resolve to, then drops the key.
//
// An agent version 3 had no launch mapping for — the whole reason this schema
// changed — gets its bare name as the command. That never launched before, so
// nothing regresses; it is now an editable starting point instead of a hard
// refusal.
//
// The command word comes from the agent key, not from hive's agents: profiles,
// because a migration cannot read that config. A profile whose command differs
// from its key (agent "fable" running "claude") migrates to a command naming
// the key, which the editor shows and the user corrects in one edit.
func autonomyToCommandTemplate(doc map[string]any) error {
	defer delete(doc, "autonomy")

	// A hand-authored manifest that already carries a command keeps it: the
	// user wrote the newer field on purpose, and autonomy alongside it is the
	// stale half.
	if existing, ok := doc["command"].(string); ok && strings.TrimSpace(existing) != "" {
		return nil
	}

	agent, _ := doc["agent"].(string)
	if agent == "" {
		return fmt.Errorf("agent-workspace: cannot build a command template: no agent")
	}

	posture, _ := doc["autonomy"].(string)
	if posture == "" {
		posture = "ask"
	}

	flags, known := autonomyPostureFlags[agent][posture]
	if !known {
		doc["command"] = agent
		return nil
	}

	command := agent
	if flags != "" {
		command += " " + flags
	}
	if agent == "claude" {
		command += autonomyCommandTail
	}
	doc["command"] = command
	return nil
}
