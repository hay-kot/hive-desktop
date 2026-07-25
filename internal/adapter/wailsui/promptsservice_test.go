package wailsui

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/prompts"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// isolateConfig points the desktop config paths at a temp dir so these tests
// never read or write the developer's real settings.
func isolateConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(settings.EnvConfigPath, filepath.Join(dir, "profiles.yaml"))
	return dir
}

func testCatalogInput() prompts.Input {
	return prompts.Input{Commands: []prompts.Command{
		{ID: "feed.next", Title: "Next item", Group: "Feeds", Context: "feed", DefaultCombos: []string{"j"}},
	}}
}

// TestCatalogRendersAgainstThisInstall is the reason prompts render in Go: a
// copied prompt has to name the paths on this machine.
func TestCatalogRendersAgainstThisInstall(t *testing.T) {
	dir := isolateConfig(t)
	svc := NewPromptsService(nil, 24917)

	catalog, err := svc.Catalog(testCatalogInput())
	require.NoError(t, err)
	require.NotEmpty(t, catalog)

	byID := make(map[string]prompts.Prompt, len(catalog))
	for _, prompt := range catalog {
		assert.NotEmptyf(t, prompt.Text, "prompt %q rendered empty", prompt.ID)
		assert.NotEmptyf(t, prompt.Title, "prompt %q has no title", prompt.ID)
		byID[prompt.ID] = prompt
	}

	assert.Contains(t, byID["flows"].Text, filepath.Join(dir, "flows"))
	assert.Contains(t, byID["actions"].Text, filepath.Join(dir, "actions.yml"))
	assert.Contains(t, byID["settings"].Text, filepath.Join(dir, "settings.yaml"))
	assert.Contains(t, byID["webhook-sources"].Text, "24917")
}

// TestCatalogSurvivesAnEmptyConfigRoot — the prompts page is most useful on a
// fresh install, before any config file exists.
func TestCatalogSurvivesAnEmptyConfigRoot(t *testing.T) {
	isolateConfig(t)
	catalog, err := NewPromptsService(nil, 0).Catalog(testCatalogInput())
	require.NoError(t, err)
	assert.NotEmpty(t, catalog)
}

func TestRenderReturnsContextScopedPrompts(t *testing.T) {
	isolateConfig(t)
	svc := NewPromptsService(nil, 24917)

	prompt, err := svc.Render("webhook-transform", prompts.Input{
		WebhookPath:   "ci-alerts",
		WebhookSample: `{"event":"deploy"}`,
	})
	require.NoError(t, err)
	assert.Contains(t, prompt.Text, "ci-alerts")
	assert.Contains(t, prompt.Text, `{"event":"deploy"}`)

	_, err = svc.Render("not-a-prompt", prompts.Input{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown prompt")
}
