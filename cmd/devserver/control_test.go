package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testControl(t *testing.T, cfg Config) (http.Handler, *Store, *Pusher) {
	t.Helper()
	require.NoError(t, cfg.normalize())
	cache := testCache(t, cfg.Cache.TTL)
	store := fixedStore(t, cfg.Overlays...)
	proxy := NewProxy(cfg.Upstream, cache, store, zerolog.Nop())
	pusher := NewPusher(cfg.Webhooks, zerolog.Nop())
	return NewControl(cfg, store, cache, proxy, pusher, zerolog.Nop()).Handler(), store, pusher
}

func ctl(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestControlActionAppliesOverlay(t *testing.T) {
	handler, store, _ := testControl(t, Config{})

	rec := ctl(t, handler, http.MethodPost, "/_ctl/action",
		`{"repo":"hay-kot/hive-desktop","num":58,"action":"approval-requested"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	overlay, ok := store.Get("hay-kot/hive-desktop#58")
	require.True(t, ok)
	require.NotNil(t, overlay.Reason)
	assert.Equal(t, "approval_requested", *overlay.Reason)
	assert.NotNil(t, overlay.UpdatedAt, "an action must stamp updatedAt or the classifier ignores it")
}

func TestControlMergeActionSetsStateAndAbsence(t *testing.T) {
	handler, store, _ := testControl(t, Config{})

	rec := ctl(t, handler, http.MethodPost, "/_ctl/action",
		`{"repo":"hay-kot/hive-desktop","num":58,"action":"merge"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	overlay, ok := store.Get("hay-kot/hive-desktop#58")
	require.True(t, ok)
	require.NotNil(t, overlay.State)
	assert.Equal(t, "merged", *overlay.State)
	require.NotNil(t, overlay.Absent)
	assert.True(t, *overlay.Absent, "a merge must also drop the item from search results")
}

func TestControlRejectsUnknownAction(t *testing.T) {
	handler, _, _ := testControl(t, Config{})
	rec := ctl(t, handler, http.MethodPost, "/_ctl/action",
		`{"repo":"o/r","num":1,"action":"approve"}`)
	// "approve" is deliberately absent: GitHub has no such notification
	// reason, and inventing one would teach a wrong lesson about the app.
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unknown action")
}

func TestControlRejectsInvalidMatcher(t *testing.T) {
	handler, _, _ := testControl(t, Config{})
	for _, body := range []string{
		`{"repo":"noslash","num":1,"action":"comment"}`,
		`{"repo":"o/r","num":0,"action":"comment"}`,
		`{"repo":"","num":1,"action":"comment"}`,
	} {
		rec := ctl(t, handler, http.MethodPost, "/_ctl/action", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}

func TestControlRejectsUnknownFields(t *testing.T) {
	handler, _, _ := testControl(t, Config{})
	rec := ctl(t, handler, http.MethodPost, "/_ctl/action",
		`{"repo":"o/r","num":1,"action":"comment","typo":true}`)
	// A typo in a hand-written curl call should fail loudly, not silently do
	// nothing and look like a devserver bug.
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestControlOverlaySetAndClear(t *testing.T) {
	handler, store, _ := testControl(t, Config{})

	rec := ctl(t, handler, http.MethodPost, "/_ctl/overlay",
		`{"repo":"o/r","num":1,"set":{"state":"closed","labels":["bug"]}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	overlay, ok := store.Get("o/r#1")
	require.True(t, ok)
	require.NotNil(t, overlay.Labels)
	assert.Equal(t, []string{"bug"}, *overlay.Labels)

	rec = ctl(t, handler, http.MethodPost, "/_ctl/overlay/clear", `{"repo":"o/r","num":1}`)
	require.Equal(t, http.StatusOK, rec.Code)
	_, ok = store.Get("o/r#1")
	assert.False(t, ok)
}

func TestControlOverlayRejectsInvalidState(t *testing.T) {
	handler, _, _ := testControl(t, Config{})
	rec := ctl(t, handler, http.MethodPost, "/_ctl/overlay",
		`{"repo":"o/r","num":1,"set":{"state":"squashed"}}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not one of")
}

func TestControlClearAll(t *testing.T) {
	handler, store, _ := testControl(t, Config{})
	store.Apply("o/r#1", Mutations{State: stringPtr("closed")})
	store.Apply("o/r#2", Mutations{State: stringPtr("closed")})

	require.Equal(t, http.StatusOK, ctl(t, handler, http.MethodPost, "/_ctl/overlays/clear", `{}`).Code)
	assert.Empty(t, store.Overlays())
}

func TestControlStateReportsEverythingTheDashboardNeeds(t *testing.T) {
	handler, store, _ := testControl(t, Config{
		Scenarios: map[string]Scenario{
			"pr-flow": {Description: "d", Steps: []ScenarioStep{{
				Match: Matcher{Repo: "o/r", Num: 1}, Set: Mutations{Reason: stringPtr("comment")},
			}}},
		},
		Webhooks: WebhookConfig{
			Targets:  []WebhookTarget{{Name: "local", URL: "http://127.0.0.1:1/hooks/x", Secret: "s"}},
			Payloads: map[string]map[string]any{"pr-opened": {"id": "1"}},
		},
	})
	store.Apply("o/r#1", Mutations{State: stringPtr("closed")})

	rec := ctl(t, handler, http.MethodGet, "/_ctl/state", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var view StateView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, DefaultUpstream, view.Upstream)
	assert.Contains(t, view.Overlays, "o/r#1")
	require.Len(t, view.Scenarios, 1)
	assert.Equal(t, "pr-flow", view.Scenarios[0].Name)
	assert.False(t, view.Scenarios[0].Running)
	require.Len(t, view.Targets, 1)
	assert.Equal(t, []string{"pr-opened"}, view.Payloads)
	assert.NotEmpty(t, view.Actions, "the dashboard renders whatever the vocabulary contains")
}

// TestControlStateJSONKeysMatchDashboard pins the wire keys the dashboard's
// JS reads. Decoding back into the Go structs would pass whatever the field
// names are, so it cannot catch a missing json tag — which is exactly how the
// target list first shipped rendering "undefined".
func TestControlStateJSONKeysMatchDashboard(t *testing.T) {
	handler, store, _ := testControl(t, Config{
		Scenarios: map[string]Scenario{"flow": {Steps: []ScenarioStep{
			{Match: Matcher{Repo: "o/r", Num: 1}, Set: Mutations{Reason: stringPtr("comment")}},
		}}},
		Webhooks: WebhookConfig{
			Targets:  []WebhookTarget{{Name: "local", URL: "http://127.0.0.1:1/hooks/x"}},
			Payloads: map[string]map[string]any{"p": {"id": "1"}},
		},
	})
	store.Apply("o/r#1", Mutations{State: stringPtr("closed"), Absent: boolPtr(true)})

	var raw map[string]any
	require.NoError(t, json.Unmarshal(ctl(t, handler, http.MethodGet, "/_ctl/state", "").Body.Bytes(), &raw))

	for _, key := range []string{
		"upstream", "cacheTTL", "cacheEntries", "stats", "items",
		"overlays", "scenarios", "targets", "payloads", "recent", "pushes", "actions",
	} {
		assert.Contains(t, raw, key)
	}

	targets, ok := raw["targets"].([]any)
	require.True(t, ok)
	require.Len(t, targets, 1)
	assert.Contains(t, targets[0], "name")
	assert.Contains(t, targets[0], "url")
	assert.NotContains(t, targets[0], "secret", "the secret must not be serializable at all")

	overlay, ok := raw["overlays"].(map[string]any)["o/r#1"].(map[string]any)
	require.True(t, ok)
	// Lowercase mutation keys, and untouched fields omitted rather than null.
	assert.Equal(t, "closed", overlay["state"])
	assert.Equal(t, true, overlay["absent"])
	assert.NotContains(t, overlay, "labels")

	scenario, ok := raw["scenarios"].([]any)[0].(map[string]any)
	require.True(t, ok)
	for _, key := range []string{"name", "description", "steps", "running"} {
		assert.Contains(t, scenario, key)
	}

	action, ok := raw["actions"].([]any)[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, action, "name")
	assert.Contains(t, action, "label")
}

func TestControlStateNeverLeaksWebhookSecrets(t *testing.T) {
	handler, _, _ := testControl(t, Config{
		Webhooks: WebhookConfig{Targets: []WebhookTarget{
			{Name: "local", URL: "http://127.0.0.1:1/hooks/x", Secret: "super-secret"},
		}},
	})
	rec := ctl(t, handler, http.MethodGet, "/_ctl/state", "")
	assert.NotContains(t, rec.Body.String(), "super-secret")
}

func TestControlRunScenarioAppliesStepsInOrder(t *testing.T) {
	handler, store, _ := testControl(t, Config{
		Scenarios: map[string]Scenario{"flow": {Steps: []ScenarioStep{
			{Match: Matcher{Repo: "o/r", Num: 1}, Set: Mutations{Reason: stringPtr("review_requested")}},
			{Match: Matcher{Repo: "o/r", Num: 1}, Set: Mutations{State: stringPtr("merged"), Absent: boolPtr(true)}},
		}}},
	})

	rec := ctl(t, handler, http.MethodPost, "/_ctl/scenarios/flow/run", `{}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	require.Eventually(t, func() bool {
		overlay, ok := store.Get("o/r#1")
		return ok && overlay.State != nil && *overlay.State == "merged"
	}, 2*time.Second, 10*time.Millisecond)

	overlay, _ := store.Get("o/r#1")
	require.NotNil(t, overlay.Reason)
	assert.Equal(t, "review_requested", *overlay.Reason, "later steps must accumulate, not replace")
}

func TestControlRunScenarioRejectsUnknownName(t *testing.T) {
	handler, _, _ := testControl(t, Config{})
	rec := ctl(t, handler, http.MethodPost, "/_ctl/scenarios/nope/run", `{}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestControlRunScenarioRejectsConcurrentRun(t *testing.T) {
	handler, _, _ := testControl(t, Config{
		Scenarios: map[string]Scenario{"slow": {Steps: []ScenarioStep{
			{Match: Matcher{Repo: "o/r", Num: 1}, Set: Mutations{Reason: stringPtr("comment")}, Wait: time.Second},
		}}},
	})

	require.Equal(t, http.StatusAccepted, ctl(t, handler, http.MethodPost, "/_ctl/scenarios/slow/run", `{}`).Code)
	second := ctl(t, handler, http.MethodPost, "/_ctl/scenarios/slow/run", `{}`)
	assert.Equal(t, http.StatusConflict, second.Code,
		"two overlapping runs of one scenario would interleave their steps")
}

// ── Pusher ───────────────────────────────────────────────────────────────────

func TestPusherDeliversPayloadWithSecret(t *testing.T) {
	var (
		mu        sync.Mutex
		secret    string
		body      map[string]any
		decodeErr error
	)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		secret = r.Header.Get(secretHeader)
		decodeErr = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"delivered":1}`)) //nolint:errcheck // test server
	}))
	defer target.Close()

	handler, _, pusher := testControl(t, Config{Webhooks: WebhookConfig{
		Targets:  []WebhookTarget{{Name: "local", URL: target.URL, Secret: "s3cret"}},
		Payloads: map[string]map[string]any{"pr-opened": {"id": "pr-1", "title": "Add retry"}},
	}})

	rec := ctl(t, handler, http.MethodPost, "/_ctl/webhooks/push",
		`{"target":"local","payload":"pr-opened","overrides":{"state":"resolved"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	mu.Lock()
	defer mu.Unlock()
	require.NoError(t, decodeErr)
	// The header the desktop's webhook listener authenticates on.
	assert.Equal(t, "s3cret", secret)
	assert.Equal(t, "pr-1", body["id"])
	assert.Equal(t, "Add retry", body["title"])
	assert.Equal(t, "resolved", body["state"], "overrides must merge over the named payload")

	require.Len(t, pusher.Recent(), 1)
	assert.Equal(t, http.StatusAccepted, pusher.Recent()[0].Status)
}

func TestPusherOverridesDoNotMutateStoredPayload(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer target.Close()

	handler, _, pusher := testControl(t, Config{Webhooks: WebhookConfig{
		Targets:  []WebhookTarget{{Name: "local", URL: target.URL}},
		Payloads: map[string]map[string]any{"p": {"id": "1"}},
	}})

	ctl(t, handler, http.MethodPost, "/_ctl/webhooks/push", `{"target":"local","payload":"p","overrides":{"state":"resolved"}}`)

	// A push must not permanently rewrite the configured payload, or the
	// second push of the same name would be a different event.
	stored, ok := pusher.Payload("p")
	require.True(t, ok)
	assert.NotContains(t, stored, "state")
}

func TestPusherInlineBody(t *testing.T) {
	var (
		body      map[string]any
		decodeErr error
	)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeErr = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer target.Close()

	handler, _, _ := testControl(t, Config{Webhooks: WebhookConfig{
		Targets: []WebhookTarget{{Name: "local", URL: target.URL}},
	}})

	rec := ctl(t, handler, http.MethodPost, "/_ctl/webhooks/push",
		`{"target":"local","body":{"id":"x","title":"Inline"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, decodeErr)
	assert.Equal(t, "Inline", body["title"])
}

func TestPusherRequiresExactlyOnePayloadSource(t *testing.T) {
	handler, _, _ := testControl(t, Config{Webhooks: WebhookConfig{
		Targets:  []WebhookTarget{{Name: "local", URL: "http://127.0.0.1:1/hooks/x"}},
		Payloads: map[string]map[string]any{"p": {"id": "1"}},
	}})

	for _, body := range []string{
		`{"target":"local"}`,
		`{"target":"local","payload":"p","body":{"id":"1"}}`,
	} {
		rec := ctl(t, handler, http.MethodPost, "/_ctl/webhooks/push", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}

func TestPusherReportsTargetFailure(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no webhook endpoint at this path", http.StatusNotFound)
	}))
	defer target.Close()

	handler, _, pusher := testControl(t, Config{Webhooks: WebhookConfig{
		Targets:  []WebhookTarget{{Name: "local", URL: target.URL}},
		Payloads: map[string]map[string]any{"p": {"id": "1"}},
	}})

	rec := ctl(t, handler, http.MethodPost, "/_ctl/webhooks/push", `{"target":"local","payload":"p"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	// The most likely real failure is a path that matches no webhook-source
	// node, so the target's own message has to reach the author.
	assert.Contains(t, rec.Body.String(), "no webhook endpoint")
	require.Len(t, pusher.Recent(), 1)
	assert.NotEmpty(t, pusher.Recent()[0].Error)
}

func TestPusherRejectsUnknownTargetAndPayload(t *testing.T) {
	handler, _, _ := testControl(t, Config{Webhooks: WebhookConfig{
		Targets:  []WebhookTarget{{Name: "local", URL: "http://127.0.0.1:1/hooks/x"}},
		Payloads: map[string]map[string]any{"p": {"id": "1"}},
	}})

	rec := ctl(t, handler, http.MethodPost, "/_ctl/webhooks/push", `{"target":"nope","payload":"p"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "no webhook target")

	rec = ctl(t, handler, http.MethodPost, "/_ctl/webhooks/push", `{"target":"local","payload":"nope"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "no payload named")
}

// ── Config ───────────────────────────────────────────────────────────────────

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devserver.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadConfigMissingFileYieldsWorkingDefaults(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "absent.yaml"))
	require.NoError(t, err)
	// No config at all still has to give the "just stop burning my rate
	// limit" behaviour, which is the primary use.
	assert.Equal(t, DefaultListen, cfg.Listen)
	assert.Equal(t, DefaultUpstream, cfg.Upstream)
	assert.Equal(t, DefaultTTL, cfg.Cache.TTL)
	assert.NotEmpty(t, cfg.Cache.Path)
}

func TestLoadConfigParsesFullFile(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, `
listen: 127.0.0.1:9999
upstream: https://api.github.com/
cache:
  ttl: 30s
overlays:
  - match: {repo: hay-kot/hive-desktop, num: 58}
    set:
      state: open
      reason: approval_requested
      labels: [needs-review]
scenarios:
  pr-flow:
    description: approve then merge
    steps:
      - match: {repo: hay-kot/hive-desktop, num: 58}
        set: {reason: approval_requested}
      - wait: 5s
      - match: {repo: hay-kot/hive-desktop, num: 58}
        set: {state: merged, absent: true}
webhooks:
  targets:
    - name: local
      url: http://127.0.0.1:24681/hooks/devserver
      secret: dev
  payloads:
    pr-opened:
      id: pr-1
      title: Add retry
`))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:9999", cfg.Listen)
	assert.Equal(t, "https://api.github.com", cfg.Upstream, "a trailing slash must be trimmed")
	assert.Equal(t, 30*time.Second, cfg.Cache.TTL)

	require.Len(t, cfg.Overlays, 1)
	require.NotNil(t, cfg.Overlays[0].Set.Labels)
	assert.Equal(t, []string{"needs-review"}, *cfg.Overlays[0].Set.Labels)
	assert.Equal(t, "hay-kot/hive-desktop#58", cfg.Overlays[0].Match.Key())

	require.Len(t, cfg.Scenarios["pr-flow"].Steps, 3)
	assert.Equal(t, 5*time.Second, cfg.Scenarios["pr-flow"].Steps[1].Wait)
	require.NotNil(t, cfg.Scenarios["pr-flow"].Steps[2].Set.Absent)
	assert.True(t, *cfg.Scenarios["pr-flow"].Steps[2].Set.Absent)

	require.Len(t, cfg.Webhooks.Targets, 1)
	assert.Equal(t, "dev", cfg.Webhooks.Targets[0].Secret)
	assert.Equal(t, "pr-1", cfg.Webhooks.Payloads["pr-opened"]["id"])
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"bad overlay repo":    "overlays:\n  - match: {repo: nope, num: 1}\n    set: {state: open}\n",
		"zero overlay num":    "overlays:\n  - match: {repo: o/r, num: 0}\n    set: {state: open}\n",
		"unknown state":       "overlays:\n  - match: {repo: o/r, num: 1}\n    set: {state: squashed}\n",
		"empty scenario":      "scenarios:\n  flow:\n    steps: []\n",
		"no-op scenario step": "scenarios:\n  flow:\n    steps:\n      - match: {repo: o/r, num: 1}\n",
		"scenario bad match":  "scenarios:\n  flow:\n    steps:\n      - match: {repo: nope, num: 1}\n        set: {state: open}\n",
		"target missing name": "webhooks:\n  targets:\n    - url: http://127.0.0.1:1/x\n",
		"target bad url":      "webhooks:\n  targets:\n    - {name: a, url: ftp://x}\n",
		"duplicate target":    "webhooks:\n  targets:\n    - {name: a, url: 'http://127.0.0.1:1/x'}\n    - {name: a, url: 'http://127.0.0.1:2/x'}\n",
		"malformed yaml":      "listen: [unclosed\n",
	}
	for name, body := range cases {
		_, err := LoadConfig(writeConfig(t, body))
		assert.Error(t, err, name)
	}
}

func TestLoadConfigAllowsWaitOnlyScenarioStep(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, "scenarios:\n  flow:\n    steps:\n      - wait: 1s\n"))
	require.NoError(t, err)
	assert.Equal(t, time.Second, cfg.Scenarios["flow"].Steps[0].Wait)
}
