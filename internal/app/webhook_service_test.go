package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/sourcemark"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

func isolateSettings(t *testing.T) {
	t.Helper()
	t.Setenv(settings.EnvConfigDir, filepath.Join(t.TempDir(), "config"))
	t.Setenv(settings.EnvHTTPPort, "")
}

func testSettingsStore(t *testing.T) *settings.Store {
	t.Helper()
	return settings.NewStore(filepath.Join(t.TempDir(), "settings.yaml"))
}

func TestWebhookServiceInfoWithoutListener(t *testing.T) {
	service := newWebhookService(testSettingsStore(t), nil, nil, nil, "127.0.0.1", 24483)
	running, port := service.Endpoint(t.Context())
	assert.False(t, running)
	assert.Equal(t, 24483, port)
	assert.Equal(t, "http://127.0.0.1:24483/hooks/", WebhookBaseURLAt(service.Host(), port))
}

func TestWebhookServiceCapture(t *testing.T) {
	db, err := store.Open(t.Context(), t.TempDir(), store.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	service := newWebhookService(testSettingsStore(t), db, nil, nil, "127.0.0.1", 24483)

	view, err := service.Capture(t.Context(), "triage", "hook")
	require.NoError(t, err)
	assert.Zero(t, view.ReceivedAt)

	ctx := t.Context()
	require.NoError(t, db.Queries().UpsertWebhookCapture(ctx, store.UpsertWebhookCaptureParams{
		Topic: "source:triage/hook", ReceivedAt: 42, Body: []byte(`{"event":"deploy"}`),
	}))
	view, err = service.Capture(t.Context(), "triage", "hook")
	require.NoError(t, err)
	assert.Equal(t, int64(42), view.ReceivedAt)
	assert.False(t, view.FeedShaped)
	assert.Equal(t, []string{"id", "kind", "repo", "title", "url"}, view.MissingFields)
}

func TestWebhookServiceSettingsDefaultEnabled(t *testing.T) {
	isolateSettings(t)
	service := newWebhookService(testSettingsStore(t), nil, nil, nil, "127.0.0.1", 0)

	view, err := service.State(t.Context())
	require.NoError(t, err)
	assert.True(t, view.Enabled, "the loopback HTTP server is on by default")
	assert.Equal(t, "127.0.0.1", view.Host)
	assert.Zero(t, view.Port)
	assert.False(t, view.PortOverridden)
	assert.False(t, view.Running, "no listener was constructed for this test")
	assert.True(t, view.RestartRequired, "enabled in config but not yet running")
}

func TestWebhookServiceSettingsPortOverride(t *testing.T) {
	isolateSettings(t)
	t.Setenv(settings.EnvHTTPPort, "24499")
	service := newWebhookService(testSettingsStore(t), nil, nil, nil, "127.0.0.1", 24499)

	view, err := service.State(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 24499, view.Port)
	assert.True(t, view.PortOverridden)
}

func TestWebhookServiceSetSettings(t *testing.T) {
	isolateSettings(t)
	service := newWebhookService(testSettingsStore(t), nil, nil, nil, "127.0.0.1", 0)
	cfg := settings.DefaultSettings()
	cfg.Polling.Interval = settings.Duration(2 * time.Minute)
	require.NoError(t, settings.SaveSettings(cfg))

	require.NoError(t, service.SetState(t.Context(), true, "127.0.0.1", 27777))

	view, err := service.State(t.Context())
	require.NoError(t, err)
	assert.True(t, view.Enabled)
	assert.Equal(t, 27777, view.Port)
	assert.True(t, view.RestartRequired)

	saved, err := settings.LoadPersistedSettings()
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, saved.Polling.Interval.Duration())
}

func TestWebhookServiceSetSettingsValidation(t *testing.T) {
	isolateSettings(t)
	service := newWebhookService(testSettingsStore(t), nil, nil, nil, "127.0.0.1", 0)

	for _, tc := range []struct {
		host string
		port int
	}{{"0.0.0.0", 0}, {"127.0.0.1", 80}, {"127.0.0.1", 65536}} {
		err := service.SetState(t.Context(), true, tc.host, tc.port)
		require.Error(t, err)
		require.Equal(t, KindInvalid, KindOf(err))
	}
	require.NoError(t, service.SetState(t.Context(), true, "127.0.0.1", 0))
}

func TestWebhookServiceGeneratePort(t *testing.T) {
	isolateSettings(t)
	service := newWebhookService(testSettingsStore(t), nil, nil, nil, "127.0.0.1", 0)
	port, err := service.GeneratePort(t.Context())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, port, settings.WebhookPortMin)
	assert.LessOrEqual(t, port, settings.WebhookPortMax)
}

func TestWebhookServiceMarkImageRoundTrip(t *testing.T) {
	marks := sourcemark.NewStore(t.TempDir())
	service := newWebhookService(testSettingsStore(t), nil, nil, marks, "127.0.0.1", 0)

	hash, err := service.StoreMarkImage(t.Context(), testPNG(t))
	require.NoError(t, err)
	require.True(t, sourcemark.ValidHash(hash))

	data, ok, err := service.MarkImage(t.Context(), hash)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.NotEmpty(t, data)

	// A hash with no stored file resolves as absent, not an error — the feed
	// falls back to the glyph.
	_, ok, err = service.MarkImage(t.Context(), "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestWebhookServiceStoreMarkImageRejectsBadInput(t *testing.T) {
	service := newWebhookService(testSettingsStore(t), nil, nil, sourcemark.NewStore(t.TempDir()), "127.0.0.1", 0)

	_, err := service.StoreMarkImage(t.Context(), []byte("not an image"))
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err))
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := range 40 {
		for x := range 40 {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}
