package hiveconf_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/hiveconf"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/config"
)

// write puts contents at dir/config.yaml and returns the path.
func write(t *testing.T, dir, contents string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

// workspaceDir makes a parent folder holding `repos` git repositories, which
// is the shape every workspace assertion here needs.
func workspaceDir(t *testing.T, repos int) string {
	t.Helper()
	dir := t.TempDir()
	for i := range repos {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, fmt.Sprintf("repo-%d", i), ".git"), 0o755))
	}
	return dir
}

// loadHive loads the written file through hive's own loader, which is the
// only check that matters: hive fails the whole config when agents.default
// names no profile. HIVE_DEFAULT_AGENT is cleared because it overrides
// agents.default at load time, and a developer who has it exported would
// otherwise see these tests pass or fail on their shell rather than on the
// file under test.
func loadHive(t *testing.T, path string) *config.Config {
	t.Helper()
	t.Setenv("HIVE_DEFAULT_AGENT", "")
	loaded, err := config.Load(path, t.TempDir())
	require.NoError(t, err)
	return loaded
}

func TestLoadReportsAMissingFileAsTheOrdinaryFirstRun(t *testing.T) {
	setup := hiveconf.Load(filepath.Join(t.TempDir(), "config.yaml"))

	assert.False(t, setup.Exists)
	assert.False(t, setup.Usable)
	assert.Empty(t, setup.Unreadable, "an absent file is not a failure to report")
	assert.Empty(t, setup.Profiles, "hive's claude fallback is not a choice the user made")
}

func TestLoadReadsWhatTheFileDeclares(t *testing.T) {
	repos := workspaceDir(t, 3)
	path := write(t, t.TempDir(), `
workspaces:
  - `+repos+`
agents:
  default: opencode
  agent_selector: true
  opencode:
    command: opencode
    flags: ["--agent", "free-permissions-runner"]
  claude: {}
`)

	setup := hiveconf.Load(path)

	assert.True(t, setup.Exists)
	assert.True(t, setup.Usable)
	assert.Equal(t, "opencode", setup.DefaultAgent)
	assert.ElementsMatch(t, []hiveconf.Profile{
		{Name: "opencode", Command: "opencode", Flags: []string{"--agent", "free-permissions-runner"}},
		{Name: "claude", Command: "claude"},
	}, setup.Profiles, "a profile with no command is named by its key, and agent_selector is not a profile")
	require.Len(t, setup.Workspaces, 1)
	assert.True(t, setup.Workspaces[0].Exists)
	assert.Equal(t, 3, setup.Workspaces[0].Repos)
}

// TestLoadReportsAConfigWithNoWorkspacesAsUnusable is the case first run turns
// on: the file is present, so "do they have a config?" says yes, while the
// session launcher it feeds has nothing to list.
func TestLoadReportsAConfigWithNoWorkspacesAsUnusable(t *testing.T) {
	path := write(t, t.TempDir(), "agents:\n  default: claude\n  claude: {}\n")

	setup := hiveconf.Load(path)

	assert.True(t, setup.Exists)
	assert.False(t, setup.Usable)
	assert.Len(t, setup.Profiles, 1, "what it does declare is still reported")
}

func TestLoadReadsHivesDeprecatedRepoDirsSpelling(t *testing.T) {
	repos := workspaceDir(t, 1)
	path := write(t, t.TempDir(), "repo_dirs:\n  - "+repos+"\nagents:\n  default: claude\n  claude: {}\n")

	setup := hiveconf.Load(path)

	assert.True(t, setup.Usable, "an older config is not an unconfigured one")
}

// TestLoadReportsAnUnparseableFileWithoutClaimingItIsEmpty: the caller must be
// able to tell "no config" from "a config this app could not read", because
// only one of those is safe to write over.
func TestLoadReportsAnUnparseableFileWithoutClaimingItIsEmpty(t *testing.T) {
	path := write(t, t.TempDir(), "workspaces: [\n")

	setup := hiveconf.Load(path)

	assert.True(t, setup.Exists)
	assert.False(t, setup.Usable)
	assert.NotEmpty(t, setup.Unreadable)
}

func TestLoadCountsOnlyDirectoriesThatAreRepositories(t *testing.T) {
	dir := workspaceDir(t, 2)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "not-a-repo"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".hidden", ".git"), 0o755))
	path := write(t, t.TempDir(), "workspaces:\n  - "+dir+"\nagents:\n  default: claude\n  claude: {}\n")

	setup := hiveconf.Load(path)

	assert.Equal(t, 2, setup.Workspaces[0].Repos)
}

func TestLoadMarksAConfiguredFolderThatIsNoLongerThere(t *testing.T) {
	path := write(t, t.TempDir(), "workspaces:\n  - /nope/gone\nagents:\n  default: claude\n  claude: {}\n")

	setup := hiveconf.Load(path)

	require.Len(t, setup.Workspaces, 1)
	assert.False(t, setup.Workspaces[0].Exists)
	assert.True(t, setup.Usable, "a moved folder leaves the file valid; it just finds nothing")
}

func TestAgentOptionsMarkWhatThisMachineCanRun(t *testing.T) {
	options := hiveconf.AgentOptions(t.Context(), func(_ context.Context, name string) (string, error) {
		if name == "opencode" {
			return "/usr/local/bin/opencode", nil
		}
		return "", os.ErrNotExist
	})

	require.NotEmpty(t, options)
	assert.Equal(t, "claude", options[0].Name, "the catalog keeps its order so the picker does not reshuffle between launches")
	for _, option := range options {
		assert.Equal(t, option.Name == "opencode", option.Installed)
	}
}

func TestValidateWorkspaceRejectsARepositoryItself(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))

	require.ErrorIs(t, hiveconf.ValidateWorkspace(dir), hiveconf.ErrWorkspaceIsRepo)
	require.Error(t, hiveconf.ValidateWorkspace(filepath.Join(dir, "missing")))
	require.NoError(t, hiveconf.ValidateWorkspace(t.TempDir()))
}

// TestApplyWritesAConfigHiveCanLoad is the contract that matters most: hive
// fails the whole file when agents.default names no profile, so a config this
// app writes and cannot load again would stop the next launch.
func TestApplyWritesAConfigHiveCanLoad(t *testing.T) {
	repos := workspaceDir(t, 1)
	path := filepath.Join(t.TempDir(), "config.yaml")

	require.NoError(t, hiveconf.Apply(path, hiveconf.Edit{
		DefaultAgent: "opencode",
		Profiles: []hiveconf.Profile{
			{Name: "opencode", Command: "opencode", Flags: []string{"--agent", "free-permissions-runner"}},
			{Name: "claude", Command: "claude"},
		},
		Workspaces: []string{repos},
	}))

	loaded := loadHive(t, path)
	assert.Equal(t, "opencode", loaded.Agents.Default)
	assert.Equal(t, []string{repos}, loaded.Workspaces)
	assert.Equal(t, []string{"--agent", "free-permissions-runner"}, loaded.Agents.Profiles["opencode"].Flags)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "# Hive configuration", "a created file explains itself")
}

// TestApplyKeepsEverythingItDoesNotOwn: the file is shared with the hive CLI,
// so a write that dropped a user's rules or comments would be destroying work
// this app never asked about.
func TestApplyKeepsEverythingItDoesNotOwn(t *testing.T) {
	repos := workspaceDir(t, 1)
	path := write(t, t.TempDir(), `# my hive setup
copy_command: pbcopy

# these matter to me
rules:
  - pattern: ""
    max_recycled: 3

agents:
  default: claude
  agent_selector: true
  claude:
    command: claude
    flags: []

workspaces:
  - /old/place
`)

	require.NoError(t, hiveconf.Apply(path, hiveconf.Edit{
		DefaultAgent: "codex",
		Profiles:     []hiveconf.Profile{{Name: "codex", Command: "codex", Flags: []string{"--full-auto"}}},
		Workspaces:   []string{repos},
	}))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	out := string(raw)
	assert.Contains(t, out, "# my hive setup")
	assert.Contains(t, out, "# these matter to me")
	assert.Contains(t, out, "pbcopy")
	assert.Contains(t, out, "max_recycled: 3")
	assert.Contains(t, out, "agent_selector: true", "a reserved key is not a profile to reconcile away")
	assert.NotContains(t, out, "/old/place")
	assert.NotContains(t, out, "claude", "a profile the edit dropped is gone, not left as a second default")

	loaded := loadHive(t, path)
	assert.Equal(t, "codex", loaded.Agents.Default)
	assert.Len(t, loaded.Rules, 1)
}

// TestApplyKeepsAYAMLMergeKeyInsideAgents: "<<" is not a profile name this app
// invented, and reconciling it away would edit a line the user wrote for
// reasons of their own. Hive does not resolve a merge key inside agents — its
// own UnmarshalYAML reads "<<" as a profile key — so preserving it is about
// not destroying someone's text, not about making the merge work.
func TestApplyKeepsAYAMLMergeKeyInsideAgents(t *testing.T) {
	repos := workspaceDir(t, 1)
	path := write(t, t.TempDir(), `shared: &shared
  codex:
    command: codex
    flags: []

agents:
  <<: *shared
  default: claude
  claude:
    command: claude
    flags: []
`)

	require.NoError(t, hiveconf.Apply(path, hiveconf.Edit{
		DefaultAgent: "claude",
		Profiles:     []hiveconf.Profile{{Name: "claude", Command: "claude"}},
		Workspaces:   []string{repos},
	}))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "<<: *shared")

	loaded := loadHive(t, path)
	assert.Equal(t, "claude", loaded.Agents.Default, "the file still loads, which is the contract that matters")
}

func TestApplyPreservesAnUnknownKeyInsideAProfileItRewrites(t *testing.T) {
	repos := workspaceDir(t, 1)
	path := write(t, t.TempDir(), `agents:
  default: claude
  claude:
    command: claude
    flags: []
    future_hive_key: keep-me
`)

	require.NoError(t, hiveconf.Apply(path, hiveconf.Edit{
		DefaultAgent: "claude",
		Profiles:     []hiveconf.Profile{{Name: "claude", Command: "claude", Flags: []string{"--dangerously-skip-permissions"}}},
		Workspaces:   []string{repos},
	}))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "future_hive_key: keep-me")
	assert.Contains(t, string(raw), "--dangerously-skip-permissions")
}

// TestApplyDropsTheDeprecatedRepoDirsKeyItReplaces: hive reads repo_dirs only
// when workspaces is empty, so leaving it is not harmless duplication — it is
// what an older hive would read instead of the edit.
func TestApplyDropsTheDeprecatedRepoDirsKeyItReplaces(t *testing.T) {
	repos := workspaceDir(t, 1)
	path := write(t, t.TempDir(), "repo_dirs:\n  - /old\nagents:\n  default: claude\n  claude: {}\n")

	require.NoError(t, hiveconf.Apply(path, hiveconf.Edit{
		DefaultAgent: "claude",
		Profiles:     []hiveconf.Profile{{Name: "claude"}},
		Workspaces:   []string{repos},
	}))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "repo_dirs")
	assert.Contains(t, string(raw), repos)
}

// TestApplyFillsAFileHiveCreatedButLeftEmpty: a file with comments but no keys
// is rendered whole, not edited in place. yaml.v3 attaches a keyless
// document's comments to no node, so an in-place edit would drop them and
// leave a config with no header at all.
func TestApplyFillsAFileHiveCreatedButLeftEmpty(t *testing.T) {
	repos := workspaceDir(t, 1)
	for name, existing := range map[string]string{
		"comments only": "# Hive configuration\n",
		"empty":         "",
	} {
		t.Run(name, func(t *testing.T) {
			path := write(t, t.TempDir(), existing)

			require.NoError(t, hiveconf.Apply(path, hiveconf.Edit{
				DefaultAgent: "claude",
				Profiles:     []hiveconf.Profile{{Name: "claude"}},
				Workspaces:   []string{repos},
			}))

			loaded := loadHive(t, path)
			assert.Equal(t, []string{repos}, loaded.Workspaces)

			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, string(raw), "Reference: https://hive.colonyops.io/configuration")
		})
	}
}

func TestApplyRefusesAnEditThatWouldNotLoadAndLeavesTheFileAlone(t *testing.T) {
	repos := workspaceDir(t, 1)
	original := "agents:\n  default: claude\n  claude: {}\n"
	path := write(t, t.TempDir(), original)

	for name, edit := range map[string]hiveconf.Edit{
		"default names no chosen profile": {
			DefaultAgent: "codex",
			Profiles:     []hiveconf.Profile{{Name: "claude"}},
			Workspaces:   []string{repos},
		},
		"no agents at all": {
			DefaultAgent: "claude",
			Workspaces:   []string{repos},
		},
		"no workspaces": {
			DefaultAgent: "claude",
			Profiles:     []hiveconf.Profile{{Name: "claude"}},
		},
		"a workspace that is not there": {
			DefaultAgent: "claude",
			Profiles:     []hiveconf.Profile{{Name: "claude"}},
			Workspaces:   []string{filepath.Join(repos, "nope")},
		},
		"the same agent twice": {
			DefaultAgent: "claude",
			Profiles:     []hiveconf.Profile{{Name: "claude"}, {Name: "claude"}},
			Workspaces:   []string{repos},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, hiveconf.Apply(path, edit))
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, original, string(raw), "a rejected edit writes nothing")
		})
	}
}

func TestApplyQuotesAPathThatWouldChangeMeaningUnquoted(t *testing.T) {
	dir := t.TempDir()
	odd := filepath.Join(dir, "code: mine")
	require.NoError(t, os.MkdirAll(odd, 0o755))
	path := filepath.Join(dir, "config.yaml")

	require.NoError(t, hiveconf.Apply(path, hiveconf.Edit{
		DefaultAgent: "claude",
		Profiles:     []hiveconf.Profile{{Name: "claude"}},
		Workspaces:   []string{odd},
	}))

	loaded := loadHive(t, path)
	assert.Equal(t, []string{odd}, loaded.Workspaces)
}

func TestExpandTildeResolvesAgainstHome(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, home, hiveconf.ExpandTilde("~"))
	assert.Equal(t, filepath.Join(home, "code"), hiveconf.ExpandTilde("~/code"))
	assert.Equal(t, "/abs/path", hiveconf.ExpandTilde("/abs/path"))
	assert.True(t, strings.HasPrefix(hiveconf.ExpandTilde("~notauser/x"), "~"), "only this user's home expands")
}
