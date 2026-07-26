package app

import (
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStarterSeedFetchesAsTheGivenAccount(t *testing.T) {
	seed := starterSeed(seedRef)

	sources := 0
	for _, node := range seed.Nodes {
		if node.Type != ghsource.Descriptor.Type {
			continue
		}
		sources++
		cfg, ok := node.Config.(*flow.SourceConfig)
		require.True(t, ok, "node %q should carry a source config", node.ID)
		assert.Equal(t, seedRef, cfg.Connector().(*ghsource.Config).Credential)
	}
	assert.Positive(t, sources, "the starter graph is made of source nodes")
}

func TestStarterSeedShipsANotifyingReviewRequestsFeed(t *testing.T) {
	seed := starterSeed(seedRef)

	nodesByID := map[string]flow.Node{}
	for _, node := range seed.Nodes {
		nodesByID[node.ID] = node
	}

	filter, ok := nodesByID["review-requests-filter"]
	require.True(t, ok, "the starter graph should ship a review-request filter")
	require.Equal(t, []string{"review_requested"}, filter.Config.(*flow.GithubFilterConfig).Reasons)

	reviewRequests, ok := nodesByID["review-requests"]
	require.True(t, ok, "the starter graph should ship a review-requests feed")
	notify := reviewRequests.Config.(*flow.FeedConfig).Notify
	require.NotNil(t, notify, "the seeded review-requests feed should notify")
	require.NotEmpty(t, notify.Title)

	// Every other seeded feed stays quiet — only the one feed whose whole
	// purpose is interrupting you does.
	require.Nil(t, nodesByID["notifications"].Config.(*flow.FeedConfig).Notify)
	require.Nil(t, nodesByID["my-open-prs"].Config.(*flow.FeedConfig).Notify)

	// The filter branches off the notifications source rather than sitting in
	// front of the Notifications feed: everything still lands there too.
	assert.Contains(t, seed.Wires, flow.Wire{From: "notifications-src", To: "notifications"})
	assert.Contains(t, seed.Wires, flow.Wire{From: "notifications-src", To: "review-requests-filter"})
	assert.Contains(t, seed.Wires, flow.Wire{From: "review-requests-filter", Out: 0, To: "review-requests"})
	assert.Contains(t, seed.Layout.Nodes, "review-requests", "the seeded node needs a canvas position")
}
