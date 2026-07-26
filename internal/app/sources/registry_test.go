package sources

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
)

// A descriptor is the whole declaration of a connector: everything the editor,
// the docs, and later an MCP tool listing read comes from it. A field left
// zero is not a smaller connector, it is one that renders as a blank palette
// entry or an unroutable node type — so every one is required here rather
// than defaulted somewhere downstream.
func TestEveryDescriptorIsComplete(t *testing.T) {
	t.Parallel()

	for _, connectorType := range Types() {
		descriptor, ok := Lookup(connectorType)
		require.Truef(t, ok, "Types() returned %q but Lookup does not know it", connectorType)

		assert.Equalf(t, connectorType, descriptor.Type,
			"connector %q is registered under a key that is not its own Type", connectorType)
		assert.NotEmptyf(t, descriptor.Title, "connector %q has no title", connectorType)
		assert.Containsf(t, []connector.Mode{connector.ModePull, connector.ModePush}, descriptor.Mode,
			"connector %q declares no ingestion mode", connectorType)
		assert.NotZerof(t, descriptor.Stability, "connector %q declares no stability", connectorType)
		require.NotNilf(t, descriptor.NewConfig, "connector %q has no config factory", connectorType)
	}
}

// A descriptor's Provider is half of a credentials.Ref, so it has to satisfy
// the same rule a ref does. The mistake this catches is pasting the connector
// type in ("sources.github" instead of "github"): a ref built from it fails to
// parse, every source node on that connector reports a bad credential, and the
// Integrations card lists no accounts — three confusing symptoms of one typo.
func TestDescriptorProviderIsAValidCredentialProvider(t *testing.T) {
	t.Parallel()

	for _, connectorType := range Types() {
		descriptor, _ := Lookup(connectorType)
		if descriptor.Provider == "" {
			continue // a connector with nothing to authenticate as
		}
		ref := credentials.Ref{Provider: descriptor.Provider, Account: "account"}
		assert.NoErrorf(t, ref.Validate(),
			"connector %q declares provider %q, which is not a usable credentials provider",
			connectorType, descriptor.Provider)
	}
}

// The type string is the node's `type:` discriminator, the registry key, the
// docs filename, and the frontend's registry key. Namespacing it is what
// makes source connectors sort together everywhere those are enumerated, and
// it only holds if every connector follows it.
func TestConnectorTypesAreNamespaced(t *testing.T) {
	t.Parallel()

	for _, connectorType := range Types() {
		assert.Truef(t, strings.HasPrefix(connectorType, "sources."),
			"connector %q must be namespaced \"sources.<name>\"", connectorType)
		assert.NotEmptyf(t, strings.TrimPrefix(connectorType, "sources."),
			"connector %q has an empty name after its namespace", connectorType)
	}
}

// The decoder mutates a config in place, so a factory that returns a shared
// pointer would have two source nodes editing one value — the same constraint
// flow's node factories carry, restated here because a connector author only
// reads this package.
func TestNewConfigReturnsADistinctValue(t *testing.T) {
	t.Parallel()

	for _, connectorType := range Types() {
		descriptor, _ := Lookup(connectorType)
		first, second := descriptor.NewConfig(), descriptor.NewConfig()
		assert.NotSamef(t, first, second,
			"connector %q returns the same config value twice; the decoder mutates it in place", connectorType)
	}
}

// A zero config must fail validation. Every connector has at least one field
// it cannot work without (a kind, a path), and a config that validates empty
// means a source node saved with nothing filled in reaches the producer.
func TestZeroConfigDoesNotValidate(t *testing.T) {
	t.Parallel()

	for _, connectorType := range Types() {
		descriptor, _ := Lookup(connectorType)
		assert.Errorf(t, descriptor.NewConfig().Validate(),
			"connector %q accepts an empty config", connectorType)
	}
}

// The schema is what phase 7's MCP tool input schemas and a schema-rendered
// editor form are generated from. Reflecting it means it cannot describe a
// field the struct lacks, but it can still fail to produce an object schema
// at all — which fails silently as an empty form.
func TestEveryDescriptorReflectsAnObjectSchema(t *testing.T) {
	t.Parallel()

	for _, connectorType := range Types() {
		descriptor, _ := Lookup(connectorType)

		raw, err := connector.Schema(descriptor)
		require.NoErrorf(t, err, "connector %q", connectorType)

		var schema struct {
			Type       string                     `json:"type"`
			Title      string                     `json:"title"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		require.NoErrorf(t, json.Unmarshal(raw, &schema), "connector %q schema is not JSON", connectorType)

		assert.Equalf(t, "object", schema.Type, "connector %q schema is not an object", connectorType)
		assert.Equalf(t, descriptor.Title, schema.Title, "connector %q schema title", connectorType)
		assert.NotEmptyf(t, schema.Properties, "connector %q schema declares no properties", connectorType)
	}
}

// All returns a copy so a caller ranging over the registry cannot delete a
// connector from it. The registry is package state read by the flow and
// runtime node registries at init, so a mutation would be unrecoverable.
func TestAllReturnsACopy(t *testing.T) {
	t.Parallel()

	before := len(All())
	require.NotZero(t, before)

	mutated := All()
	for name := range mutated {
		delete(mutated, name)
	}

	assert.Len(t, All(), before)
}
