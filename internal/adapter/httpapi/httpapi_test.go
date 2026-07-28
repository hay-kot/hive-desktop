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
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/adapter/httpapi"
	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
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

	// The route index sits at the mount root. Through the listener a request for
	// /api is redirected to /api/, so it must be served there — the in-process
	// handler test cannot catch this because it has no mount prefix in front.
	for _, target := range []string{base + "/api/", base + "/api"} {
		idxResp, err := http.Get(target) //nolint:noctx // loopback test
		require.NoError(t, err)
		body, _ := io.ReadAll(idxResp.Body)
		idxResp.Body.Close() //nolint:errcheck // test
		require.Equalf(t, http.StatusOK, idxResp.StatusCode, "the route index is reachable at %s", target)
		assert.Containsf(t, string(body), `"routes"`, "%s returns the index", target)
	}

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

func TestProfileCreateAndDeleteOverHTTP(t *testing.T) {
	_, handler := testServer(t)

	create := do(t, handler, http.MethodPost, "/api/profiles", []byte(`{"name":"Agent Made"}`))
	require.Equal(t, http.StatusCreated, create.Code, create.Body.String())
	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(create.Body.Bytes(), &created))
	assert.Equal(t, "Agent Made", created.Name)
	require.NotEmpty(t, created.ID)

	assert.Contains(t, get(t, handler, "/api/profiles").Body.String(), `"id":"`+created.ID+`"`)

	bad := do(t, handler, http.MethodPost, "/api/profiles", []byte(`{"name":"  "}`))
	assert.Equal(t, http.StatusUnprocessableEntity, bad.Code, "an empty name is rejected")

	del := do(t, handler, http.MethodDelete, "/api/profiles/"+created.ID, nil)
	assert.Equal(t, http.StatusNoContent, del.Code)
	assert.NotContains(t, get(t, handler, "/api/profiles").Body.String(), `"id":"`+created.ID+`"`)
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

// webhookHTTPFlow is a minimal flow with one webhook source wired to a feed, so
// the node-image endpoints have a real sources.webhook node to target.
func webhookHTTPFlow() flow.Flow {
	return flow.Flow{
		ID: "hooks", Name: "Hooks", Enabled: true,
		Nodes: []flow.Node{
			{ID: "hook", Type: "sources.webhook", Config: flow.NewSourceConfig(webhook.Descriptor.Type, &webhook.Config{Path: "ci"})},
			{ID: "inbox", Type: "feed", Name: "Inbox", Config: &flow.FeedConfig{}},
		},
		Wires: []flow.Wire{{From: "hook", To: "inbox"}},
	}
}

func TestNodeImageLifecycleOverHTTP(t *testing.T) {
	core, handler := testServer(t)
	require.NoError(t, core.Flows.Save(t.Context(), webhookHTTPFlow()))

	base := "/api/flows/hooks/nodes/hook/image"

	// PUT stores the bytes and records the hash on the node in one call.
	put := do(t, handler, http.MethodPut, base, testPNG(t))
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	var view struct {
		FlowID   string `json:"flowId"`
		NodeID   string `json:"nodeId"`
		HasImage bool   `json:"hasImage"`
	}
	require.NoError(t, json.Unmarshal(put.Body.Bytes(), &view))
	assert.Equal(t, "hooks", view.FlowID)
	assert.Equal(t, "hook", view.NodeID)
	assert.True(t, view.HasImage)

	// GET returns a 128px-square PNG.
	img := get(t, handler, base)
	require.Equal(t, http.StatusOK, img.Code)
	assert.Equal(t, "image/png", img.Header().Get("Content-Type"))
	decoded, err := png.Decode(bytes.NewReader(img.Body.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, 128, 128), decoded.Bounds())

	// DELETE clears it; the image then reads as absent.
	del := do(t, handler, http.MethodDelete, base, nil)
	require.Equal(t, http.StatusOK, del.Code)
	assert.Equal(t, http.StatusNotFound, get(t, handler, base).Code)
}

func TestNodeImageRejectsBadRequests(t *testing.T) {
	core, handler := testServer(t)
	require.NoError(t, core.Flows.Save(t.Context(), webhookHTTPFlow()))

	bad := do(t, handler, http.MethodPut, "/api/flows/hooks/nodes/hook/image", []byte("not an image"))
	assert.Equal(t, http.StatusBadRequest, bad.Code, "an undecodable body is a 400")

	missingFlow := do(t, handler, http.MethodPut, "/api/flows/none/nodes/hook/image", testPNG(t))
	assert.Equal(t, http.StatusNotFound, missingFlow.Code, "an unknown flow is a 404")

	missingNode := do(t, handler, http.MethodPut, "/api/flows/hooks/nodes/ghost/image", testPNG(t))
	assert.Equal(t, http.StatusNotFound, missingNode.Code, "an unknown node is a 404")

	notWebhook := do(t, handler, http.MethodPut, "/api/flows/hooks/nodes/inbox/image", testPNG(t))
	assert.Equal(t, http.StatusBadRequest, notWebhook.Code, "a non-webhook node is a 400")
}

// TestAPIIndexListsEveryRoute covers the GET /api discovery index: it names the
// service, points at the OpenAPI document, and describes every route — including
// that the avatar upload takes a raw body, not multipart (issue #97).
func TestAPIIndexListsEveryRoute(t *testing.T) {
	_, handler := testServer(t)

	rec := get(t, handler, "/api/")
	require.Equal(t, http.StatusOK, rec.Code)

	var idx struct {
		Service string `json:"service"`
		OpenAPI string `json:"openapi"`
		Routes  []struct {
			Method  string `json:"method"`
			Path    string `json:"path"`
			Summary string `json:"summary"`
			Request string `json:"request"`
		} `json:"routes"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &idx))
	assert.Equal(t, "hive.desktop.api", idx.Service)
	assert.Equal(t, "/api/openapi.json", idx.OpenAPI)

	notes := make(map[string]string, len(idx.Routes))
	for _, r := range idx.Routes {
		assert.NotEmpty(t, r.Summary, "%s %s has a summary", r.Method, r.Path)
		assert.True(t, strings.HasPrefix(r.Path, "/api"), "%s is under /api", r.Path)
		notes[r.Method+" "+r.Path] = r.Request
	}
	assert.Contains(t, notes, "GET /api/", "the index lists itself")
	assert.Contains(t, notes, "GET /api/openapi.json", "the index lists the spec")
	require.Contains(t, notes, "PUT /api/profiles/{id}/image")
	assert.Contains(t, notes["PUT /api/profiles/{id}/image"], "not multipart",
		"the raw-body requirement is documented inline")
}

// TestOpenAPIDocumentAgreesWithIndex asserts the spec and the index are built
// from one table (every advertised route has an operation) and that the shapes
// the issue stumbled on are documented: the raw image body and the inbox query.
func TestOpenAPIDocumentAgreesWithIndex(t *testing.T) {
	_, handler := testServer(t)

	var idx struct {
		Routes []struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"routes"`
	}
	require.NoError(t, json.Unmarshal(get(t, handler, "/api/").Body.Bytes(), &idx))

	rec := get(t, handler, "/api/openapi.json")
	require.Equal(t, http.StatusOK, rec.Code)

	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	assert.Equal(t, "3.2.0", doc.OpenAPI)
	assert.NotEmpty(t, doc.Info.Title)
	assert.NotEmpty(t, doc.Info.Version)
	require.NotEmpty(t, doc.Servers)
	assert.True(t, strings.HasPrefix(doc.Servers[0].URL, "http://"))

	for _, r := range idx.Routes {
		methods, ok := doc.Paths[r.Path]
		require.True(t, ok, "spec describes path %s", r.Path)
		_, ok = methods[strings.ToLower(r.Method)]
		assert.True(t, ok, "spec describes %s %s", r.Method, r.Path)
	}

	var put struct {
		Parameters []struct {
			Name string `json:"name"`
			In   string `json:"in"`
		} `json:"parameters"`
		RequestBody struct {
			Content map[string]json.RawMessage `json:"content"`
		} `json:"requestBody"`
		Responses map[string]json.RawMessage `json:"responses"`
	}
	require.NoError(t, json.Unmarshal(doc.Paths["/api/profiles/{id}/image"]["put"], &put))
	assert.Contains(t, put.RequestBody.Content, "image/png")
	assert.NotContains(t, put.RequestBody.Content, "multipart/form-data")
	assert.Contains(t, put.Responses, "default", "the error shape is documented")
	hasID := false
	for _, p := range put.Parameters {
		if p.Name == "id" && p.In == "path" {
			hasID = true
		}
	}
	assert.True(t, hasID, "id is a path parameter")

	var inbox struct {
		Parameters []struct {
			Name string `json:"name"`
			In   string `json:"in"`
		} `json:"parameters"`
	}
	require.NoError(t, json.Unmarshal(doc.Paths["/api/inbox"]["get"], &inbox))
	query := make(map[string]string, len(inbox.Parameters))
	for _, p := range inbox.Parameters {
		query[p.Name] = p.In
	}
	assert.Equal(t, "query", query["feed"])
	assert.Equal(t, "query", query["profile"])
}

// TestOpenAPIParametersAreDescribed guards the fixes the agent evaluation drove:
// a genuinely-required query param is marked required (the spec must not
// contradict the server), and params carry descriptions/examples.
func TestOpenAPIParametersAreDescribed(t *testing.T) {
	_, handler := testServer(t)
	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name        string `json:"name"`
				Required    bool   `json:"required"`
				Description string `json:"description"`
				Example     string `json:"example"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(get(t, handler, "/api/openapi.json").Body.Bytes(), &doc))

	var profile struct {
		Required    bool
		Description string
		Example     string
	}
	for _, p := range doc.Paths["/api/feeds"]["get"].Parameters {
		if p.Name == "profile" {
			profile.Required, profile.Description, profile.Example = p.Required, p.Description, p.Example
		}
	}
	assert.True(t, profile.Required, "feeds.profile is required in the spec, matching the server")
	assert.NotEmpty(t, profile.Description, "feeds.profile carries a description")
	assert.Equal(t, "hive", profile.Example)
}

// TestOpenAPIDocumentsInboxDetail covers the enrichments the agent evaluation
// asked for: inbox items carry a feedId, the state fields are enumerated, and
// the events endpoint documents its real error statuses, not just a default.
func TestOpenAPIDocumentsInboxDetail(t *testing.T) {
	_, handler := testServer(t)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(get(t, handler, "/api/openapi.json").Body.Bytes(), &doc))

	obj := func(v any) map[string]any {
		m, ok := v.(map[string]any)
		require.True(t, ok, "expected object, got %T", v)
		return m
	}
	paths := obj(doc["paths"])

	events := obj(obj(obj(paths["/api/inbox/events"])["get"])["responses"])
	for _, code := range []string{"404", "409", "422", "default"} {
		assert.Contains(t, events, code, "events documents its %s response", code)
	}

	schema := obj(obj(obj(obj(obj(obj(paths["/api/inbox"])["get"])["responses"])["200"])["content"])["application/json"])["schema"]
	itemProps := obj(obj(obj(obj(schema)["properties"])["items"])["items"])["properties"]
	assert.Contains(t, obj(itemProps), "feedId", "inbox items carry a feedId")
	assert.Contains(t, obj(obj(itemProps)["lifecycle"])["enum"], "terminal", "lifecycle is enumerated")
}

// TestOpenAPISpecIsValid validates the generated document against the embedded
// OpenAPI 3.2 schema, so a reflected struct that produces a malformed schema
// fails here rather than in a downstream consumer.
func TestOpenAPISpecIsValid(t *testing.T) {
	_, handler := testServer(t)
	rec := get(t, handler, "/api/openapi.json")
	require.Equal(t, http.StatusOK, rec.Code)

	doc, err := libopenapi.NewDocument(rec.Body.Bytes())
	require.NoError(t, err)

	v, verrs := validator.NewValidator(doc)
	require.Empty(t, verrs, "the validator builds from the document")

	valid, valErrs := v.ValidateDocument()
	for _, e := range valErrs {
		t.Errorf("openapi: %s (%s)", e.Message, e.Reason)
	}
	assert.True(t, valid)
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
