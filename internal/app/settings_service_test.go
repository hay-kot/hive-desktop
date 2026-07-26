package app

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/hivecore/github"
)

func TestSettingsServiceSetGithubSettingsRejectsBelowFloor(t *testing.T) {
	t.Setenv(settings.EnvConfigDir, filepath.Join(t.TempDir(), "config"))
	service := newSettingsService(settings.NewStore(settings.SettingsPath()), nil, nil)

	err := service.SetGithub(t.Context(), GithubSettings{PollInterval: settings.MinPollInterval - time.Second})
	require.Error(t, err)
	require.Equal(t, KindInvalid, KindOf(err), "a caller below the floor is asking for something invalid, not hitting a fault")
}

func TestSettingsServiceNotificationSettings(t *testing.T) {
	t.Setenv(settings.EnvConfigDir, filepath.Join(t.TempDir(), "config"))
	service := newSettingsService(settings.NewStore(settings.SettingsPath()), nil, nil)

	got, err := service.Notifications(t.Context())
	require.NoError(t, err)
	require.Equal(t, NotificationSettings{Enabled: true, Delivery: settings.DeliveryAuto, Sound: true}, got)
}

func TestSettingsServiceSetNotificationSettingsHealsUnknownDelivery(t *testing.T) {
	t.Setenv(settings.EnvConfigDir, filepath.Join(t.TempDir(), "config"))
	service := newSettingsService(settings.NewStore(settings.SettingsPath()), nil, nil)

	require.NoError(t, service.SetNotifications(t.Context(), NotificationSettings{
		Enabled: true, Delivery: "banner", Sound: true,
	}))

	got, err := settings.LoadSettings()
	require.NoError(t, err)
	require.Equal(t, settings.DeliveryAuto, got.Notifications.Delivery)
}

func TestSettingsServiceSetNotificationSettingsPreservesUnrelatedFields(t *testing.T) {
	t.Setenv(settings.EnvConfigDir, filepath.Join(t.TempDir(), "config"))
	cfg := settings.DefaultSettings()
	cfg.Updates.Enabled = false
	require.NoError(t, settings.SaveSettings(cfg))

	service := newSettingsService(settings.NewStore(settings.SettingsPath()), nil, nil)
	want := NotificationSettings{Enabled: false, Delivery: settings.DeliveryApp, Sound: false}
	require.NoError(t, service.SetNotifications(t.Context(), want))

	got, err := settings.LoadSettings()
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, got.Polling.Interval.Duration())
	require.False(t, got.Updates.Enabled)
	require.False(t, got.Notifications.Enabled)
	require.Equal(t, settings.DeliveryApp, got.Notifications.Delivery)
	require.False(t, got.Notifications.Sound)
}

func TestSettingsServiceSetGithubSettingsPreservesAutoUpdate(t *testing.T) {
	t.Setenv(settings.EnvConfigDir, filepath.Join(t.TempDir(), "config"))
	// Seed an explicit updates.enabled:false alongside a poll interval.
	cfg := settings.DefaultSettings()
	cfg.Updates.Enabled = false
	require.NoError(t, settings.SaveSettings(cfg))

	service := newSettingsService(settings.NewStore(settings.SettingsPath()), nil, nil)
	require.NoError(t, service.SetGithub(t.Context(), GithubSettings{PollInterval: 2 * time.Minute}))

	got, err := settings.LoadSettings()
	require.NoError(t, err)
	require.Equal(t, 2*time.Minute, got.Polling.Interval.Duration())
	require.False(t, got.Updates.Enabled, "updates.enabled must survive a poll-interval save")
}

// settingsServiceSources is the producer's Sources seam: one pull instance
// over the counting source below, so the test can assert a re-tick happened
// without standing up the connector registry.
type settingsServiceSources struct{ source connector.PullSource }

func (s settingsServiceSources) PullInstances() []connector.Instance {
	return []connector.Instance{{
		Type:     "sources.test",
		Node:     connector.Node{FlowID: "profile", NodeID: "github"},
		Metadata: connector.Metadata{ProfileID: "profile", SourceKind: "generic"},
		Pull:     s.source,
	}}
}

func (settingsServiceSources) Prefetch(context.Context, []connector.Instance) error { return nil }

type settingsServiceSource struct {
	mu    sync.Mutex
	calls int
}

func (s *settingsServiceSource) Produce(_ context.Context, _ func(ingest.Msg) error) error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return nil
}

func (s *settingsServiceSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestSettingsServiceSetGithubSettingsPersistsAndApplies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		t.Setenv(settings.EnvConfigDir, filepath.Join(t.TempDir(), "config"))
		fetchers := ghsource.NewFetchers(github.NewClient(), credentials.NewMemoryStore(), zerolog.Nop())
		db, err := store.Open(t.Context(), t.TempDir(), store.DefaultOpenOptions())
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		source := &settingsServiceSource{}
		producer := ingest.NewProducer(db, settingsServiceSources{source}, time.Hour, nil, zerolog.Nop())
		service := newSettingsService(settings.NewStore(settings.SettingsPath()), producer, fetchers)

		require.NoError(t, service.SetGithub(t.Context(), GithubSettings{PollInterval: 2 * time.Minute}))
		saved, err := settings.LoadSettings()
		require.NoError(t, err)
		require.Equal(t, 2*time.Minute, saved.Polling.Interval.Duration())

		got, err := service.Github(t.Context())
		require.NoError(t, err)
		require.Equal(t, 2*time.Minute, got.PollInterval)
		require.Equal(t, settings.MinPollInterval, got.MinPollInterval)

		producer.Start(t.Context())
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		producer.Stop()
		require.Equal(t, 1, source.callCount(), "saved settings reset the live producer cadence")
	})
}

func TestSettingsServiceAppearanceSettingsDefaultsToUnset(t *testing.T) {
	t.Setenv(settings.EnvConfigDir, filepath.Join(t.TempDir(), "config"))
	service := newSettingsService(settings.NewStore(settings.SettingsPath()), nil, nil)

	got, err := service.Theme(t.Context())
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestSettingsServiceSetAppearanceSettingsPreservesUnrelatedFields(t *testing.T) {
	t.Setenv(settings.EnvConfigDir, filepath.Join(t.TempDir(), "config"))
	cfg := settings.DefaultSettings()
	cfg.Updates.Enabled = false
	require.NoError(t, settings.SaveSettings(cfg))

	service := newSettingsService(settings.NewStore(settings.SettingsPath()), nil, nil)
	require.NoError(t, service.SetTheme(t.Context(), "midnight"))

	got, err := settings.LoadSettings()
	require.NoError(t, err)
	require.Equal(t, "midnight", got.Appearance.Theme)
	require.Equal(t, 5*time.Minute, got.Polling.Interval.Duration())
	require.False(t, got.Updates.Enabled)

	roundTripped, err := service.Theme(t.Context())
	require.NoError(t, err)
	require.Equal(t, "midnight", roundTripped)
}
