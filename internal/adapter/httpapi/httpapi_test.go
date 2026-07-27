package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
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
	return core, httpapi.New(core, zerolog.Nop()).Handler()
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

func do(t *testing.T, handler http.Handler, method, target string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(method, target, r))
	return rec
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 48, 32))
	for y := range 32 {
		for x := range 48 {
			img.Set(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 7), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// TestServedOverWebhookListener exercises main.go's real path: MountAPI onto the
// webhook listener, Start binds one loopback port, and the API answers over TCP
// on it — the shared-port design end to end.
func TestServedOverWebhookListener(t *testing.T) {
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, "feed")
	t.Setenv(settings.EnvHTTPEnabled, "true")
	t.Setenv(settings.EnvHTTPPort, "0")

	cfg, err := settings.NewStore(filepath.Join(root, "config", "settings.yaml")).Effective()
	require.NoError(t, err)
	core, err := app.New(t.Context(), app.Config{Settings: cfg, MockMode: cfg.MockMode(), Logger: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = core.Close() })

	require.True(t, core.MountAPI(httpapi.PathPrefix, httpapi.New(core, zerolog.Nop()).Handler()),
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

func TestFindByExternalID(t *testing.T) {
	core, handler := testServer(t)
	seedItem(t, core, "p1", "PR_1", `{"repo":"acme/widgets","num":7}`)
	seedItem(t, core, "p2", "PR_2", `{}`)

	var listed struct {
		Items []store.InboxItemView `json:"items"`
	}
	rec := get(t, handler, "/api/inbox?externalId=PR_1")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed.Items, 1)
	assert.Equal(t, "PR_1", listed.Items[0].ExternalID)
}

func TestEventsResolution(t *testing.T) {
	core, handler := testServer(t)
	id := seedItem(t, core, "p1", "PR_1", `{}`)
	seedItem(t, core, "p2", "PR_1", `{}`) // same external id, second profile

	assert.Equal(t, http.StatusUnprocessableEntity, get(t, handler, "/api/inbox/events").Code,
		"itemId or externalId is required")
	assert.Equal(t, http.StatusOK, get(t, handler, "/api/inbox/events?itemId="+strconv.FormatInt(id, 10)).Code)
	assert.Equal(t, http.StatusConflict, get(t, handler, "/api/inbox/events?externalId=PR_1").Code,
		"one external id matched two items")
}

// TestProfileRequired covers the sibling-endpoint validation, including that a
// feed-scoped inbox read demands a profile rather than silently returning empty.
func TestProfileRequired(t *testing.T) {
	_, handler := testServer(t)

	rec := get(t, handler, "/api/feeds")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body struct {
		Kind   string            `json:"kind"`
		Fields map[string]string `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "invalid", body.Kind)
	assert.Contains(t, body.Fields, "profile")

	assert.Equal(t, http.StatusOK, get(t, handler, "/api/feeds?profile=p1").Code)
	assert.Equal(t, http.StatusUnprocessableEntity, get(t, handler, "/api/inbox?feed=team").Code)
	assert.Equal(t, http.StatusOK, get(t, handler, "/api/inbox?feed=team&profile=p1").Code)
}

func TestProfileImageLifecycleOverHTTP(t *testing.T) {
	core, handler := testServer(t)
	created, err := core.Flows.Create(t.Context(), "Agent Profile")
	require.NoError(t, err)

	// PUT the raw image bytes; the core normalizes and stores.
	put := do(t, handler, http.MethodPut, "/api/profiles/"+created.ID+"/image", testPNG(t))
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	var view struct {
		ID       string `json:"id"`
		HasImage bool   `json:"hasImage"`
	}
	require.NoError(t, json.Unmarshal(put.Body.Bytes(), &view))
	assert.Equal(t, created.ID, view.ID)
	assert.True(t, view.HasImage)

	// GET returns a 128px-square PNG.
	img := get(t, handler, "/api/profiles/"+created.ID+"/image")
	require.Equal(t, http.StatusOK, img.Code)
	assert.Equal(t, "image/png", img.Header().Get("Content-Type"))
	decoded, err := png.Decode(bytes.NewReader(img.Body.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, 128, 128), decoded.Bounds())

	// The listing reports it.
	list := get(t, handler, "/api/profiles")
	require.Equal(t, http.StatusOK, list.Code)
	var listed struct {
		Profiles []struct {
			ID       string `json:"id"`
			HasImage bool   `json:"hasImage"`
		} `json:"profiles"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &listed))
	found := false
	for _, p := range listed.Profiles {
		if p.ID == created.ID {
			found = true
			assert.True(t, p.HasImage)
		}
	}
	assert.True(t, found, "the created profile appears in the listing")

	// DELETE clears it; the image then reads as absent.
	del := do(t, handler, http.MethodDelete, "/api/profiles/"+created.ID+"/image", nil)
	require.Equal(t, http.StatusOK, del.Code)
	assert.Equal(t, http.StatusNotFound, get(t, handler, "/api/profiles/"+created.ID+"/image").Code)
}

func TestProfileImageRejectsBadRequests(t *testing.T) {
	core, handler := testServer(t)
	created, err := core.Flows.Create(t.Context(), "Agent Profile")
	require.NoError(t, err)

	bad := do(t, handler, http.MethodPut, "/api/profiles/"+created.ID+"/image", []byte("not an image"))
	assert.Equal(t, http.StatusBadRequest, bad.Code, "an undecodable body is a 400")

	missing := do(t, handler, http.MethodPut, "/api/profiles/does-not-exist/image", testPNG(t))
	assert.Equal(t, http.StatusNotFound, missing.Code, "an unknown profile is a 404")
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
