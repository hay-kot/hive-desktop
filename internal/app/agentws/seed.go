package agentws

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const defaultMCPsYAML = `version: 1

# This is your own MCP server library — the escape hatch beyond the shipped
# catalogue (playwright today). Add a server here and reference its id from a
# workspace's "mcps:" list to enable it. A user id can also replace a shipped
# one of the same name; the catalogue reports which entry won.
#
# servers:
#   home-assistant:
#     title: Home Assistant
#     type: http
#     url: http://homeassistant.local:8123/mcp
#     headers:
#       Authorization: "Bearer op://vault/item/token"
servers: {}
`

// SeedDefaultsIfMissing installs a commented mcps.yaml (version 1, an empty
// servers map) only if it does not already exist, and creates .shared/skills/
// unconditionally. It never interprets or replaces a present mcps.yaml,
// including an empty or invalid one, and follows actions.SeedDefaultsIfMissing
// (internal/app/actions/seed.go:143-189): write to a temp file, then hard-link
// it into place, so a concurrent writer that wins the race keeps its own
// bytes intact. The returned boolean reports whether this invocation
// installed mcps.yaml.
//
// .shared/prompts/ is deliberately not created — see the package doc: skills
// are unambiguous (.claude/skills, .agents/skills), but no agent this build
// targets has a portable prompts convention to merge one into.
func SeedDefaultsIfMissing(root string) (bool, error) {
	if err := os.MkdirAll(filepath.Join(root, ".shared", "skills"), 0o700); err != nil {
		return false, fmt.Errorf("create .shared/skills: %w", err)
	}

	path := filepath.Join(root, libraryFileName)
	if _, err := os.Lstat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat mcps.yaml seed target: %w", err)
	}

	if err := os.MkdirAll(root, 0o700); err != nil {
		return false, fmt.Errorf("create agent workspace root: %w", err)
	}
	f, err := os.CreateTemp(root, ".mcps-seed-*")
	if err != nil {
		return false, fmt.Errorf("create mcps.yaml seed temp: %w", err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write([]byte(defaultMCPsYAML))
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, fmt.Errorf("write mcps.yaml seed: %w", err)
	}
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("install mcps.yaml seed: %w", err)
	}
	d, err := os.Open(root)
	if err == nil {
		err = d.Sync()
		closeErr := d.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return false, fmt.Errorf("sync agent workspace root: %w", err)
	}
	return true, nil
}

const hiveWorkspaceYAML = `version: 1
name: Hive
agent: claude
autonomy: ask
skills:
  - hive-http-api
`

const hiveAgentsMD = `# Hive

This workspace drives Hive Desktop itself — settings, the feed, flows,
actions, keybindings, and webhooks — through the full set of hive skills,
which the app keeps up to date here automatically.

Autonomy is "ask": nothing here runs unprompted.
`

// SeedHiveWorkspace writes <root>/hive/: an agent-workspace.yaml with
// autonomy: ask (explicit, though it is also the default since hc-ou4o02zx
// §4), plus an AGENTS.md explaining what the workspace is for. It gives a
// user an agent surface for configuration and feed curation without
// authoring YAML first — the orchestrator case from spec §1, working on
// first launch. The manifest's skills: list is not this seed's to keep
// current: SyncHiveWorkspaceSkills rewrites it to the full shipped set on
// every startup, including the one that just seeded it.
//
// Callers must invoke this only when EnsureRoot creates root for the first
// time, never on every open: deleting the workspace must leave it deleted
// (spec §14), and reseeding on every launch would contradict that.
func SeedHiveWorkspace(root string) error {
	dir := filepath.Join(root, "hive")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create hive workspace dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFileName), []byte(hiveWorkspaceYAML), 0o600); err != nil {
		return fmt.Errorf("write hive workspace manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(hiveAgentsMD), 0o600); err != nil {
		return fmt.Errorf("write hive workspace AGENTS.md: %w", err)
	}
	return nil
}

// SyncHiveWorkspaceSkills rewrites the seeded hive workspace's skills: list
// to slugs, editing the manifest's node tree in place so comments and keys
// the sync does not own survive. It runs on every startup — the workspace
// exists to drive Hive Desktop, so it tracks the full shipped skill set as
// releases add or remove skills, including hand-removed entries. A missing
// manifest is left missing: deleting the workspace must leave it deleted
// (spec §14). The returned boolean reports whether the file was rewritten.
func SyncHiveWorkspaceSkills(root string, slugs []string) (bool, error) {
	path := filepath.Join(root, "hive", manifestFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("hive workspace manifest: %w", err)
	}
	doc, mapping, err := parseManifestNode(raw)
	if err != nil {
		return false, fmt.Errorf("hive workspace manifest: %w", err)
	}
	if err := setManifestValue(mapping, "skills", slugs); err != nil {
		return false, fmt.Errorf("hive workspace manifest: %w", err)
	}
	out, err := encodeManifestDoc(doc)
	if err != nil {
		return false, fmt.Errorf("hive workspace manifest: %w", err)
	}
	if bytes.Equal(raw, out) {
		return false, nil
	}
	if err := writeManifestAtomic(path, out); err != nil {
		return false, fmt.Errorf("hive workspace manifest: %w", err)
	}
	return true, nil
}
