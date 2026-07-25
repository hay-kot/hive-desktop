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

	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/sources/github/feed"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/hivecore/github"
)

func TestSettingsServiceSetGithubSettingsRejectsBelowFloor(t *testing.T) {
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	service := newSettingsService(nil, nil)

	err := service.SetGithub(t.Context(), GithubSettings{PollInterval: settings.MinPollInterval - time.Second})
	require.Error(t, err)
	require.Equal(t, KindInvalid, KindOf(err), "a caller below the floor is asking for something invalid, not hitting a fault")
}

func TestSettingsServiceNotificationSettings(t *testing.T) {
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	service := newSettingsService(nil, nil)

	got, err := service.Notifications(t.Context())
	require.NoError(t, err)
	require.Equal(t, NotificationSettings{Enabled: true, Delivery: settings.DeliveryAuto, Sound: true}, got)
}

func TestSettingsServiceSetNotificationSettingsHealsUnknownDelivery(t *testing.T) {
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	service := newSettingsService(nil, nil)

	require.NoError(t, service.SetNotifications(t.Context(), NotificationSettings{
		Enabled: true, Delivery: "banner", Sound: true,
	}))

	got, err := settings.LoadSettings()
	require.NoError(t, err)
	require.Equal(t, settings.DeliveryAuto, got.NotificationDelivery)
}

func TestSettingsServiceSetNotificationSettingsPreservesUnrelatedFields(t *testing.T) {
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	autoUpdate := false
	require.NoError(t, settings.SaveSettings(settings.Settings{
		PollInterval: "5m",
		AutoUpdate:   &autoUpdate,
	}))

	service := newSettingsService(nil, nil)
	want := NotificationSettings{Enabled: false, Delivery: settings.DeliveryApp, Sound: false}
	require.NoError(t, service.SetNotifications(t.Context(), want))

	got, err := settings.LoadSettings()
	require.NoError(t, err)
	require.Equal(t, "5m", got.PollInterval)
	require.NotNil(t, got.AutoUpdate)
	require.False(t, *got.AutoUpdate)
	require.NotNil(t, got.NotificationsEnabled)
	require.NotNil(t, got.NotificationSound)
	require.False(t, *got.NotificationsEnabled)
	require.Equal(t, settings.DeliveryApp, got.NotificationDelivery)
	require.False(t, *got.NotificationSound)
}

func TestSettingsServiceSetGithubSettingsPreservesAutoUpdate(t *testing.T) {
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	// Seed an explicit auto_update:false alongside a poll interval.
	disabled := false
	require.NoError(t, settings.SaveSettings(settings.Settings{PollInterval: "5m", AutoUpdate: &disabled}))

	service := newSettingsService(nil, nil)
	require.NoError(t, service.SetGithub(t.Context(), GithubSettings{PollInterval: 2 * time.Minute}))

	got, err := settings.LoadSettings()
	require.NoError(t, err)
	require.Equal(t, "2m0s", got.PollInterval)
	require.NotNil(t, got.AutoUpdate, "auto_update must survive a poll-interval save")
	require.False(t, *got.AutoUpdate)
}

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
		t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
		provider := feed.NewLiveProvider(github.NewClient(), nil, zerolog.Nop())
		db, err := store.Open(t.Context(), t.TempDir(), store.DefaultOpenOptions())
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		source := &settingsServiceSource{}
		producer := ingest.NewProducer(db, func(context.Context) (map[string]ingest.Source, error) {
			return map[string]ingest.Source{"github": source}, nil
		}, time.Hour, nil, zerolog.Nop())
		service := newSettingsService(producer, provider)

		require.NoError(t, service.SetGithub(t.Context(), GithubSettings{PollInterval: 2 * time.Minute}))
		saved, err := settings.LoadSettings()
		require.NoError(t, err)
		require.Equal(t, "2m0s", saved.PollInterval)

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
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	service := newSettingsService(nil, nil)

	got, err := service.Theme(t.Context())
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestSettingsServiceSetAppearanceSettingsPreservesUnrelatedFields(t *testing.T) {
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	autoUpdate := false
	require.NoError(t, settings.SaveSettings(settings.Settings{
		PollInterval: "5m",
		AutoUpdate:   &autoUpdate,
	}))

	service := newSettingsService(nil, nil)
	require.NoError(t, service.SetTheme(t.Context(), "midnight"))

	got, err := settings.LoadSettings()
	require.NoError(t, err)
	require.Equal(t, "midnight", got.Appearance.Theme)
	require.Equal(t, "5m", got.PollInterval)
	require.NotNil(t, got.AutoUpdate)
	require.False(t, *got.AutoUpdate)

	roundTripped, err := service.Theme(t.Context())
	require.NoError(t, err)
	require.Equal(t, "midnight", roundTripped)
}
