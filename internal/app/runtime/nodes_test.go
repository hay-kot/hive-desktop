package runtime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// The two registries are the two halves of one declaration: flow.registry
// says how a node type is configured, and behaviors says what it does at run
// time. A type in one and not the other is a node the editor offers and the
// engine cannot run, or the reverse — and both fail at the worst moment
// (deploy, or the first message), so they fail here instead.
func TestEveryNodeTypeHasARuntimeBehavior(t *testing.T) {
	t.Parallel()

	configured := flow.NodeTypes()
	require.NotEmpty(t, configured)

	for _, nodeType := range configured {
		behavior, ok := behaviors[nodeType]
		require.Truef(t, ok, "node type %q is registered in flow but the engine has no behavior for it", nodeType)

		declared := 0
		if behavior.relay {
			declared++
		}
		if behavior.sinks != nil {
			declared++
		}
		if behavior.processor != nil {
			declared++
		}
		require.Equalf(t, 1, declared, "node type %q must declare exactly one runtime behavior", nodeType)
	}

	for nodeType := range behaviors {
		require.Containsf(t, configured, nodeType, "the engine has a behavior for %q but no node type is registered under it", nodeType)
	}
}

// A terminal's outputs are one message's committed side effects. The counts
// here are the contract the engine's accounting depends on — a feed that
// interrupts is two outputs for one message, not two messages.
func TestTerminalSinks(t *testing.T) {
	t.Parallel()

	msg := store.Msg{ID: "1", Payload: json.RawMessage(`{"title":"hi"}`)}
	msg.Key = "k"
	msg.OccurrenceKey = "oc"
	msg.Topic = "source:f/src"
	msg.SourceKind = "github"
	msg.SourceScope = "notifications"

	t.Run("a quiet feed claims membership and nothing else", func(t *testing.T) {
		t.Parallel()
		outputs := feedSinks("f", "inbox", &flow.FeedConfig{}, msg)
		require.Len(t, outputs, 1)
		require.Equal(t, "feed", outputs[0].Sink.Kind)
		require.Equal(t, "f/inbox", outputs[0].Sink.TargetID)
		require.Equal(t, "k", outputs[0].Key)
		require.Empty(t, outputs[0].Payload, "a membership claim carries identity, not content")
	})

	t.Run("a feed that interrupts also raises a notify targeting itself", func(t *testing.T) {
		t.Parallel()
		outputs := feedSinks("f", "inbox", &flow.FeedConfig{Notify: &flow.NotifyConfig{Title: "New"}}, msg)
		require.Len(t, outputs, 2)
		require.Equal(t, "feed", outputs[0].Sink.Kind)
		require.Equal(t, "notify", outputs[1].Sink.Kind)
		require.Equal(t, "f/inbox", outputs[1].Sink.TargetID, "both deliver through one executor, targeting the feed's own id")
		require.JSONEq(t, `{"title":"hi"}`, string(outputs[1].Payload))
		require.Empty(t, outputs[1].NotifyDedupKey, "a feed keeps occurrence-key dedup; only a notify node overrides it")
	})

	t.Run("an action names its catalog id and carries no source identity", func(t *testing.T) {
		t.Parallel()
		outputs := actionSinks("f", "run", &flow.ActionConfig{Action: "triage-it"}, msg)
		require.Len(t, outputs, 1)
		require.Equal(t, "action", outputs[0].Sink.Kind)
		require.Equal(t, "triage-it", outputs[0].Sink.TargetID)
		require.Equal(t, "oc", outputs[0].OccurrenceKey, "the occurrence key is the dedup key that stops an action re-firing")
		require.Empty(t, outputs[0].SourceTopic)
	})

	t.Run("a notify node carries source identity so the banner can reveal the item", func(t *testing.T) {
		t.Parallel()
		outputs := notifySinks("f", "ping", &flow.NotifyConfig{Title: "Ping"}, msg)
		require.Len(t, outputs, 1)
		require.Equal(t, "notify", outputs[0].Sink.Kind)
		require.Equal(t, "f/ping", outputs[0].Sink.TargetID)
		require.Equal(t, "github", outputs[0].SourceKind)
		require.Equal(t, "source:f/src", outputs[0].SourceTopic)
	})

	t.Run("a notify node with no dedup template keys on the item id", func(t *testing.T) {
		t.Parallel()
		outputs := notifySinks("f", "ping", &flow.NotifyConfig{Title: "Ping"}, msg)
		require.Equal(t, "k", outputs[0].NotifyDedupKey, "fire once per item, not once per occurrence")
	})
}

// A notify node's dedup key is what stops it re-interrupting for every update
// to an item: by default the item id (fire once), or a template rendered over
// the message (fire once per distinct value).
func TestNotifyDedup(t *testing.T) {
	t.Parallel()

	base := store.Msg{Key: "pr-7", OccurrenceKey: "pr-7:open:1700:comment", Payload: json.RawMessage(`{"state":"approved","n":3,"blank":""}`)}

	t.Run("absent template defaults to the item id", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "pr-7", notifyDedup(&flow.NotifyConfig{Title: "x"}, base))
	})

	t.Run("a template renders over the message payload", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "approved", notifyDedup(&flow.NotifyConfig{Title: "x", Dedup: "{{ .Payload.state }}"}, base))
	})

	t.Run("Key is addressable in the template", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "pr-7/approved", notifyDedup(&flow.NotifyConfig{Title: "x", Dedup: "{{ .Key }}/{{ .Payload.state }}"}, base))
	})

	t.Run("a blank render falls back to the item id rather than collapsing every item onto one key", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "pr-7", notifyDedup(&flow.NotifyConfig{Title: "x", Dedup: "{{ .Payload.blank }}"}, base))
	})

	t.Run("a broken template falls back to the item id rather than wedging the commit", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "pr-7", notifyDedup(&flow.NotifyConfig{Title: "x", Dedup: "{{ .Payload.state "}, base))
	})

	t.Run("a keyless message with no usable dedup leaves the key empty for the digest fallback", func(t *testing.T) {
		t.Parallel()
		keyless := store.Msg{Payload: json.RawMessage(`{}`)}
		require.Empty(t, notifyDedup(&flow.NotifyConfig{Title: "x"}, keyless))
	})
}
