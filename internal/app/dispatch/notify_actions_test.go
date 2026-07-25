package dispatch

import (
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flowListerTest struct{ flows []flow.Flow }

func (f flowListerTest) List() []flow.Flow { return f.flows }

type actionListerTest struct{ actions map[string]actions.Action }

func (a actionListerTest) Get(id string) (actions.Action, bool) {
	action, ok := a.actions[id]
	return action, ok
}

func notifyFlows() flowListerTest {
	silent := false
	return flowListerTest{flows: []flow.Flow{{
		ID: "triage",
		Nodes: []flow.Node{
			{ID: "src", Type: "github-source", Config: &flow.GithubSourceConfig{}},
			{ID: "tell-me", Type: "notify", Name: "Tell me", Config: &flow.NotifyConfig{
				Title: "{{ .Payload.repo }}", Body: "{{ .Payload.title }}", Severity: "warning", Sound: &silent,
			}},
			{ID: "bare", Type: "notify", Config: &flow.NotifyConfig{Title: "hi"}},
		},
	}}}
}

func TestFlowNotifyActions_SynthesizesFromTheFlowNode(t *testing.T) {
	lister := NewFlowNotifyActions(notifyFlows(), actionListerTest{})

	action, ok := lister.Get(store.NotifyActionID("triage/tell-me"))
	require.True(t, ok)
	assert.Equal(t, ActionTypeNotify, action.Type)
	assert.Equal(t, "Tell me", action.Label)
	assert.Equal(t, &NotifyActionConfig{
		Title:    "{{ .Payload.repo }}",
		Body:     "{{ .Payload.title }}",
		Severity: "warning",
		Sound:    false,
	}, action.Config)
}

// A node with no author-given name still needs a label for the Activity view
// and the jobs list, and its unset optional fields resolve to their defaults.
func TestFlowNotifyActions_FillsDefaultsForABareNode(t *testing.T) {
	lister := NewFlowNotifyActions(notifyFlows(), actionListerTest{})

	action, ok := lister.Get(store.NotifyActionID("triage/bare"))
	require.True(t, ok)
	assert.Equal(t, "Notify bare", action.Label)
	assert.Equal(t, &NotifyActionConfig{Title: "hi", Severity: flow.NotifySeverityDefault, Sound: true}, action.Config)
}

func TestFlowNotifyActions_DelegatesAuthoredIDs(t *testing.T) {
	authored := actions.Action{ID: "review-pr", Type: "launch-session"}
	lister := NewFlowNotifyActions(notifyFlows(), actionListerTest{actions: map[string]actions.Action{"review-pr": authored}})

	action, ok := lister.Get("review-pr")
	require.True(t, ok)
	assert.Equal(t, authored, action)

	_, ok = lister.Get("does-not-exist")
	assert.False(t, ok)
}

// A notify id whose node is gone is unknown, exactly like a deleted
// actions.yml entry: its queued command fails visibly instead of silently
// doing nothing.
func TestFlowNotifyActions_UnknownNotifyTargets(t *testing.T) {
	lister := NewFlowNotifyActions(notifyFlows(), actionListerTest{})

	for _, id := range []string{
		store.NotifyActionID("triage/deleted"),
		store.NotifyActionID("other-flow/tell-me"),
		store.NotifyActionID("triage/src"), // exists, but is not a notify node
		store.NotifyActionID("no-slash"),
		store.NotifyActionPrefix,
	} {
		_, ok := lister.Get(id)
		assert.False(t, ok, id)
	}
}

func TestNotifyActionID_RoundTrips(t *testing.T) {
	target, ok := store.NotifyActionTarget(store.NotifyActionID("triage/tell-me"))
	require.True(t, ok)
	assert.Equal(t, "triage/tell-me", target)

	// An authored action id is a slug, so it can never look like a notify id.
	_, ok = store.NotifyActionTarget("review-pr")
	assert.False(t, ok)
}

func TestNotifyActionConfig_RequiresATitle(t *testing.T) {
	require.NoError(t, (&NotifyActionConfig{Title: "hi"}).Validate())
	require.Error(t, (&NotifyActionConfig{}).Validate())
}

// A notifying feed resolves through the same synthetic action id a notify
// node does — one delivery path for both — and is the only shape that carries
// the new-activity restriction.
func TestFlowNotifyActions_ResolvesANotifyingFeed(t *testing.T) {
	quiet := false
	flows := flowListerTest{flows: []flow.Flow{{ID: "triage", Nodes: []flow.Node{
		{ID: "review-requests", Type: "feed", Name: "Review requests", Config: &flow.FeedConfig{
			Icon: "eye",
			Notify: &flow.NotifyConfig{
				Title:    "Review requested",
				Body:     "{{ .Payload.repo }}",
				Severity: "warning",
				Sound:    &quiet,
			},
		}},
		{ID: "quiet", Type: "feed", Config: &flow.FeedConfig{}},
	}}}}

	action, ok := NewFlowNotifyActions(flows, nil).Get(store.NotifyActionID("triage/review-requests"))
	require.True(t, ok)
	assert.Equal(t, ActionTypeNotify, action.Type)
	assert.Equal(t, "Review requests", action.Label)
	assert.Equal(t, &NotifyActionConfig{
		Title:       "Review requested",
		Body:        "{{ .Payload.repo }}",
		Severity:    "warning",
		Sound:       false,
		OnlyWhenNew: true,
	}, action.Config)

	// A feed that does not notify has nothing to resolve: reaching here for
	// one means the flow was edited after the command was queued.
	_, ok = NewFlowNotifyActions(flows, nil).Get(store.NotifyActionID("triage/quiet"))
	assert.False(t, ok)
}
