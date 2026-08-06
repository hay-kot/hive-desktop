package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnCallURLReadsPluginSettings(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/plugins/grafana-irm-app/settings", r.URL.Path)
		assert.Equal(t, "Bearer t", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"jsonData":{"onCallApiUrl":"https://oncall.example.com/oncall/"}}`))
	}))
	defer server.Close()

	base, err := NewClient(server.URL, "t").OnCallURL(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "https://oncall.example.com/oncall", base, "the trailing slash is trimmed so path joins stay single-slashed")
}

// A stack without IRM answers the settings call but names no OnCall host. That
// is a configuration problem, not a decode failure, so it must say so.
func TestOnCallURLRejectsAnUnconfiguredPlugin(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonData":{}}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "t").OnCallURL(t.Context())
	assert.ErrorContains(t, err, "no OnCall API URL")
}

func TestAlertGroupsSendsScopeAndAuth(t *testing.T) {
	t.Parallel()

	var query, auth, grafanaURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/alert_groups/", r.URL.Path)
		query, auth, grafanaURL = r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("X-Grafana-URL")
		_, _ = w.Write([]byte(`{"results":[],"next":null}`))
	}))
	defer server.Close()

	client := NewOnCallClient(server.URL, "https://stack.example.com", "t")
	_, err := client.AlertGroups(t.Context(), AlertGroupQuery{
		States:        []string{"new", "acknowledged"},
		IntegrationID: "CFRPV98RPR1U8",
		TeamID:        "T3HRAP3K2FE1J",
	})
	require.NoError(t, err)

	assert.Equal(t, "Bearer t", auth)
	assert.Equal(t, "https://stack.example.com", grafanaURL, "OnCall resolves the token's org from the stack URL")
	assert.Contains(t, query, "state=new&state=acknowledged", "states go out as repeated params")
	assert.Contains(t, query, "integration_id=CFRPV98RPR1U8")
	assert.Contains(t, query, "team_id=T3HRAP3K2FE1J")
}

// An empty scope must not send empty params — the API reads integration_id=""
// as "match the integration named empty string", which matches nothing.
func TestAlertGroupsOmitsAnEmptyScope(t *testing.T) {
	t.Parallel()

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"results":[],"next":null}`))
	}))
	defer server.Close()

	_, err := NewOnCallClient(server.URL, "https://stack.example.com", "t").AlertGroups(t.Context(), AlertGroupQuery{})
	require.NoError(t, err)
	assert.NotContains(t, query, "integration_id")
	assert.NotContains(t, query, "team_id")
	assert.NotContains(t, query, "state=")
}

func TestAlertGroupsDecodesAGroup(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{
			"id":"I68T24C13IFW1",
			"integration_id":"CFRPV98RPR1U8",
			"team_id":"T3HRAP3K2FE1J",
			"alerts_count":6,
			"state":"acknowledged",
			"title":"Memory above 90% threshold",
			"created_at":"2026-08-01T12:00:00Z",
			"acknowledged_at":"2026-08-01T12:04:00Z",
			"silenced_at":null,
			"permalinks":{"slack":"https://slack.example.com/thread","telegram":null},
			"labels":[
				{"key":{"id":"k1","name":"severity"},"value":{"id":"v1","name":"critical"}},
				{"key":{"id":"k2","name":"squad"},"value":{"id":"v2","name":"adaptive-telemetry"}}
			]
		}],"next":null}`))
	}))
	defer server.Close()

	groups, err := NewOnCallClient(server.URL, "https://stack.example.com", "t").AlertGroups(t.Context(), AlertGroupQuery{})
	require.NoError(t, err)
	require.Len(t, groups, 1)

	group := groups[0]
	assert.Equal(t, "I68T24C13IFW1", group.ID)
	assert.Equal(t, "acknowledged", group.State)
	assert.Equal(t, 6, group.AlertsCount)
	assert.Equal(t, "T3HRAP3K2FE1J", group.TeamID)
	// A null permalink and a null timestamp are ordinary, not a decode failure.
	assert.Empty(t, group.Permalinks.Telegram)
	assert.Empty(t, group.SilencedAt)
	assert.Equal(t, "https://slack.example.com/thread", group.URL())
	assert.Equal(t, map[string]string{"severity": "critical", "squad": "adaptive-telemetry"}, group.LabelMap())
}

// Slack is where the on-call conversation is, so it wins; the IRM web page is
// the fallback when the group was never posted to a channel.
func TestAlertGroupURLPrefersSlackThenWeb(t *testing.T) {
	t.Parallel()

	both := AlertGroup{Permalinks: Permalinks{Slack: "https://slack", Web: "https://web"}}
	assert.Equal(t, "https://slack", both.URL())

	webOnly := AlertGroup{Permalinks: Permalinks{Web: "https://web"}}
	assert.Equal(t, "https://web", webOnly.URL())

	assert.Empty(t, AlertGroup{}.URL(), "a group with no links has no url rather than a broken one")
}

func TestAlertGroupsWalksEveryPage(t *testing.T) {
	t.Parallel()

	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		if page == "1" {
			_, _ = fmt.Fprintf(w, `{"results":[{"id":"a"}],"next":"%s/api/v1/alert_groups/?page=2"}`, r.Host)
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"id":"b"}],"next":null}`))
	}))
	defer server.Close()

	groups, err := NewOnCallClient(server.URL, "https://stack.example.com", "t").AlertGroups(t.Context(), AlertGroupQuery{})
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2"}, pages)
	require.Len(t, groups, 2)
	assert.Equal(t, "b", groups[1].ID)
}

// A short snapshot would read as "these groups are gone" and archive live
// alerts, so running out of pages has to fail the poll instead.
func TestAlertGroupsFailsRatherThanTruncate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"id":"a"}],"next":"http://next.example.com"}`))
	}))
	defer server.Close()

	groups, err := NewOnCallClient(server.URL, "https://stack.example.com", "t").AlertGroups(t.Context(), AlertGroupQuery{})
	require.Error(t, err)
	assert.Nil(t, groups, "a truncated set must not be returned alongside the error")
	assert.ErrorContains(t, err, "scope this source", "the error says how to fix it")
}
