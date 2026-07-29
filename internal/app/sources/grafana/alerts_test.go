package grafana

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/store"
)

func TestAlertsConfigValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		config  AlertsConfig
		wantErr bool
	}{
		{name: "valid", config: AlertsConfig{Credential: "grafana/host-1"}},
		{name: "zero config", config: AlertsConfig{}, wantErr: true},
		{name: "wrong provider", config: AlertsConfig{Credential: "github/octocat"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.config.Validate()
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func alertsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func TestAlertsProduceEmitsOnePerFiringAlert(t *testing.T) {
	t.Parallel()

	server := alertsServer(t, `[
		{"fingerprint":"abc","labels":{"alertname":"HighLatency"},"annotations":{"summary":"p99 over 1s"},"status":{"state":"active"}},
		{"fingerprint":"def","labels":{"alertname":"DiskFull"},"annotations":{},"status":{"state":"active"}}
	]`)
	defer server.Close()
	fx, _ := connectedFetcher(t, server.URL)

	src := &alertsSource{fetcher: fx, topic: "source:flow/node"}

	var msgs []store.Msg
	require.NoError(t, src.Produce(t.Context(), func(m store.Msg) error {
		msgs = append(msgs, m)
		return nil
	}))

	require.Len(t, msgs, 2, "one message per firing alert")
	assert.Equal(t, "abc", msgs[0].Key, "keyed by fingerprint")
	assert.Equal(t, SourceKind, msgs[0].SourceKind)

	var first alertPayload
	require.NoError(t, json.Unmarshal(msgs[0].Payload, &first))
	assert.Equal(t, "p99 over 1s", first.Title, "title prefers the summary annotation")
	assert.Equal(t, stateFiring, first.State)

	var second alertPayload
	require.NoError(t, json.Unmarshal(msgs[1].Payload, &second))
	assert.Equal(t, "DiskFull", second.Title, "title falls back to the alertname")
}

func TestAlertsClassifierFiringThenResolved(t *testing.T) {
	t.Parallel()

	firing := store.Observation{ExternalID: "abc", Title: "HighLatency", Payload: []byte(`{"state":"firing"}`)}
	resolved := store.Observation{ExternalID: "abc", Title: "HighLatency", Payload: []byte(`{"state":"resolved"}`)}

	first := alertsClassifier{}.Classify(nil, firing)
	assert.Equal(t, stateFiring, first.Kind)
	assert.Equal(t, store.LifecycleActive, first.Lifecycle)

	transition := alertsClassifier{}.Classify(&firing, resolved)
	assert.Equal(t, stateResolved, transition.Kind)
	assert.Equal(t, store.LifecycleTerminal, transition.Lifecycle)
	assert.Equal(t, store.TransitionEnteredTerminal, transition.Transition)
	assert.Equal(t, stateResolved, transition.ArchivedReason)

	reopened := alertsClassifier{}.Classify(&resolved, firing)
	assert.Equal(t, store.TransitionLeftTerminal, reopened.Transition, "a re-firing alert leaves the terminal state")
}

// A still-firing alert re-observed with a changed payload takes the default arm
// every poll. It must stay trivial, not re-raise attention — the opposite would
// notify on every poll for the lifetime of the alert.
func TestAlertsClassifierReobservedStaysTrivial(t *testing.T) {
	t.Parallel()

	firing := store.Observation{ExternalID: "abc", Title: "HighLatency", Payload: []byte(`{"state":"firing"}`)}
	resolved := store.Observation{ExternalID: "abc", Title: "HighLatency", Payload: []byte(`{"state":"resolved"}`)}

	stillFiring := alertsClassifier{}.Classify(&firing, firing)
	assert.Equal(t, "updated", stillFiring.Kind)
	assert.Equal(t, store.TransitionNone, stillFiring.Transition)
	assert.Equal(t, store.AttentionTrivial, stillFiring.Attention, "a re-observed firing alert must not re-raise attention")

	stillResolved := alertsClassifier{}.Classify(&resolved, resolved)
	assert.Equal(t, "updated", stillResolved.Kind)
	assert.Equal(t, store.AttentionTrivial, stillResolved.Attention)
}

// A payload the rewrite cannot parse as a JSON object is returned unchanged, so
// a malformed alert never blocks absence confirmation.
func TestWithResolvedStateLeavesNonObjectPayloadUnchanged(t *testing.T) {
	t.Parallel()

	payload := []byte(`"not an object"`)
	assert.Equal(t, payload, withResolvedState(payload))
}

func TestAlertsAbsenceMarksResolvedAndTerminal(t *testing.T) {
	t.Parallel()

	previous := []store.Observation{{ExternalID: "abc", Title: "HighLatency", Payload: []byte(`{"title":"HighLatency","state":"firing","labels":{"alertname":"HighLatency"}}`)}}

	verdicts, err := alertsAbsence{}.ConfirmAbsence(t.Context(), previous)
	require.NoError(t, err)

	verdict, ok := verdicts["abc"]
	require.True(t, ok, "an absent alert gets a verdict")
	assert.True(t, verdict.Terminal, "an absent alert is authoritatively resolved, so its source head is evicted")
	require.NotNil(t, verdict.Current)

	assert.Equal(t, stateResolved, alertState(verdict.Current.Payload), "the payload is rewritten to resolved")
	// Other fields survive the rewrite so the archived item keeps its identity.
	var payload alertPayload
	require.NoError(t, json.Unmarshal(verdict.Current.Payload, &payload))
	assert.Equal(t, "HighLatency", payload.Title)
	assert.Equal(t, "HighLatency", payload.Labels["alertname"])
}
