package wailsui

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

func newTestCore(t *testing.T) *app.ReleaseNotesService {
	t.Helper()
	return app.NewReleaseNotesService(settings.Paths{StateDir: t.TempDir()}, zerolog.Nop())
}

func keepChannel(defaultChannel string) string { return defaultChannel }

// A published build follows its own channel unless settings override it — the
// same resolution attachUpdater uses.
func TestReleaseNotesServiceResolvesTheBuildChannel(t *testing.T) {
	for version, want := range map[string]string{
		"1.2.0":        settings.ChannelStable,
		"1.2.0-beta.1": settings.ChannelBeta,
		"1.2.0-dev.9":  settings.ChannelDev,
	} {
		service := NewReleaseNotesService(newTestCore(t), version, keepChannel)
		assert.Equal(t, want, service.channel, "version %q", version)
	}
}

func TestReleaseNotesServiceHonoursASettingsOverride(t *testing.T) {
	service := NewReleaseNotesService(newTestCore(t), "1.2.0", func(string) string { return settings.ChannelDev })

	assert.Equal(t, settings.ChannelDev, service.channel)
}

// A source build has no channel of its own, so its history falls back to dev —
// the channel that receives everything — while Pending stays silent.
func TestReleaseNotesServiceFallsBackToDevForSourceBuilds(t *testing.T) {
	service := NewReleaseNotesService(newTestCore(t), "dev", keepChannel)

	assert.Equal(t, settings.ChannelDev, service.channel)
	assert.False(t, service.Pending(t.Context()).Show)
	assert.NotEmpty(t, service.History(t.Context()), "a source build can still read the changelog")
}

func TestReleaseNotesServiceMapsEntriesForTheFrontend(t *testing.T) {
	service := NewReleaseNotesService(newTestCore(t), "dev", keepChannel)

	history := service.History(t.Context())
	require.NotEmpty(t, history)
	newest := history[0]
	assert.NotEmpty(t, newest.Version)
	assert.NotEmpty(t, newest.Body)
	assert.NotEmpty(t, newest.Channel)
	// Dates cross the boundary as plain YYYY-MM-DD, not as an instant.
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, newest.Date)
}

func TestReleaseNotesServicePendingAndAcknowledge(t *testing.T) {
	core := newTestCore(t)
	require.NoError(t, core.Acknowledge(t.Context(), "0.0.1"))

	service := NewReleaseNotesService(core, "1.2.0-dev.9", keepChannel)
	require.True(t, service.Pending(t.Context()).Show, "a build newer than the acknowledged version surfaces")

	require.NoError(t, service.Acknowledge(t.Context()))
	assert.False(t, service.Pending(t.Context()).Show)
}
