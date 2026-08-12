package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
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
	h := newAgentHarness(t)

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
	assertNoNullArrays(t, resp2, "sessions", "missingMcps", "missingPackages")

	resp3 := h.post(t, AgentWorkspacesPathPrefix+"skills", testToken, struct{}{})
	defer func() { _ = resp3.Body.Close() }()
	assertNoNullArrays(t, resp3, "packages")
}

// The package catalogue is the workspace editor's read. A fresh install has
// the seeded hive package, and its members are the skills this build ships —
// resolved through the glob, not enumerated anywhere.
func TestAgentSkillPackagesServeTheSeededHivePackage(t *testing.T) {
	h := newAgentHarness(t)

	resp := h.post(t, AgentWorkspacesPathPrefix+"skills", testToken, struct{}{})
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body agentSkillPackagesResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Empty(t, body.Problem)
	require.NotEmpty(t, body.Packages)

	var hive agentSkillPackageView
	for _, pkg := range body.Packages {
		if pkg.Name == "hive" {
			hive = pkg
		}
	}
	require.Equal(t, "hive", hive.Name, "the seeded package must be served")
	require.NotEmpty(t, hive.Members, "hive-* selects the shipped skills")
	for _, member := range hive.Members {
		assert.True(t, member.Shipped)
		assert.True(t, strings.HasPrefix(member.Slug, "hive-"), "member %q does not match the package pattern", member.Slug)
	}
}
