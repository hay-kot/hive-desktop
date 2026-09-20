// Package hiveconf reads and writes the external Hive CLI configuration the
// desktop shares with the `hive` binary.
//
// The desktop does not require this file — hive's own defaults apply when it
// is absent. It requires it to be *useful*: with no file, hive defaults to a
// single "claude" profile whether or not claude is installed, and to no
// workspaces at all, which leaves the session launcher with an empty
// repository list. Setup exists to answer those two questions.
//
// Nothing here exposes a vendored hive type. The package reads the file's own
// YAML to report what the user declared, rather than the merged config, so
// "hive would fall back to claude" is never mistaken for "the user chose
// claude" (Bounded Context, architecture.md).
package hiveconf

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// KnownAgents is the catalog setup offers, in the order it offers them. It
// mirrors `hive init`'s own list so the two tools agree on what an agent is
// called; the label is this app's, because a picker needs a name a person
// recognises.
//
// An agent missing from this list is not unsupported — it is only absent from
// the picker. Any profile already in the file round-trips untouched.
var KnownAgents = []AgentKind{
	{Name: "claude", Label: "Claude Code", SkipPermissionFlags: []string{"--dangerously-skip-permissions"}},
	{Name: "opencode", Label: "OpenCode", SkipPermissionFlags: []string{"--agent", "free-permissions-runner"}},
	{Name: "codex", Label: "Codex", SkipPermissionFlags: []string{"--full-auto"}},
	{Name: "copilot", Label: "GitHub Copilot"},
	{Name: "cursor", Label: "Cursor"},
	{Name: "amp", Label: "Amp"},
	{Name: "pi", Label: "Pi"},
}

// AgentKind is one entry in the catalog: what the profile is called in the
// config file, what to show a person, and the flags that turn off the agent's
// own permission prompts.
type AgentKind struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// SkipPermissionFlags is empty for an agent with no known flag. Offering
	// the toggle for one of those would write a profile that does nothing.
	SkipPermissionFlags []string `json:"skipPermissionFlags"`
}

// AgentOption is an AgentKind plus whether this machine can actually run it.
type AgentOption struct {
	AgentKind
	// Installed reports whether the command resolved on the launch PATH. A
	// missing agent is still selectable: it may be installed later, and the
	// resolved PATH of a Dock launch is not always the one a person sees in
	// their terminal.
	Installed bool `json:"installed"`
}

// Profile is one agent profile as the config file declares it.
type Profile struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Flags   []string `json:"flags"`
}

// Workspace is one configured parent directory plus what is at that path now.
type Workspace struct {
	Path string `json:"path"`
	// Exists reports whether the directory is present. A configured path that
	// has been moved or is on an unmounted volume reads as missing rather
	// than as an error: the file is still valid, it just will not find repos.
	Exists bool `json:"exists"`
	// Repos counts immediate subdirectories holding a .git entry. It is the
	// cheap shape of hive's own scan — no git process per repository — so it
	// can run while a folder picker is open. It can read one high: hive skips
	// a repository it cannot read an origin remote from, and this does not.
	// That is the right trade for what it is for, which is confirming the
	// folder is the one the user meant, not promising a launch list.
	Repos int `json:"repos"`
}

// Setup is what the Hive config declares right now, and whether that is
// enough for the session launcher to do anything.
type Setup struct {
	Path string `json:"path"`
	// Exists reports whether the file is on disk. Everything below is read
	// from the file itself, so an absent file reports no profiles and no
	// workspaces rather than hive's fallbacks.
	Exists bool `json:"exists"`
	// EnvironmentOverride reports that HIVE_CONFIG chose this path.
	EnvironmentOverride bool `json:"environmentOverride"`
	// Usable reports that the file declares at least one agent profile and at
	// least one workspace. It is what first run branches on: a file that
	// exists but declares neither leaves the launcher exactly as empty as no
	// file at all.
	Usable bool `json:"usable"`
	// Unreadable carries a parse or read failure. The rest of Setup is then
	// zero, and the caller must not offer to rewrite the file — the user has
	// content here that this app could not understand.
	Unreadable string `json:"unreadable"`

	DefaultAgent string      `json:"defaultAgent"`
	Profiles     []Profile   `json:"profiles"`
	Workspaces   []Workspace `json:"workspaces"`
}

// document is the narrow slice of the Hive config this package reads. Decoding
// into it rather than into hive's Config is deliberate: hive's loader fills in
// defaults, and a default is exactly what Setup must not report as a choice.
type document struct {
	Agents     yaml.Node `yaml:"agents"`
	Workspaces []string  `yaml:"workspaces"`
	// RepoDirs is hive's deprecated spelling of workspaces. It is read so an
	// older config reports as usable, but a write never produces it.
	RepoDirs []string `yaml:"repo_dirs"`
}

// Load reports what the config at path declares. A missing file is not an
// error: it is the ordinary first-run state, and reports Exists false.
func Load(path string, environmentOverride bool) Setup {
	setup := Setup{Path: path, EnvironmentOverride: environmentOverride}
	if path == "" {
		return setup
	}

	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return setup
	case err != nil:
		setup.Unreadable = err.Error()
		return setup
	}
	setup.Exists = true

	var doc document
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		setup.Unreadable = err.Error()
		return setup
	}

	setup.DefaultAgent, setup.Profiles = decodeAgents(doc.Agents)
	paths := doc.Workspaces
	if len(paths) == 0 {
		paths = doc.RepoDirs
	}
	for _, p := range paths {
		setup.Workspaces = append(setup.Workspaces, describeWorkspace(p))
	}
	setup.Usable = len(setup.Profiles) > 0 && len(setup.Workspaces) > 0
	return setup
}

// decodeAgents splits hive's agents mapping into the reserved "default" key
// and the profile entries beside it. It mirrors hive's own UnmarshalYAML
// rather than calling it, so an unknown reserved key is skipped instead of
// being reported as a profile a person could pick.
func decodeAgents(node yaml.Node) (defaultAgent string, profiles []Profile) {
	if node.Kind != yaml.MappingNode {
		return "", nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		switch key {
		case "default":
			defaultAgent = value.Value
		case "agent_selector":
		default:
			var p struct {
				Command string   `yaml:"command"`
				Flags   []string `yaml:"flags"`
			}
			if err := value.Decode(&p); err != nil {
				continue
			}
			command := p.Command
			if command == "" {
				command = key
			}
			profiles = append(profiles, Profile{Name: key, Command: command, Flags: p.Flags})
		}
	}
	return defaultAgent, profiles
}

// Inspect reports what is at a candidate workspace path without reading the
// config. It is what the picker shows after a folder is chosen.
func Inspect(path string) Workspace { return describeWorkspace(path) }

func describeWorkspace(path string) Workspace {
	expanded := ExpandTilde(path)
	w := Workspace{Path: path}
	info, err := os.Stat(expanded)
	if err != nil || !info.IsDir() {
		return w
	}
	w.Exists = true
	entries, err := os.ReadDir(expanded)
	if err != nil {
		return w
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(expanded, entry.Name(), ".git")); err == nil {
			w.Repos++
		}
	}
	return w
}

// LookPath resolves a command on the environment the app launches subprocesses
// with. It is satisfied by execenv.Resolver.LookPath (Consumer-defined
// interfaces, architecture.md).
type LookPath func(ctx context.Context, name string) (string, error)

// AgentOptions returns the catalog with each entry marked by whether its
// command resolves on the launch PATH. Order is the catalog's: installed
// agents are not floated to the top, because a picker whose rows move between
// launches is harder to use than one that does not.
func AgentOptions(ctx context.Context, lookPath LookPath) []AgentOption {
	options := make([]AgentOption, 0, len(KnownAgents))
	for _, kind := range KnownAgents {
		installed := false
		if lookPath != nil {
			_, err := lookPath(ctx, kind.Name)
			installed = err == nil
		}
		options = append(options, AgentOption{AgentKind: kind, Installed: installed})
	}
	return options
}

// ExpandTilde replaces a leading ~ with the user's home directory, matching
// how hive resolves a configured workspace at scan time. A path this app
// cannot expand is returned unchanged so it fails as the literal it is.
func ExpandTilde(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// ErrWorkspaceIsRepo reports a chosen path that is itself a git repository.
// It is the one validation failure worth its own message: picking a repo
// instead of the folder that holds repos is the mistake people actually make,
// and the resulting config finds nothing with no hint as to why.
var ErrWorkspaceIsRepo = InvalidEditError{Reason: "that is a repository, not the folder that holds your repositories"}

// ValidateWorkspace checks a path is usable as a workspace parent. It mirrors
// `hive init`'s validateWorkspaceParent so the two tools reject the same
// input.
func ValidateWorkspace(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return invalid("choose a folder")
	}
	expanded := ExpandTilde(trimmed)
	info, err := os.Stat(expanded)
	if err != nil {
		return invalid("that folder does not exist")
	}
	if !info.IsDir() {
		return invalid("that is a file, not a folder")
	}
	if _, err := os.Stat(filepath.Join(expanded, ".git")); err == nil {
		return ErrWorkspaceIsRepo
	}
	return nil
}
