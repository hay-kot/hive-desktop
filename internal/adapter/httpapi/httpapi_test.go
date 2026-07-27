package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/adapter/httpapi"
	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

func testServer(t *testing.T) (*app.App, http.Handler) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, "feed")

	core, err := app.New(t.Context(), app.Config{
		Settings: settings.DefaultSettings(),
		MockMode: settings.MockMode(),
		Logger:   zerolog.Nop(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })
	return core, httpapi.New(core).Handler()
}

func seedItem(t *testing.T, core *app.App, profile, external, payload string) int64 {
	t.Helper()
	item, err := core.Store.Queries().InsertInboxItem(t.Context(), store.InsertInboxItemParams{
		ProfileID: profile, SourceKind: "github", SourceScope: "s", ExternalID: external,
		Payload: []byte(payload), Lifecycle: "active",
	})
	require.NoError(t, err)
	return item.ID
}

func get(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestListAndFindInbox(t *testing.T) {
	core, handler := testServer(t)
	seedItem(t, core, "p1", "PR_1", `{"repo":"o/r","num":7,"reason":"review_requested","state":"open"}`)
	seedItem(t, core, "p2", "PR_2", `{"repo":"o/r","num":8}`)

	var listed struct {
		Items []store.InboxItemView `json:"items"`
	}
	rec := get(t, handler, "/api/inbox")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	assert.Len(t, listed.Items, 2)

	rec = get(t, handler, "/api/inbox?externalId=PR_1")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed.Items, 1)
	assert.Equal(t, "PR_1", listed.Items[0].ExternalID)
}

func TestItemEventsResolvesByItemID(t *testing.T) {
	core, handler := testServer(t)
	id := seedItem(t, core, "p1", "PR_1", `{}`)

	rec := get(t, handler, "/api/inbox/events?itemId="+strconv.FormatInt(id, 10))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = get(t, handler, "/api/inbox/events")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "itemId or externalId is required")
}

func TestEventsAmbiguousExternalIDIsConflict(t *testing.T) {
	core, handler := testServer(t)
	seedItem(t, core, "p1", "PR_1", `{}`)
	seedItem(t, core, "p2", "PR_1", `{}`)

	rec := get(t, handler, "/api/inbox/events?externalId=PR_1")
	assert.Equal(t, http.StatusConflict, rec.Code, "one external id matched two items")
}

func TestFeedsRequiresProfile(t *testing.T) {
	_, handler := testServer(t)
	assert.Equal(t, http.StatusBadRequest, get(t, handler, "/api/feeds").Code)
	assert.Equal(t, http.StatusOK, get(t, handler, "/api/feeds?profile=p1").Code)
}

func TestRefreshUnavailableInMockMode(t *testing.T) {
	_, handler := testServer(t)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sources/refresh", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "mock mode has no producer")

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, string(app.KindUnavailable), body["kind"], "the Kind reaches the wire")
}

// TestServedOverWebhookListener exercises main.go's real path: MountAPI onto the
// webhook listener, Start binds one loopback port, and /api/ is reachable over
// TCP on it — the shared-port design end to end.
func TestServedOverWebhookListener(t *testing.T) {
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, "feed")
	// Enabling webhooks + a port override is what lifts the mock-mode guard and
	// binds the listener the API rides.
	t.Setenv(settings.EnvWebhookEnabled, "true")
	t.Setenv(settings.EnvWebhookPort, "0")

	cfg, err := settings.NewStore(filepath.Join(root, "config", "settings.yaml")).Effective()
	require.NoError(t, err)
	core, err := app.New(t.Context(), app.Config{Settings: cfg, MockMode: cfg.MockMode(), Logger: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })

	require.True(t, core.MountAPI(httpapi.PathPrefix, httpapi.New(core).Handler()),
		"the webhook listener exists, so the API mounts")
	seedItem(t, core, "p1", "PR_1", `{"repo":"acme/widgets","num":7}`)
	require.NoError(t, core.Start(t.Context()))

	running, port := core.Webhooks.Endpoint(t.Context())
	require.True(t, running)
	require.NotZero(t, port)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	resp, err := http.Get(base + "/api/status") //nolint:noctx // loopback test
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck // test
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the API is reachable over the webhook port")

	var listed struct {
		Items []store.InboxItemView `json:"items"`
	}
	itemsResp, err := http.Get(base + "/api/inbox") //nolint:noctx // loopback test
	require.NoError(t, err)
	defer itemsResp.Body.Close() //nolint:errcheck // test
	require.NoError(t, json.NewDecoder(itemsResp.Body).Decode(&listed))
	require.Len(t, listed.Items, 1)
	assert.Equal(t, "PR_1", listed.Items[0].ExternalID)
}

func TestStatusAndHelp(t *testing.T) {
	_, handler := testServer(t)

	var status struct {
		Webhook struct {
			Running    bool   `json:"running"`
			PathPrefix string `json:"pathPrefix"`
		} `json:"webhook"`
		API struct {
			PathPrefix string `json:"pathPrefix"`
		} `json:"api"`
	}
	rec := get(t, handler, "/api/status")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &status))
	assert.False(t, status.Webhook.Running, "mock mode binds no webhook listener")
	assert.Equal(t, "/hooks/", status.Webhook.PathPrefix)
	assert.Equal(t, "/api/", status.API.PathPrefix)

	assert.Equal(t, http.StatusOK, get(t, handler, "/api/help").Code)
}
