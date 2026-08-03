package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hay-kot/criterio"
	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/runtime"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/web/extractors"
)

// flowExecuteRequest is one dry run. Exactly one of flowId, flow and flowYaml
// says which flow to execute; the rest is the input and the sandbox it runs
// against.
type flowExecuteRequest struct {
	FlowID string `json:"flowId,omitempty"`
	// Flow is a flow document as a JSON object — the same schema as a
	// flows/<id>.yaml file, `version` included, since the server parses it
	// through the same decoder a file goes through.
	Flow map[string]any `json:"flow,omitempty"`
	// FlowYAML is that document as YAML text, for a caller whose source of
	// truth is the file itself.
	FlowYAML string `json:"flowYaml,omitempty"`
	// FlowIDOverride is the id an inline document runs under. It only scopes
	// the run's node KV and the feed ids in its outputs; nothing is installed
	// under it.
	FlowIDOverride string `json:"flowIdOverride,omitempty"`

	NodeID   string      `json:"nodeId"`
	Messages []store.Msg `json:"messages"`
	// KV seeds the sandbox: node id -> key -> value. Durable KV is neither
	// read nor written, so this is the whole world a `kv.get` sees.
	KV map[string]map[string]json.RawMessage `json:"kv,omitempty"`
}

func (b flowExecuteRequest) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("flow", b.flowSourceCount(), exactlyOneFlowSource),
		criterio.Run("nodeId", b.NodeID, criterio.Required),
		criterio.Run("messages", b.Messages, criterio.SliceNotEmpty[store.Msg]()),
	)
}

func (b flowExecuteRequest) flowSourceCount() int {
	count := 0
	for _, given := range []bool{b.FlowID != "", len(b.Flow) > 0, b.FlowYAML != ""} {
		if given {
			count++
		}
	}
	return count
}

func exactlyOneFlowSource(count int) error {
	if count == 1 {
		return nil
	}
	return errors.New("set exactly one of flowId, flow, or flowYaml")
}

// flowExecuteResponse is what the run observed. Nothing in it was applied:
// outputs and kvMutations are what a live run *would* have committed.
type flowExecuteResponse struct {
	// FlowID is the flow that ran — the installed id, or the id an inline
	// document ran under.
	FlowID string `json:"flowId"`
	// Warnings are the soft diagnostics parsing an inline document produced,
	// the same ones a file load reports.
	Warnings []string `json:"warnings,omitempty"`
	runtime.DryRunResult
}

// FlowExecute runs a flow against supplied input and reports what every node
// emitted, committing nothing.
func (ctrl *Controller) FlowExecute(w http.ResponseWriter, r *http.Request) error {
	body, err := extractors.Body[flowExecuteRequest](w, r)
	if err != nil {
		return err
	}

	document, err := flowDocument(body)
	if err != nil {
		return err
	}

	result, err := ctrl.core.Flows.DryRun(r.Context(), app.FlowDryRun{
		FlowID:     body.FlowID,
		Document:   document,
		DocumentID: body.FlowIDOverride,
		NodeID:     body.NodeID,
		Messages:   body.Messages,
		KV:         seedKV(body.KV),
	})
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, flowExecuteResponse{
		FlowID:       result.FlowID,
		Warnings:     result.Warnings,
		DryRunResult: result.Run,
	})
}

// flowDocument renders whichever inline form was given back into bytes for the
// core's parser. A JSON object needs no conversion: the parser is a YAML
// decoder, and JSON is YAML.
func flowDocument(body flowExecuteRequest) ([]byte, error) {
	switch {
	case body.FlowYAML != "":
		return []byte(body.FlowYAML), nil
	case len(body.Flow) > 0:
		encoded, err := json.Marshal(body.Flow)
		if err != nil {
			return nil, app.Wrap(err, app.KindInvalid, "encoding the flow document")
		}
		return encoded, nil
	default:
		return nil, nil
	}
}

// seedKV flattens the request's JSON values into the opaque JSON text node KV
// stores, so what a seeded `kv.get` parses back is what the caller wrote.
func seedKV(seed map[string]map[string]json.RawMessage) map[string]map[string]string {
	if len(seed) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(seed))
	for nodeID, entries := range seed {
		values := make(map[string]string, len(entries))
		for key, value := range entries {
			values[key] = string(value)
		}
		out[nodeID] = values
	}
	return out
}
