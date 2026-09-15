package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/configstate"
	"github.com/hay-kot/hive-desktop/internal/app/configwatch"
	"github.com/hay-kot/hive-desktop/internal/app/events"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

func newConfigurationTestApp(t *testing.T, ctx context.Context) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvHiveDataDir, filepath.Join(root, "hive"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, settings.MockFeed)

	core, err := New(ctx, Config{
		Settings: settings.DefaultSettings(),
		MockMode: settings.MockFeed,
		Logger:   zerolog.Nop(),
	})
	require.NoError(t, err)
	//nolint:contextcheck // Close owns its bounded shutdown context.
	t.Cleanup(func() { _ = core.Close() })
	return core
}

func TestConfigurationUsesAppLifetimeAfterStartContextCancellation(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(t.Context())
	t.Cleanup(cancelApp)
	core := newConfigurationTestApp(t, appCtx)

	updates := make(chan events.ActionsUpdated, 4)
	cancelSubscription := events.Subscribe(t.Context(), core.Events, "test.configuration-lifetime", events.Buffer(4), func(_ context.Context, event events.ActionsUpdated) {
		updates <- event
	})
	t.Cleanup(cancelSubscription)

	startCtx, cancelStart := context.WithCancel(t.Context())
	require.NoError(t, core.Start(startCtx))
	select {
	case <-updates:
	case <-time.After(5 * time.Second):
		t.Fatal("startup configuration pass did not publish actions")
	}

	cancelStart()
	require.NoError(t, appCtx.Err())
	require.NoError(t, os.WriteFile(core.paths.ActionsPath, []byte("invalid: true\n"), 0o600))
	select {
	case <-updates:
	case <-time.After(5 * time.Second):
		t.Fatal("configuration stopped after Start context cancellation")
	}

	require.NoError(t, core.Close())
	select {
	case <-core.configuration.done:
	case <-time.After(time.Second):
		t.Fatal("configuration reconciliation loop did not stop")
	}
	_, err := core.configuration.manager.Mark(t.Context(), configstate.Actions, configstate.AppWrite)
	require.ErrorIs(t, err, configwatch.ErrStopped)
	require.ErrorIs(t, core.Start(t.Context()), errAppClosed)
}

func TestConfigurationReconcileOrdersDomainsBeforePublishing(t *testing.T) {
	core := newConfigurationTestApp(t, t.Context())
	configuration := core.configuration

	var mu sync.Mutex
	sequence := make([]string, 0, 7)
	record := func(value string) {
		mu.Lock()
		defer mu.Unlock()
		sequence = append(sequence, value)
	}
	configuration.applySource = func(_ context.Context, source configstate.Source) error {
		record(string(source))
		if source == configstate.Actions {
			return errors.New("actions reload failed")
		}
		return nil
	}
	configuration.publish = func(context.Context, events.Event) { record("publish") }

	batch := configwatch.Batch{ID: 1, Dirty: []configwatch.Dirty{
		{Source: configstate.AgentWorkspaces, Generation: 1},
		{Source: configstate.Flows, Generation: 1},
		{Source: configstate.Actions, Generation: 1},
		{Source: configstate.Settings, Generation: 1},
	}}
	processed := configuration.reconcile(t.Context(), batch)

	require.Equal(t, []configstate.Source{
		configstate.Settings,
		configstate.Actions,
		configstate.Flows,
		configstate.AgentWorkspaces,
	}, []configstate.Source{processed.Dirty[0].Source, processed.Dirty[1].Source, processed.Dirty[2].Source, processed.Dirty[3].Source})
	require.Equal(t, []string{
		"settings",
		"actions",
		"flows",
		"agent_workspaces",
		"publish",
		"publish",
		"publish",
	}, sequence)
}
