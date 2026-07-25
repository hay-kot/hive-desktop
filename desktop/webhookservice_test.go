package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// isolateSettings points the desktop config root at a temp dir so settings
// reads and writes never touch the developer's real settings.yaml.
func isolateSettings(t *testing.T) {
	t.Helper()
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	t.Setenv(settings.EnvWebhookPort, "")
}

func TestWebhookServiceInfoWithoutListener(t *testing.T) {
	service := NewWebhookService(nil, nil, 24483)
	info := service.Info()
	assert.False(t, info.Running)
	assert.Equal(t, 24483, info.Port)
	assert.Equal(t, "http://127.0.0.1:24483/hooks/", info.BaseURL)
}

func TestWebhookServiceCapture(t *testing.T) {
	db, err := store.Open(t.TempDir(), store.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	service := NewWebhookService(db, nil, 24483)

	// No delivery captured yet: zero view, no error.
	view, err := service.Capture("triage", "hook")
	require.NoError(t, err)
	assert.Zero(t, view.ReceivedAt)

	ctx := context.Background()
	require.NoError(t, db.Queries().UpsertWebhookCapture(ctx, store.UpsertWebhookCaptureParams{
		Topic: "source:triage/hook", ReceivedAt: 42, Body: []byte(`{"event":"deploy"}`),
	}))
	view, err = service.Capture("triage", "hook")
	require.NoError(t, err)
	assert.Equal(t, int64(42), view.ReceivedAt)
	assert.JSONEq(t, `{"event":"deploy"}`, view.Body)
	assert.False(t, view.FeedShaped)
	assert.Equal(t, []string{"id", "kind", "repo", "title", "url"}, view.MissingFields)

	require.NoError(t, db.Queries().UpsertWebhookCapture(ctx, store.UpsertWebhookCaptureParams{
		Topic: "source:triage/hook", ReceivedAt: 43,
		Body: []byte(`{"id":"1","kind":"Alert","repo":"o/r","title":"t","url":"https://x"}`),
	}))
	view, err = service.Capture("triage", "hook")
	require.NoError(t, err)
	assert.True(t, view.FeedShaped)
	assert.Empty(t, view.MissingFields)
}

func TestWebhookServiceSettingsFirstRun(t *testing.T) {
	isolateSettings(t)
	service := NewWebhookService(nil, nil, 0)

	view, err := service.Settings()
	require.NoError(t, err)
	assert.True(t, view.Enabled, "webhooks default to enabled")
	assert.False(t, view.PortOverridden)
	assert.GreaterOrEqual(t, view.Port, settings.WebhookPortMin)
	assert.LessOrEqual(t, view.Port, settings.WebhookPortMax)
	assert.Equal(t, settings.WebhookPortMin, view.PortMin)
	assert.Equal(t, settings.WebhookPortMax, view.PortMax)
	assert.Empty(t, view.StartError)

	// Reading settings allocated and persisted a port, so it is stable.
	again, err := service.Settings()
	require.NoError(t, err)
	assert.Equal(t, view.Port, again.Port)

	// No listener in this session while settings say enabled: the pane tells
	// the user a restart is what applies it.
	assert.False(t, view.Running)
	assert.Zero(t, view.BoundPort)
	assert.True(t, view.RestartRequired)
}

func TestWebhookServiceSettingsPortOverride(t *testing.T) {
	isolateSettings(t)
	t.Setenv(settings.EnvWebhookPort, "24499")
	service := NewWebhookService(nil, nil, 24499)

	view, err := service.Settings()
	require.NoError(t, err)
	assert.Equal(t, 24499, view.Port)
	assert.True(t, view.PortOverridden)
	assert.Equal(t, "http://127.0.0.1:24499/hooks/", view.BaseURL)
}

func TestWebhookServiceSetSettings(t *testing.T) {
	isolateSettings(t)
	service := NewWebhookService(nil, nil, 0)
	require.NoError(t, settings.SaveSettings(settings.Settings{PollInterval: "2m"}))

	require.NoError(t, service.SetSettings(WebhookSettings{Enabled: false, Port: 27777}))

	view, err := service.Settings()
	require.NoError(t, err)
	assert.False(t, view.Enabled)
	assert.Equal(t, 27777, view.Port)
	assert.Equal(t, "http://127.0.0.1:27777/hooks/", view.BaseURL)
	// Disabled and not running agree, so nothing is pending.
	assert.False(t, view.RestartRequired)

	// Unrelated settings survived the write.
	saved, err := settings.LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, "2m", saved.PollInterval)
}

func TestWebhookServiceSetSettingsRejectsInvalidPort(t *testing.T) {
	isolateSettings(t)
	service := NewWebhookService(nil, nil, 0)

	for _, port := range []int{0, 80, 1023, 65536} {
		require.Error(t, service.SetSettings(WebhookSettings{Enabled: true, Port: port}), "port %d", port)
	}
}

func TestWebhookServiceGeneratePort(t *testing.T) {
	isolateSettings(t)
	service := NewWebhookService(nil, nil, 0)

	port, err := service.GeneratePort()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, port, settings.WebhookPortMin)
	assert.LessOrEqual(t, port, settings.WebhookPortMax)

	// Generating is a candidate only — nothing is persisted until save.
	saved, err := settings.LoadSettings()
	require.NoError(t, err)
	assert.Zero(t, saved.WebhookPort)
}
