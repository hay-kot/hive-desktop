package wailsui

import (
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

func TestTerminalServiceSetStateSwapsEnabledAndEndpoint(t *testing.T) {
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, "feed")
	t.Setenv(settings.EnvHTTPPort, "0")
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))

	core, err := app.New(t.Context(), app.Config{MockMode: settings.MockMode(), Logger: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })
	require.NoError(t, core.Start(t.Context()))

	service := NewTerminalService(core.Terminals, core.Webhooks, TerminalTransport{Token: "first", StreamPath: "/first"}, true)
	require.True(t, service.Enabled(t.Context()))
	endpoint, err := service.Endpoint(t.Context())
	require.NoError(t, err)
	require.Equal(t, "first", endpoint.Token)
	require.Contains(t, endpoint.WSURL, "/first")

	service.SetState(false, TerminalTransport{})
	require.False(t, service.Enabled(t.Context()))
	_, err = service.Endpoint(t.Context())
	require.Equal(t, app.KindUnavailable, app.KindOf(err))

	service.SetState(true, TerminalTransport{Token: "second", StreamPath: "/second"})
	require.True(t, service.Enabled(t.Context()))
	endpoint, err = service.Endpoint(t.Context())
	require.NoError(t, err)
	require.Equal(t, "second", endpoint.Token)
	require.Contains(t, endpoint.WSURL, "/second")
}
