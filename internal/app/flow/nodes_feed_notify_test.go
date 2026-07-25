package flow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestFeedNotify_Validate(t *testing.T) {
	t.Run("an absent notify block is allowed — a quiet feed", func(t *testing.T) {
		require.NoError(t, (&FeedConfig{}).Validate(nil))
	})

	t.Run("a notify block without a title is rejected", func(t *testing.T) {
		// A notification the OS would refuse is an authoring error, caught on
		// save rather than swallowed at delivery — same rule as a notify node.
		err := (&FeedConfig{Notify: &NotifyConfig{}}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "title")
	})

	t.Run("templates referencing the payload are allowed", func(t *testing.T) {
		require.NoError(t, (&FeedConfig{Notify: &NotifyConfig{
			Title: "Review requested",
			Body:  "{{ .Payload.repo }} #{{ .Payload.num }}",
		}}).Validate(nil))
	})

	t.Run("an over-long body is rejected under the notify prefix", func(t *testing.T) {
		err := (&FeedConfig{Notify: &NotifyConfig{Title: "hi", Body: strings.Repeat("x", notifyBodyMaxLen+1)}}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "notify")
		assert.Contains(t, err.Error(), "body")
	})

	t.Run("supported severities are allowed", func(t *testing.T) {
		for _, severity := range []string{"info", "success", "warning", "error"} {
			require.NoError(t, (&FeedConfig{Notify: &NotifyConfig{Title: "hi", Severity: severity}}).Validate(nil), severity)
		}
	})

	t.Run("an unsupported severity is rejected", func(t *testing.T) {
		err := (&FeedConfig{Notify: &NotifyConfig{Title: "hi", Severity: "critical"}}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "severity")
	})
}

func TestFeedNotify_RoundTrip(t *testing.T) {
	quiet := false
	n := Node{
		ID:   "review-requests",
		Type: "feed",
		Name: "Review requests",
		Config: &FeedConfig{
			Icon: "eye",
			Notify: &NotifyConfig{
				Title:    "Review requested",
				Body:     "{{ .Payload.repo }} #{{ .Payload.num }}",
				Severity: "warning",
				Sound:    &quiet,
			},
		},
	}

	jsonData, err := json.Marshal(n)
	require.NoError(t, err)
	var fromJSON Node
	require.NoError(t, json.Unmarshal(jsonData, &fromJSON))
	assert.Equal(t, n, fromJSON)

	yamlData, err := yaml.Marshal(n)
	require.NoError(t, err)
	var fromYAML Node
	require.NoError(t, yaml.Unmarshal(yamlData, &fromYAML))
	assert.Equal(t, n, fromYAML)
}

func TestFeedNotify_QuietFeedCarriesNoNotifyKeys(t *testing.T) {
	data, err := json.Marshal(Node{ID: "inbox", Type: "feed", Config: &FeedConfig{}})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "notify")

	yamlData, err := yaml.Marshal(Node{ID: "inbox", Type: "feed", Config: &FeedConfig{}})
	require.NoError(t, err)
	assert.NotContains(t, string(yamlData), "notify")
}

func TestFeedNotify_ParsesFromFlowYAML(t *testing.T) {
	f, warnings, err := parseFlow("work", []byte(`version: 1
nodes:
  - { id: src, type: sources.github, kind: notifications }
  - { id: review-requests-filter, type: github-filter, reasons: [review_requested] }
  - id: review-requests
    type: feed
    name: Review requests
    notify:
      title: Review requested
      body: "{{ .Payload.repo }}"
wires:
  - { from: src, to: review-requests-filter }
  - { from: review-requests-filter, out: 0, to: review-requests }
`), nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	cfg, ok := f.Nodes[2].Config.(*FeedConfig)
	require.True(t, ok)
	require.NotNil(t, cfg.Notify)
	assert.Equal(t, "Review requested", cfg.Notify.Title)
}
