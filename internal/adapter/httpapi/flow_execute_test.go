package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/runtime"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// executeResponse mirrors the endpoint's response shape from the outside, so
// the test reads what an agent would read rather than the core's own types.
type executeResponse struct {
	FlowID        string               `json:"flowId"`
	Warnings      []string             `json:"warnings"`
	Nodes         []runtime.NodeTrace  `json:"nodes"`
	Outputs       []store.Output       `json:"outputs"`
	FeedSnapshots []store.FeedSnapshot `json:"feedSnapshots"`
	KVMutations   []store.KVMutation   `json:"kvMutations"`
}

func execute(t *testing.T, handler http.Handler, body map[string]any) *executeResponse {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	rec := do(t, handler, http.MethodPost, "/api/flows/execute", encoded)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var out executeResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return &out
}

func executeTrace(t *testing.T, resp *executeResponse, nodeID string) runtime.NodeTrace {
	t.Helper()
	for _, node := range resp.Nodes {
		if node.NodeID == nodeID {
			return node
		}
	}
	t.Fatalf("no trace for node %q", nodeID)
	return runtime.NodeTrace{}
}

// fanOutHTTPFlow is the flow the issue describes: a source feeding a function
// node that turns one message into one feed item per entity.
func fanOutHTTPFlow() flow.Flow {
	return flow.Flow{
		ID: "metrics", Name: "Metrics", Enabled: true,
		Nodes: []flow.Node{
			{ID: "src", Type: "sources.webhook", Config: flow.NewSourceConfig(webhook.Descriptor.Type, &webhook.Config{Path: "metrics"})},
			{ID: "split", Type: "function", Config: &flow.FunctionConfig{OnMessage: "console.log(\"series\", msg.Payload.series.length);\n" +
				"return msg.Payload.series.map(function (s) { return { ...msg, Key: s.name, Payload: { title: s.name } }; });"}},
			{ID: "inbox", Type: "feed", Name: "Inbox", Config: &flow.FeedConfig{}},
		},
		Wires: []flow.Wire{{From: "src", To: "split"}, {From: "split", To: "inbox"}},
	}
}

// The whole point of the endpoint: run an installed flow against a payload and
// read back what every node emitted, without a poll and without a write.
func TestFlowExecuteRunsAnInstalledFlow(t *testing.T) {
	core, handler := testServer(t)
	require.NoError(t, core.Flows.Save(t.Context(), fanOutHTTPFlow()))

	resp := execute(t, handler, map[string]any{
		"flowId": "metrics",
		"nodeId": "src",
		"messages": []map[string]any{
			{"Key": "poll", "Payload": map[string]any{"series": []map[string]any{{"name": "a"}, {"name": "b"}}}},
		},
	})

	assert.Equal(t, "metrics", resp.FlowID)

	split := executeTrace(t, resp, "split")
	require.Len(t, split.Emitted, 1)
	require.Len(t, split.Emitted[0].Messages, 2)
	assert.Equal(t, "a", split.Emitted[0].Messages[0].Key)
	require.Len(t, split.Console, 1)
	assert.Equal(t, "series 2", split.Console[0].Text)

	// The topic is filled in from the source node, so the outputs a feed would
	// have claimed are the real ones rather than blank-scoped.
	require.Len(t, resp.Outputs, 2)
	assert.Equal(t, "metrics/inbox", resp.Outputs[0].Sink.TargetID)
	assert.Equal(t, "source:metrics/src", resp.Outputs[0].SourceTopic)

	// Nothing was committed: the profile's inbox is still empty.
	items, err := core.Inbox.ListItems(t.Context(), "metrics", 100)
	require.NoError(t, err)
	assert.Empty(t, items)
}

// Testing an unsaved edit is what makes this a testing tool rather than "run
// the installed flow harder", so a document that is not on disk must run — and
// must not become one.
func TestFlowExecuteRunsAnInlineDocument(t *testing.T) {
	_, handler := testServer(t)

	const document = `
version: 1
name: Draft
nodes:
  - id: fn
    type: function
    on_message: |
      if (msg.Payload.state === "closed") return null;
      return { ...msg, Payload: { title: msg.Payload.title } };
  - id: inbox
    type: feed
wires:
  - from: fn
    to: inbox
`

	resp := execute(t, handler, map[string]any{
		"flowYaml": document,
		"nodeId":   "fn",
		"messages": []map[string]any{
			{"Key": "1", "Topic": "source:draft/src", "Payload": map[string]any{"title": "open one", "state": "open"}},
			{"Key": "2", "Topic": "source:draft/src", "Payload": map[string]any{"title": "closed one", "state": "closed"}},
		},
	})

	assert.Equal(t, app.DryRunDocumentID, resp.FlowID)
	fn := executeTrace(t, resp, "fn")
	assert.Equal(t, 2, fn.In)
	assert.Equal(t, 1, fn.Dropped, "the closed one returned null")
	require.Len(t, resp.Outputs, 1)
	assert.Equal(t, "1", resp.Outputs[0].Key)

	// Nothing was installed under the document's id.
	list := get(t, handler, "/api/profiles")
	assert.NotContains(t, list.Body.String(), app.DryRunDocumentID)
}

// The same document as a JSON object, since an API caller building a request
// body should not have to render YAML to use the endpoint.
func TestFlowExecuteAcceptsAJSONDocument(t *testing.T) {
	_, handler := testServer(t)

	resp := execute(t, handler, map[string]any{
		"flowIdOverride": "draft",
		"flow": map[string]any{
			"version": 1,
			"name":    "Draft",
			"nodes": []map[string]any{
				{"id": "fn", "type": "function", "on_message": "return msg;"},
				{"id": "inbox", "type": "feed"},
			},
			"wires": []map[string]any{{"from": "fn", "to": "inbox"}},
		},
		"nodeId":   "fn",
		"messages": []map[string]any{{"Key": "1", "Payload": map[string]any{"n": 1}}},
	})

	assert.Equal(t, "draft", resp.FlowID)
	assert.Len(t, resp.Outputs, 1)
}

// Dedup and notify-once are the hardest node behaviours to reason about, so the
// seeded kv has to be the whole world the script sees — and its writes have to
// come back rather than land.
func TestFlowExecuteSandboxesKV(t *testing.T) {
	_, handler := testServer(t)

	body := map[string]any{
		"flow": map[string]any{
			"version": 1,
			"nodes": []map[string]any{
				{"id": "fn", "type": "function", "on_message": `
					if (kv.has(msg.Key)) return null;
					kv.set(msg.Key, { notified: true });
					return msg;
				`},
				{"id": "alert", "type": "notify", "title": "{{ .Key }}"},
			},
			"wires": []map[string]any{{"from": "fn", "to": "alert"}},
		},
		"nodeId": "fn",
		"kv":     map[string]any{"fn": map[string]any{"seen": map[string]any{"notified": true}}},
		"messages": []map[string]any{
			{"Key": "seen", "Payload": map[string]any{}},
			{"Key": "fresh", "Payload": map[string]any{}},
		},
	}

	first := execute(t, handler, body)
	require.Len(t, first.Outputs, 1, "the seeded key is suppressed")
	assert.Equal(t, "fresh", first.Outputs[0].Key)
	require.Len(t, first.KVMutations, 1)
	assert.Equal(t, "fresh", first.KVMutations[0].Key)

	// Repeatable: the previous run's write was reported, not persisted, so the
	// second call sees the same seed and gives the same answer.
	second := execute(t, handler, body)
	assert.Equal(t, first.Outputs, second.Outputs)
	assert.Equal(t, first.KVMutations, second.KVMutations)
}

// A script failure is the case the endpoint exists for, so it is an answer with
// a position in it — not a 500.
func TestFlowExecuteReportsAScriptFailure(t *testing.T) {
	_, handler := testServer(t)

	resp := execute(t, handler, map[string]any{
		"flow": map[string]any{
			"version": 1,
			"nodes":   []map[string]any{{"id": "fn", "type": "function", "on_message": "\nthrow new Error('boom');"}},
		},
		"nodeId":   "fn",
		"messages": []map[string]any{{"Key": "1", "Payload": map[string]any{}}},
	})

	fn := executeTrace(t, resp, "fn")
	assert.False(t, fn.OK)
	require.NotNil(t, fn.Error)
	assert.Equal(t, runtime.ScriptErrorRuntime, fn.Error.Kind)
	assert.Equal(t, 2, fn.Error.Line)
	assert.Contains(t, fn.Error.Message, "boom")
}

func TestFlowExecuteRejectsBadRequests(t *testing.T) {
	core, handler := testServer(t)
	require.NoError(t, core.Flows.Save(t.Context(), fanOutHTTPFlow()))

	status := func(body map[string]any) int {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		return do(t, handler, http.MethodPost, "/api/flows/execute", encoded).Code
	}
	message := []map[string]any{{"Key": "1", "Payload": map[string]any{}}}

	assert.Equal(t, http.StatusUnprocessableEntity,
		status(map[string]any{"nodeId": "src", "messages": message}),
		"no flow was named")

	assert.Equal(t, http.StatusUnprocessableEntity,
		status(map[string]any{"flowId": "metrics", "flowYaml": "version: 1", "nodeId": "src", "messages": message}),
		"two flows were named")

	assert.Equal(t, http.StatusUnprocessableEntity,
		status(map[string]any{"flowId": "metrics", "nodeId": "src"}),
		"no input")

	assert.Equal(t, http.StatusNotFound,
		status(map[string]any{"flowId": "nope", "nodeId": "src", "messages": message}))

	assert.Equal(t, http.StatusBadRequest,
		status(map[string]any{"flowId": "metrics", "nodeId": "ghost", "messages": message}),
		"the flow has no such node")

	assert.Equal(t, http.StatusBadRequest,
		status(map[string]any{"flowYaml": "version: 1\nnodes:\n  - id: fn\n    type: nope\n", "nodeId": "fn", "messages": message}),
		"the document does not parse")
}
