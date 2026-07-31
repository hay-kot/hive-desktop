package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/adapter/wailsui"
	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

func TestApplyLoopbackMountsRebuildsTerminalRoutes(t *testing.T) {
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, "feed")
	t.Setenv(settings.EnvHTTPPort, "0")
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))

	core, err := app.New(t.Context(), app.Config{MockMode: settings.MockMode(), Logger: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })

	cfg := settings.DefaultSettings()
	disabled, transport := applyLoopbackMounts(core, zerolog.Nop(), cfg)
	require.False(t, disabled)
	require.Empty(t, transport.Token)

	transports := make(chan wailsui.TerminalTransport, 3)
	core.SetRebuildMounts(func(next settings.Settings) {
		_, transport := applyLoopbackMounts(core, zerolog.Nop(), next)
		transports <- transport
	})
	require.NoError(t, core.Start(t.Context()))

	running, _ := core.Webhooks.Endpoint(t.Context())
	require.True(t, running)
	assertStatus := func(path string, want int) {
		t.Helper()
		require.Eventually(t, func() bool {
			running, port := core.Webhooks.Endpoint(t.Context())
			if !running {
				return false
			}
			response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
			if err != nil {
				return false
			}
			defer func() { _ = response.Body.Close() }()
			return response.StatusCode == want
		}, 5*time.Second, 10*time.Millisecond)
	}
	assertStatus("/api/", http.StatusOK)
	assertStatus("/api/terminal/stream", http.StatusNotFound)

	_, err = core.Settings.SetExperimentalTerminal(t.Context(), true)
	require.NoError(t, err)
	first := <-transports
	require.NotEmpty(t, first.Token)
	require.NotEmpty(t, first.StreamPath)
	assertStatus("/api/", http.StatusOK)
	assertStatus("/api/terminal/stream", http.StatusBadRequest)

	_, err = core.Settings.SetExperimentalTerminal(t.Context(), false)
	require.NoError(t, err)
	transport = <-transports
	require.Empty(t, transport.Token)
	assertStatus("/api/", http.StatusOK)
	assertStatus("/api/terminal/stream", http.StatusNotFound)

	_, err = core.Settings.SetExperimentalTerminal(t.Context(), true)
	require.NoError(t, err)
	second := <-transports
	require.NotEmpty(t, second.Token)
	require.NotEqual(t, first.Token, second.Token)
	assertStatus("/api/", http.StatusOK)
	assertStatus("/api/terminal/stream", http.StatusBadRequest)
}
