package wailsui

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
	service := NewSettingsService(nil, nil, zerolog.Nop())

	err := service.SetGithubSettings(GithubSettings{PollIntervalSeconds: int(settings.MinPollInterval/time.Second) - 1})
	require.Error(t, err)
}

func TestSettingsServiceNotificationSettings(t *testing.T) {
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	service := NewSettingsService(nil, nil, zerolog.Nop())

	got, err := service.NotificationSettings()
	require.NoError(t, err)
	require.Equal(t, NotificationSettings{
		NotificationsEnabled: true,
		Delivery:             settings.DeliveryAuto,
		NotificationSound:    true,
	}, got)
}

func TestSettingsServiceSetNotificationSettingsHealsUnknownDelivery(t *testing.T) {
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	service := NewSettingsService(nil, nil, zerolog.Nop())

	require.NoError(t, service.SetNotificationSettings(NotificationSettings{
		NotificationsEnabled: true,
		Delivery:             "banner",
		NotificationSound:    true,
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

	service := NewSettingsService(nil, nil, zerolog.Nop())
	want := NotificationSettings{
		NotificationsEnabled: false,
		Delivery:             settings.DeliveryApp,
		NotificationSound:    false,
	}
	require.NoError(t, service.SetNotificationSettings(want))

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

	service := NewSettingsService(nil, nil, zerolog.Nop())
	require.NoError(t, service.SetGithubSettings(GithubSettings{PollIntervalSeconds: 120}))

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
		db, err := store.Open(t.TempDir(), store.DefaultOpenOptions())
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		source := &settingsServiceSource{}
		producer := ingest.NewProducer(db, func(context.Context) (map[string]ingest.Source, error) {
			return map[string]ingest.Source{"github": source}, nil
		}, time.Hour, nil, zerolog.Nop())
		service := NewSettingsService(producer, provider, zerolog.Nop())

		require.NoError(t, service.SetGithubSettings(GithubSettings{PollIntervalSeconds: 120}))
		settings, err := settings.LoadSettings()
		require.NoError(t, err)
		require.Equal(t, "2m0s", settings.PollInterval)

		got, err := service.GithubSettings()
		require.NoError(t, err)
		require.Equal(t, 120, got.PollIntervalSeconds)
		require.Equal(t, 60, got.MinPollIntervalSeconds)

		producer.Start(t.Context())
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		producer.Stop()
		require.Equal(t, 1, source.callCount(), "saved settings reset the live producer cadence")
	})
}

func TestSettingsServiceAppearanceSettingsDefaultsToUnset(t *testing.T) {
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	service := NewSettingsService(nil, nil, zerolog.Nop())

	got, err := service.AppearanceSettings()
	require.NoError(t, err)
	require.Equal(t, AppearanceSettings{}, got)
}

func TestSettingsServiceSetAppearanceSettingsPreservesUnrelatedFields(t *testing.T) {
	t.Setenv(settings.EnvConfigPath, filepath.Join(t.TempDir(), "config", "profiles.yaml"))
	autoUpdate := false
	require.NoError(t, settings.SaveSettings(settings.Settings{
		PollInterval: "5m",
		AutoUpdate:   &autoUpdate,
	}))

	service := NewSettingsService(nil, nil, zerolog.Nop())
	require.NoError(t, service.SetAppearanceSettings(AppearanceSettings{Theme: "midnight"}))

	got, err := settings.LoadSettings()
	require.NoError(t, err)
	require.Equal(t, "midnight", got.Appearance.Theme)
	require.Equal(t, "5m", got.PollInterval)
	require.NotNil(t, got.AutoUpdate)
	require.False(t, *got.AutoUpdate)

	roundTripped, err := service.AppearanceSettings()
	require.NoError(t, err)
	require.Equal(t, AppearanceSettings{Theme: "midnight"}, roundTripped)
}
