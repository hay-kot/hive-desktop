package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/hiveconf"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// hiveSetupApp builds a real core against a temporary Hive config that does
// not exist yet — the first-run state — and a workspace folder holding one
// repository.
func hiveSetupApp(t *testing.T) (core *App, configPath, workspace string) {
	t.Helper()
	root := t.TempDir()
	configPath = filepath.Join(root, "hive.yaml")
	workspace = filepath.Join(root, "code")
	seedRepo(t, workspace, "a-repo")

	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv("HIVE_CONFIG", configPath)
	// HIVE_DEFAULT_AGENT wins over agents.default at load, so a developer who
	// exports it would otherwise see these assertions answer to their shell.
	t.Setenv("HIVE_DEFAULT_AGENT", "")
	t.Setenv(settings.EnvMockMode, "feed")

	core, err := New(t.Context(), Config{
		Settings: settings.DefaultSettings(),
		MockMode: settings.MockMode(),
		Logger:   zerolog.Nop(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })
	return core, configPath, workspace
}

// seedRepo makes a real git repository with an origin remote under parent.
// Hive's scan reads the remote and skips a directory it cannot get one from,
// so a bare .git directory would not be discovered — the repository has to be
// real for the launch options to list it.
func seedRepo(t *testing.T, parent, name string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := filepath.Join(parent, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"remote", "add", "origin", "https://github.com/example/" + name + ".git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
}

func TestSetupReportsFirstRunWhenNoConfigExists(t *testing.T) {
	core, configPath, _ := hiveSetupApp(t)

	setup := core.HiveConfig.Setup(t.Context())

	assert.Equal(t, configPath, setup.Config.Path)
	assert.False(t, setup.Config.Exists)
	assert.False(t, setup.Config.Usable)
	assert.NotEmpty(t, setup.Agents, "the picker has a catalog to offer even with no config")
	assert.Empty(t, setup.Config.Profiles, "hive's claude fallback is not a choice the user made")
}

// TestSaveTakesEffectWithoutARestart is the feature: the session launcher
// reads the Hive config, the app can now write that config, and the two have
// to agree in the same process — otherwise first run ends with a setup the
// user cannot use until they quit and reopen.
func TestSaveTakesEffectWithoutARestart(t *testing.T) {
	core, _, workspace := hiveSetupApp(t)

	before, err := core.Sessions.SessionLaunchOptions(t.Context())
	require.NoError(t, err)
	require.Empty(t, before.Repositories, "with no config there is nothing to launch against")

	saved, err := core.HiveConfig.Save(t.Context(), HiveSetupRequest{
		DefaultAgent: "opencode",
		Profiles: []hiveconf.Profile{
			{Name: "opencode", Command: "opencode", Flags: []string{"--agent", "free-permissions-runner"}},
		},
		Workspaces: []string{workspace},
	})
	require.NoError(t, err)
	assert.True(t, saved.Config.Usable)

	after, err := core.Sessions.SessionLaunchOptions(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"opencode"}, after.Agents)
	assert.Equal(t, "opencode", after.DefaultAgent)
	require.Len(t, after.Repositories, 1, "the repository under the saved workspace is offered now, not after a relaunch")
	assert.Equal(t, "a-repo", after.Repositories[0].Name)
}

// TestSaveUpdatesTheWorkspacePresets: the agent workspace editor's preset list
// is built from the same config, and it holds a function rather than a
// captured map precisely so a reload reaches it.
func TestSaveUpdatesTheWorkspacePresets(t *testing.T) {
	core, _, workspace := hiveSetupApp(t)

	_, err := core.HiveConfig.Save(t.Context(), HiveSetupRequest{
		DefaultAgent: "my-agent",
		Profiles:     []hiveconf.Profile{{Name: "my-agent", Command: "my-agent", Flags: []string{"--yolo"}}},
		Workspaces:   []string{workspace},
	})
	require.NoError(t, err)

	var found bool
	for _, preset := range core.AgentWorkspaces.Presets(t.Context()) {
		if preset.ID == "hive-my-agent" {
			found = true
		}
	}
	assert.True(t, found, "a profile saved just now is offered as a preset")
}

func TestSaveRejectsAnEditThatWouldNotLoadAndLeavesTheRuntimeAlone(t *testing.T) {
	core, configPath, workspace := hiveSetupApp(t)

	_, err := core.HiveConfig.Save(t.Context(), HiveSetupRequest{
		DefaultAgent: "codex",
		Profiles:     []hiveconf.Profile{{Name: "claude"}},
		Workspaces:   []string{workspace},
	})

	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err), "a default naming no chosen agent is the user's to fix, not a crash")
	_, statErr := os.Stat(configPath)
	assert.True(t, os.IsNotExist(statErr), "a rejected save writes no file at all")
}

func TestSaveRewritesOnlyTheKeysItOwns(t *testing.T) {
	core, configPath, workspace := hiveSetupApp(t)
	require.NoError(t, os.WriteFile(configPath, []byte("copy_command: pbcopy\n# keep me\ntmux:\n  poll_interval: 900ms\n"), 0o644))
	require.NoError(t, core.ReloadHiveRuntime(t.Context()))

	_, err := core.HiveConfig.Save(t.Context(), HiveSetupRequest{
		DefaultAgent: "claude",
		Profiles:     []hiveconf.Profile{{Name: "claude", Command: "claude"}},
		Workspaces:   []string{workspace},
	})
	require.NoError(t, err)

	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "# keep me")
	assert.Contains(t, string(raw), "poll_interval: 900ms")
	assert.Contains(t, string(raw), "pbcopy")
}

func TestSetupReportsTheEnvironmentOverridingTheChosenAgent(t *testing.T) {
	core, _, workspace := hiveSetupApp(t)
	_, err := core.HiveConfig.Save(t.Context(), HiveSetupRequest{
		DefaultAgent: "codex",
		Profiles:     []hiveconf.Profile{{Name: "codex"}, {Name: "claude"}},
		Workspaces:   []string{workspace},
	})
	require.NoError(t, err)
	t.Setenv("HIVE_DEFAULT_AGENT", "claude")

	setup := core.HiveConfig.Setup(t.Context())

	assert.Equal(t, "codex", setup.Config.DefaultAgent, "the file still says what the user chose")
	assert.Equal(t, "claude", setup.DefaultAgentOverride, "and the screen can say what is actually winning")
}

// Hive ignores HIVE_DEFAULT_AGENT when it names no configured profile, so
// reporting one would warn about an override that is not happening.
func TestSetupIgnoresAnEnvironmentAgentThatIsNotConfigured(t *testing.T) {
	core, _, workspace := hiveSetupApp(t)
	_, err := core.HiveConfig.Save(t.Context(), HiveSetupRequest{
		DefaultAgent: "codex",
		Profiles:     []hiveconf.Profile{{Name: "codex"}},
		Workspaces:   []string{workspace},
	})
	require.NoError(t, err)
	t.Setenv("HIVE_DEFAULT_AGENT", "not-a-profile")

	setup := core.HiveConfig.Setup(t.Context())

	assert.Empty(t, setup.DefaultAgentOverride)
	assert.Equal(t, "codex", setup.Config.DefaultAgent)
}

func TestInspectWorkspaceAnswersBeforeTheSave(t *testing.T) {
	core, _, workspace := hiveSetupApp(t)

	found, err := core.HiveConfig.InspectWorkspace(t.Context(), workspace)
	require.NoError(t, err)
	assert.True(t, found.Exists)
	assert.Equal(t, 1, found.Repos)

	_, err = core.HiveConfig.InspectWorkspace(t.Context(), filepath.Join(workspace, "a-repo"))
	require.Error(t, err, "a repository is not the folder that holds repositories")
	assert.Equal(t, KindInvalid, KindOf(err))
}

// TestReloadFailureLeavesTheRunningServicesServing: a config edited to
// something hive refuses must not leave the process with no session service.
func TestReloadFailureLeavesTheRunningServicesServing(t *testing.T) {
	core, configPath, workspace := hiveSetupApp(t)
	_, err := core.HiveConfig.Save(t.Context(), HiveSetupRequest{
		DefaultAgent: "claude",
		Profiles:     []hiveconf.Profile{{Name: "claude"}},
		Workspaces:   []string{workspace},
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(configPath, []byte("workspaces: [\n"), 0o644))
	require.Error(t, core.ReloadHiveRuntime(t.Context()))

	after, err := core.Sessions.SessionLaunchOptions(t.Context())
	require.NoError(t, err)
	assert.Len(t, after.Repositories, 1, "the last good config is still serving")
}
