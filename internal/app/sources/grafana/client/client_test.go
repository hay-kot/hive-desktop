package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/sources/sourcehttp"
)

func TestValidateTokenReturnsOrg(t *testing.T) {
	t.Parallel()

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/org/", r.URL.Path)
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(Org{ID: 7, Name: "Main Org."})
	}))
	defer server.Close()

	org, err := NewClient(server.URL, "svc-token").ValidateToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 7, org.ID)
	assert.Equal(t, "Main Org.", org.Name)
	assert.Equal(t, "Bearer svc-token", gotAuth, "the service-account token is sent as a bearer")
}

func TestValidateTokenUnauthorized(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "bad").ValidateToken(t.Context())
	assert.ErrorIs(t, err, sourcehttp.ErrUnauthorized)
}

func TestQueryProxiesPromQLAndReturnsData(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/datasources/proxy/uid/ds-uid/api/v1/query", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, "query=up", string(body), "PromQL is form-encoded")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up"},"value":[1,"1"]}]}}`))
	}))
	defer server.Close()

	result, err := NewClient(server.URL, "t").Query(t.Context(), "ds-uid", "up")
	require.NoError(t, err)
	assert.Equal(t, "vector", result.ResultType)
	assert.JSONEq(t, `[{"metric":{"__name__":"up"},"value":[1,"1"]}]`, string(result.Result))
}

func TestQueryReportsPrometheusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Prometheus reports a bad query in the body with HTTP 400.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "t").Query(t.Context(), "ds-uid", "up{")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse error")
}

func TestQueryRateLimited(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "t").Query(t.Context(), "ds-uid", "up")
	assert.ErrorIs(t, err, sourcehttp.ErrRateLimited)
}
