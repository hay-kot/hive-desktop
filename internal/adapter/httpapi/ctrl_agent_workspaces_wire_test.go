package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Array-valued fields must encode as [] on the wire, never null: encoding/json
// marshals a nil Go slice as the JSON literal null, and the frontend calls
// .length on these fields unconditionally. The seeded hive/ workspace declares
// no mcps and its open misses nothing, which is exactly the shape that carried
// null before the DTO mappers normalized it.
func TestAgentWireArraysAreNeverNull(t *testing.T) {
	h := newAgentHarness(t, true)

	assertNoNullArrays := func(t *testing.T, resp *http.Response, fields ...string) {
		t.Helper()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		raw, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		var doc map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &doc))
		for _, field := range fields {
			assert.NotEqualf(t, "null", string(doc[field]), "%s must be [] on the wire, got %s in %s", field, doc[field], raw)
		}
		assert.NotContainsf(t, string(raw), `"mcps":null`, "a workspace view carried a null mcps array: %s", raw)
	}

	resp := h.post(t, AgentWorkspacesPathPrefix+"workspaces", testToken, struct{}{})
	defer func() { _ = resp.Body.Close() }()
	assertNoNullArrays(t, resp, "workspaces")

	resp2 := h.post(t, AgentWorkspacesPathPrefix+"workspaces/open", testToken, map[string]string{"dir": "hive"})
	defer func() { _ = resp2.Body.Close() }()
	assertNoNullArrays(t, resp2, "sessions", "missingMcps")
}
