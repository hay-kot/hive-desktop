package app

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/releasenotes"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

func newTestReleaseNotes(t *testing.T, entries releasenotes.Entries) *ReleaseNotesService {
	t.Helper()
	return &ReleaseNotesService{entries: entries, state: releasenotes.NewState(t.TempDir())}
}

func testEntries(versions ...string) releasenotes.Entries {
	out := make(releasenotes.Entries, 0, len(versions))
	for _, v := range versions {
		channel, _ := releasenotes.Channel(v)
		out = append(out, releasenotes.Entry{Version: v, Channel: channel, Body: "notes for " + v})
	}
	return out
}

// A fresh install has no version it upgraded *from*, so it adopts the running
// version silently rather than announcing a release the user never crossed.
func TestPendingIsSilentOnAFreshInstall(t *testing.T) {
	service := newTestReleaseNotes(t, testEntries("1.2.0"))

	assert.False(t, service.Pending("1.2.0", settings.ChannelStable).Show)
	assert.False(t, service.Pending("1.2.0", settings.ChannelStable).Show,
		"the recorded version keeps it silent on the next launch too")
}

func TestPendingShowsNotesAfterAnUpgrade(t *testing.T) {
	service := newTestReleaseNotes(t, testEntries("1.3.0", "1.2.0"))
	require.NoError(t, service.Acknowledge("1.2.0"))

	pending := service.Pending("1.3.0", settings.ChannelStable)
	require.True(t, pending.Show)
	assert.Equal(t, PresentationModal, pending.Presentation)
	assert.Equal(t, "1.3.0", pending.Version)
	require.Len(t, pending.Entries, 1)
	assert.Equal(t, "1.3.0", pending.Entries[0].Version)
}

// The acceptance criterion: shown once, then not again for that version.
func TestPendingStopsAfterAcknowledgement(t *testing.T) {
	service := newTestReleaseNotes(t, testEntries("1.3.0", "1.2.0"))
	require.NoError(t, service.Acknowledge("1.2.0"))

	require.True(t, service.Pending("1.3.0", settings.ChannelStable).Show)
	require.True(t, service.Pending("1.3.0", settings.ChannelStable).Show,
		"an unacknowledged surface returns until it is dismissed")

	require.NoError(t, service.Acknowledge("1.3.0"))
	assert.False(t, service.Pending("1.3.0", settings.ChannelStable).Show)
}

func TestPendingIsSilentOnSourceBuilds(t *testing.T) {
	service := newTestReleaseNotes(t, testEntries("1.3.0"))
	require.NoError(t, service.Acknowledge("1.2.0"))

	assert.False(t, service.Pending("dev", settings.ChannelDev).Show)
	assert.False(t, service.Pending("(devel)", settings.ChannelDev).Show)
}

func TestPendingIsSilentOnADowngrade(t *testing.T) {
	service := newTestReleaseNotes(t, testEntries("1.3.0", "1.2.0"))
	require.NoError(t, service.Acknowledge("1.3.0"))

	assert.False(t, service.Pending("1.2.0", settings.ChannelStable).Show)
}

// Dev builds are cut close to daily, so they get the toast rather than a modal
// on nearly every launch.
func TestPendingUsesAToastOnTheDevChannel(t *testing.T) {
	service := newTestReleaseNotes(t, testEntries("1.2.0-dev.4", "1.2.0-dev.3"))
	require.NoError(t, service.Acknowledge("1.2.0-dev.3"))

	pending := service.Pending("1.2.0-dev.4", settings.ChannelDev)
	require.True(t, pending.Show)
	assert.Equal(t, PresentationToast, pending.Presentation)
}

// An upgrade with no entry in range still reports the version bump — a modal
// with an empty body would say less than the one-line toast does.
func TestPendingDegradesToAToastWhenNoEntriesExist(t *testing.T) {
	service := newTestReleaseNotes(t, testEntries("1.2.0"))
	require.NoError(t, service.Acknowledge("1.2.0"))

	pending := service.Pending("1.4.0", settings.ChannelStable)
	require.True(t, pending.Show)
	assert.Equal(t, PresentationToast, pending.Presentation)
	assert.Empty(t, pending.Entries)
	assert.Equal(t, "1.4.0", pending.Version)
}

func TestPendingCollectsEverySkippedRelease(t *testing.T) {
	service := newTestReleaseNotes(t, testEntries("1.2.0-dev.4", "1.2.0-dev.3", "1.2.0-dev.2"))
	require.NoError(t, service.Acknowledge("1.2.0-dev.2"))

	pending := service.Pending("1.2.0-dev.4", settings.ChannelDev)
	require.Len(t, pending.Entries, 2, "both crossed releases are reported")
	assert.Equal(t, "1.2.0-dev.4", pending.Entries[0].Version)
	assert.Equal(t, "1.2.0-dev.3", pending.Entries[1].Version)
}

func TestHistoryIsScopedToTheChannel(t *testing.T) {
	service := newTestReleaseNotes(t, testEntries("1.3.0", "1.3.0-beta.1", "1.2.1-dev.1"))

	assert.Len(t, service.History(settings.ChannelStable), 1)
	assert.Len(t, service.History(settings.ChannelBeta), 2)
	assert.Len(t, service.History(settings.ChannelDev), 3)
}

// The constructor degrades rather than failing when the changelog cannot be
// parsed, because release notes are not worth refusing to launch over.
func TestNewReleaseNotesServiceLoadsTheEmbeddedChangelog(t *testing.T) {
	service := NewReleaseNotesService(settings.Paths{StateDir: t.TempDir()}, zerolog.Nop())

	assert.NotEmpty(t, service.History(settings.ChannelDev))
}
