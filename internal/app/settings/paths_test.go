package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlowsDirFollowsTheConfigRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvConfigDir, filepath.Join(root, "config"))

	assert.Equal(t, filepath.Join(root, "config", "flows"), FlowsDir())
}

// The onboarding mock mode stands in for a fresh install, and first run is
// gated on having no workspaces. Reading a config root that already holds
// flows would boot it straight to the feed — the one thing the mode exists to
// avoid.
func TestFlowsDirIsScratchInTheOnboardingMockMode(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "config", "flows")
	require.NoError(t, os.MkdirAll(real, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(real, "triage.yaml"), []byte("nodes: []\n"), 0o600))

	t.Setenv(EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(EnvMockMode, MockOnboarding)

	dir := FlowsDir()
	require.NotEqual(t, real, dir)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the onboarding mock mode starts with no workspaces")

	// Resolved once per process: the flow store, the watcher, and the prompts
	// env all call this and must agree on one directory.
	assert.Equal(t, dir, FlowsDir())
}

// An explicit override is how the e2e harness — and anyone wanting a specific
// fixture set — opts out of the scratch directory.
func TestResolvePathsUsesResolvedYAMLMockMode(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvConfigDir, filepath.Join(root, "config"))
	unsetEnv(t, EnvMockMode)

	paths := ResolvePaths(Bootstrap{}, MockOnboarding)
	require.NotEqual(t, filepath.Join(root, "config", "flows"), paths.FlowsDir)
}

func TestFlowsDirOverrideWinsOverTheOnboardingMockMode(t *testing.T) {
	t.Setenv(EnvMockMode, MockOnboarding)
	t.Setenv(EnvFlowsDir, "/tmp/explicit-flows")

	assert.Equal(t, "/tmp/explicit-flows", FlowsDir())
}
