package flow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestFeedConfig_Validate(t *testing.T) {
	t.Run("empty icon and description are allowed", func(t *testing.T) {
		require.NoError(t, (&FeedConfig{}).Validate(nil))
	})

	t.Run("supported icon is allowed", func(t *testing.T) {
		require.NoError(t, (&FeedConfig{Icon: "sparkles"}).Validate(nil))
	})

	t.Run("unsupported icon is rejected", func(t *testing.T) {
		err := (&FeedConfig{Icon: "not-an-icon"}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "icon")
	})

	t.Run("description over the cap is rejected", func(t *testing.T) {
		err := (&FeedConfig{Description: strings.Repeat("x", feedDescriptionMaxLen+1)}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "description")
	})

	t.Run("description at the cap is allowed", func(t *testing.T) {
		require.NoError(t, (&FeedConfig{Description: strings.Repeat("x", feedDescriptionMaxLen)}).Validate(nil))
	})
}

func TestFeedConfig_RoundTrip(t *testing.T) {
	n := Node{
		ID:     "team-feed",
		Type:   "feed",
		Name:   "Team feed",
		Config: &FeedConfig{Icon: "sparkles", Description: "PRs the triage bot flagged for the team."},
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

func TestFeedConfig_OmitsEmptyFields(t *testing.T) {
	data, err := json.Marshal(Node{ID: "sink", Type: "feed", Config: &FeedConfig{}})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "icon")
	assert.NotContains(t, string(data), "description")
}

func TestNotifyConfig_Ports(t *testing.T) {
	cfg := &NotifyConfig{Title: "hi"}
	if cfg.Inputs() != 1 || cfg.Outputs() != 0 {
		t.Fatalf("notify ports = %d in / %d out, want 1 / 0", cfg.Inputs(), cfg.Outputs())
	}
}

func TestNotifyConfig_Validate(t *testing.T) {
	t.Run("title alone is enough", func(t *testing.T) {
		require.NoError(t, (&NotifyConfig{Title: "{{ .Payload.title }}"}).Validate(nil))
	})

	t.Run("missing title is rejected", func(t *testing.T) {
		err := (&NotifyConfig{}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "title")
	})

	t.Run("whitespace-only title is rejected", func(t *testing.T) {
		err := (&NotifyConfig{Title: "   "}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "title")
	})

	t.Run("title over the cap is rejected", func(t *testing.T) {
		err := (&NotifyConfig{Title: strings.Repeat("x", notifyTitleMaxLen+1)}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "title")
	})

	t.Run("body over the cap is rejected", func(t *testing.T) {
		err := (&NotifyConfig{Title: "hi", Body: strings.Repeat("x", notifyBodyMaxLen+1)}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "body")
	})

	t.Run("dedup template within the cap is allowed", func(t *testing.T) {
		require.NoError(t, (&NotifyConfig{Title: "hi", Dedup: "{{ .Payload.state }}"}).Validate(nil))
	})

	t.Run("dedup over the cap is rejected", func(t *testing.T) {
		err := (&NotifyConfig{Title: "hi", Dedup: strings.Repeat("x", notifyDedupMaxLen+1)}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dedup")
	})

	t.Run("every supported severity is allowed", func(t *testing.T) {
		for _, severity := range []string{"info", "success", "warning", "error"} {
			require.NoError(t, (&NotifyConfig{Title: "hi", Severity: severity}).Validate(nil), severity)
		}
	})

	t.Run("unsupported severity is rejected", func(t *testing.T) {
		err := (&NotifyConfig{Title: "hi", Severity: "critical"}).Validate(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "severity")
	})
}

func TestNotifyConfig_Defaults(t *testing.T) {
	cfg := &NotifyConfig{Title: "hi"}
	assert.Equal(t, NotifySeverityDefault, cfg.SeverityOrDefault())
	assert.True(t, cfg.SoundOrDefault(), "an absent sound key means sound on")

	silent := false
	assert.False(t, (&NotifyConfig{Title: "hi", Sound: &silent}).SoundOrDefault())
}

func TestNotifyConfig_RoundTrip(t *testing.T) {
	silent := false
	n := Node{
		ID:   "tell-me",
		Type: "notify",
		Name: "Tell me",
		Config: &NotifyConfig{
			Title:    "{{ .Payload.repo }} needs review",
			Body:     "{{ .Payload.title }}",
			Severity: "warning",
			Sound:    &silent,
			Dedup:    "{{ .Payload.state }}",
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

// An untouched optional key must stay out of the persisted flow file, so a
// notify node an author never configured beyond its title reads as one.
func TestNotifyConfig_OmitsEmptyFields(t *testing.T) {
	data, err := yaml.Marshal(Node{ID: "tell-me", Type: "notify", Config: &NotifyConfig{Title: "hi"}})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "body")
	assert.NotContains(t, string(data), "severity")
	assert.NotContains(t, string(data), "sound")
	assert.NotContains(t, string(data), "dedup")
}
