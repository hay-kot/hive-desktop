package prompts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/mcpcatalog"
)

func testEnv() Env {
	return Env{
		ConfigDir:          "/home/u/.config/hive/desktop",
		FlowsDir:           "/home/u/.config/hive/desktop/flows",
		ActionsPath:        "/home/u/.config/hive/desktop/actions.yml",
		SettingsPath:       "/home/u/.config/hive/desktop/settings.yaml",
		WebhookBaseURL:     "http://127.0.0.1:24917/hooks",
		WebhookEnabled:     true,
		APIBaseURL:         "http://127.0.0.1:24917/api",
		APIEnabled:         true,
		AgentWorkspacesDir: "/home/u/.config/hive/desktop/workspaces",
	}
}

func testCommands() []Command {
	return []Command{
		{ID: "feed.next", Title: "Next item", Group: "Feeds", Context: "feed", DefaultCombos: []string{"j", "arrowdown"}},
		{ID: "window.hide", Title: "Hide window", Group: "Window", Context: "global", DefaultCombos: nil},
	}
}

func testInput() Input {
	return Input{Commands: testCommands(), WebhookPath: "ci-alerts", WebhookSample: `{"event":"deploy"}`}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(testEnv())
	require.NoError(t, err)
	return svc
}

// TestEveryDefinitionRenders is the guard on the registry: a definition whose
// template is missing or malformed would otherwise silently vanish from the
// settings page (Catalog skips failures so one bad entry cannot empty it).
func TestEveryDefinitionRenders(t *testing.T) {
	svc := newTestService(t)
	for _, id := range IDs() {
		prompt, err := svc.Render(id, testInput())
		require.NoErrorf(t, err, "prompt %q", id)
		assert.Equalf(t, id, prompt.ID, "prompt %q", id)
		assert.NotEmptyf(t, prompt.Title, "prompt %q has no title", id)
		assert.NotEmptyf(t, prompt.Description, "prompt %q has no description", id)
		assert.NotEmptyf(t, strings.TrimSpace(prompt.Text), "prompt %q rendered empty", id)
		// An unresolved template action leaks as "<no value>" rather than failing.
		assert.NotContainsf(t, prompt.Text, "<no value>", "prompt %q has an unresolved template field", id)
	}
}

func TestRenderRejectsUnknownID(t *testing.T) {
	_, err := newTestService(t).Render("not-a-prompt", testInput())
	require.Error(t, err)
}

// TestFlowsPromptCoversEveryNodeType is the acceptance criterion that adding a
// node type extends the prompt with no prose edit: the prompt is asserted
// against the flow registry, not against a fixed list.
func TestFlowsPromptCoversEveryNodeType(t *testing.T) {
	prompt, err := newTestService(t).Render("flows", testInput())
	require.NoError(t, err)

	for _, nodeType := range flow.NodeTypes() {
		assert.Containsf(t, prompt.Text, "`"+nodeType+"`", "flows prompt omits node type %q", nodeType)

		doc, err := flow.NodeDoc(nodeType)
		require.NoError(t, err)
		// The doc's own heading is a stable substring of its content.
		heading := strings.TrimSpace(strings.SplitN(strings.TrimSpace(doc), "\n", 2)[0])
		assert.Containsf(t, prompt.Text, heading, "flows prompt omits the docs for %q", nodeType)
	}

	for _, category := range flow.CategoryOrder {
		assert.Containsf(t, prompt.Text, string(category), "flows prompt omits category %q", category)
	}
}

// TestFlowsPromptEmbedsCanonicalExample ties the prompt to the fixture the
// loader test validates, so the example cannot drift into something invalid.
func TestFlowsPromptEmbedsCanonicalExample(t *testing.T) {
	prompt, err := newTestService(t).Render("flows", testInput())
	require.NoError(t, err)
	assert.Contains(t, prompt.Text, strings.TrimSpace(flow.WorkedExampleYAML))
}

// TestActionsPromptCoversEveryActionType mirrors the flows assertion for the
// action type registry.
func TestActionsPromptCoversEveryActionType(t *testing.T) {
	prompt, err := newTestService(t).Render("actions", testInput())
	require.NoError(t, err)

	for _, actionType := range actions.Types() {
		assert.Containsf(t, prompt.Text, "`"+actionType+"`", "actions prompt omits action type %q", actionType)

		doc, err := actions.Doc(actionType)
		require.NoError(t, err)
		heading := strings.TrimSpace(strings.SplitN(strings.TrimSpace(doc), "\n", 2)[0])
		assert.Containsf(t, prompt.Text, heading, "actions prompt omits the docs for %q", actionType)
	}
	assert.Contains(t, prompt.Text, strings.TrimSpace(actions.ExampleYAML()))
}

// TestAgentWorkspacesPromptCoversEveryMCPType mirrors the actions assertion
// for the shipped MCP catalogue: the prompt↔catalogue bijection, so "a new
// shipped MCP extends the prompt with no prose edit" is a claim something
// checks.
func TestAgentWorkspacesPromptCoversEveryMCPType(t *testing.T) {
	prompt, err := newTestService(t).Render("agent-workspaces", testInput())
	require.NoError(t, err)

	for _, mcpType := range mcpcatalog.Types() {
		assert.Containsf(t, prompt.Text, "`"+mcpType+"`", "agent-workspaces prompt omits MCP type %q", mcpType)

		doc, err := mcpcatalog.Doc(mcpType)
		require.NoError(t, err)
		heading := strings.TrimSpace(strings.SplitN(strings.TrimSpace(doc), "\n", 2)[0])
		assert.Containsf(t, prompt.Text, heading, "agent-workspaces prompt omits the docs for %q", mcpType)
	}
}

// TestPromptsCarryInstallPaths is the point of rendering server-side: a copied
// prompt names the file on this machine, not a placeholder.
func TestPromptsCarryInstallPaths(t *testing.T) {
	env := testEnv()
	svc := newTestService(t)

	for id, want := range map[string]string{
		"flows":            env.FlowsDir,
		"actions":          env.ActionsPath,
		"keybindings":      env.SettingsPath,
		"settings":         env.SettingsPath,
		"webhook-sources":  env.WebhookBaseURL,
		"http-api":         env.APIBaseURL,
		"agent-workspaces": env.AgentWorkspacesDir,
	} {
		prompt, err := svc.Render(id, testInput())
		require.NoErrorf(t, err, "prompt %q", id)
		assert.Containsf(t, prompt.Text, want, "prompt %q does not name %q", id, want)
		assert.NotEmptyf(t, prompt.Target, "prompt %q has no target", id)
	}
}

func TestKeybindingsPromptListsSuppliedCommands(t *testing.T) {
	prompt, err := newTestService(t).Render("keybindings", testInput())
	require.NoError(t, err)

	for _, command := range testCommands() {
		assert.Contains(t, prompt.Text, command.ID)
		assert.Contains(t, prompt.Text, command.Title)
	}
	assert.Contains(t, prompt.Text, "`j`, `arrowdown`")
	// A command with no default combos must read as unbound, not as an empty cell.
	assert.Contains(t, prompt.Text, "(unbound)")
}

// TestKeybindingsPromptRequiresCatalog — a keybindings prompt with no commands
// would be confidently wrong, so it fails instead of rendering an empty table.
func TestKeybindingsPromptRequiresCatalog(t *testing.T) {
	_, err := newTestService(t).Render("keybindings", Input{})
	require.Error(t, err)
}

func TestWebhookSourcesPromptFlagsDisabledListener(t *testing.T) {
	env := testEnv()
	env.WebhookEnabled = false
	svc, err := New(env)
	require.NoError(t, err)

	prompt, err := svc.Render("webhook-sources", testInput())
	require.NoError(t, err)
	assert.Contains(t, prompt.Text, "disabled")

	enabled, err := newTestService(t).Render("webhook-sources", testInput())
	require.NoError(t, err)
	assert.NotContains(t, enabled.Text, "currently **disabled**")
}

func TestWebhookTransformPromptUsesCapturedSample(t *testing.T) {
	svc := newTestService(t)

	withSample, err := svc.Render("webhook-transform", testInput())
	require.NoError(t, err)
	assert.Contains(t, withSample.Text, `{"event":"deploy"}`)
	assert.Contains(t, withSample.Text, `"ci-alerts"`)
	assert.NotContains(t, withSample.Text, "<paste a sample payload here>")

	withoutSample, err := svc.Render("webhook-transform", Input{WebhookPath: "ci"})
	require.NoError(t, err)
	assert.Contains(t, withoutSample.Text, "<paste a sample payload here>")

	_, err = svc.Render("webhook-transform", Input{})
	require.Error(t, err, "a node-scoped prompt with no node is a caller bug")
}

// TestHTTPAPIPromptPointsAtLiveSpec — the prompt's job is to send the agent to
// the self-describing endpoints, and to flag a disabled server rather than
// pointing at a dead port.
func TestHTTPAPIPromptPointsAtLiveSpec(t *testing.T) {
	prompt, err := newTestService(t).Render("http-api", testInput())
	require.NoError(t, err)
	assert.Contains(t, prompt.Text, testEnv().APIBaseURL+"/openapi.json")
	assert.Contains(t, prompt.Text, "OpenAPI")
	assert.NotContains(t, prompt.Text, "**disabled**")

	env := testEnv()
	env.APIEnabled = false
	svc, err := New(env)
	require.NoError(t, err)
	disabled, err := svc.Render("http-api", testInput())
	require.NoError(t, err)
	assert.Contains(t, disabled.Text, "**disabled**")
}

// TestCatalogListsOnlyContextFreePrompts — the settings page renders whatever
// Catalog reports, so a prompt needing instance data must not appear there.
func TestCatalogListsOnlyContextFreePrompts(t *testing.T) {
	catalog := newTestService(t).Catalog(Input{Commands: testCommands()})
	require.NotEmpty(t, catalog)

	ids := make([]string, len(catalog))
	for i, prompt := range catalog {
		ids[i] = prompt.ID
		assert.NotEmptyf(t, prompt.Text, "catalog entry %q rendered empty", prompt.ID)
	}
	assert.Contains(t, ids, "flows")
	assert.Contains(t, ids, "actions")
	assert.NotContains(t, ids, "webhook-transform")
}

// TestCatalogSkipsPromptsItCannotRender — one entry failing must not empty the
// page.
func TestCatalogSkipsPromptsItCannotRender(t *testing.T) {
	catalog := newTestService(t).Catalog(Input{})
	require.NotEmpty(t, catalog)
	for _, prompt := range catalog {
		assert.NotEqual(t, "keybindings", prompt.ID)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	svc := newTestService(t)
	for _, id := range IDs() {
		first, err := svc.Render(id, testInput())
		require.NoError(t, err)
		second, err := svc.Render(id, testInput())
		require.NoError(t, err)
		assert.Equalf(t, first.Text, second.Text, "prompt %q is not deterministic", id)
	}
}
