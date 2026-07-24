package pipeline

import (
	"fmt"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline/actions"
	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline/flow"
)

// ActionTypeNotify is the action type the notify executor is registered
// under. Unlike "shell" or "publish-message" it is never authored in
// actions.yml: a notify node carries its config inline, and
// FlowNotifyActions synthesizes the Action the dispatcher needs from the
// live flow set.
const ActionTypeNotify = "notify"

// NotifyActionConfig is the executor-facing projection of a notify node's
// flow.NotifyConfig. It exists because the output worker speaks in
// actions.Action values (whose Config is an actions.ActionConfig), while the
// authoritative definition stays the flow node — this type is rebuilt from
// that node on every resolution, never persisted.
type NotifyActionConfig struct {
	Title    string
	Body     string
	Severity string
	Sound    bool
}

// Validate satisfies actions.ActionConfig. The flow's own validator is
// authoritative (SaveFlow rejects a notify node without a title before it can
// ever reach a queue), so this only re-states the one invariant the executor
// depends on.
func (c *NotifyActionConfig) Validate() error {
	if strings.TrimSpace(c.Title) == "" {
		return fmt.Errorf("notify: title is required")
	}
	return nil
}

// FlowNotifyActions is the output worker's ActionLister: it resolves
// synthetic "notify:<flowId>/<nodeId>" ids from the live flow set and
// delegates every other id to the authored actions.yml store.
//
// Resolution is late-bound (per lookup, not per construction) for the same
// reason the producer's source lister and the webhook listener's route table
// are: flows hot-reload, so an edited title or body applies to the next
// delivery without a restart.
type FlowNotifyActions struct {
	flows   FlowLister
	actions ActionLister
}

// NewFlowNotifyActions wraps an authored action store with notify-node
// resolution over flows.
func NewFlowNotifyActions(flows FlowLister, store ActionLister) *FlowNotifyActions {
	return &FlowNotifyActions{flows: flows, actions: store}
}

// Get resolves id to an executable action. A notify id that no longer names
// a node in any flow is reported as unknown, exactly like a deleted
// actions.yml entry: its queued command fails rather than silently doing
// nothing.
func (l *FlowNotifyActions) Get(id string) (actions.Action, bool) {
	target, ok := NotifyActionTarget(id)
	if !ok {
		if l.actions == nil {
			return actions.Action{}, false
		}
		return l.actions.Get(id)
	}
	if l.flows == nil {
		return actions.Action{}, false
	}

	flowID, nodeID, found := strings.Cut(target, "/")
	if !found {
		return actions.Action{}, false
	}
	for _, f := range l.flows.List() {
		if f.ID != flowID {
			continue
		}
		for _, node := range f.Nodes {
			if node.ID != nodeID || node.Type != "notify" {
				continue
			}
			cfg, ok := node.Config.(*flow.NotifyConfig)
			if !ok {
				return actions.Action{}, false
			}
			return actions.Action{
				ID:    id,
				Label: notifyLabel(node),
				Type:  ActionTypeNotify,
				Config: &NotifyActionConfig{
					Title:    cfg.Title,
					Body:     cfg.Body,
					Severity: cfg.SeverityOrDefault(),
					Sound:    cfg.SoundOrDefault(),
				},
			}, true
		}
	}
	return actions.Action{}, false
}

// notifyLabel is the human name a notify delivery reports in the Activity
// view and the jobs list: the node's author-given name, else its id.
func notifyLabel(node flow.Node) string {
	if node.Name != "" {
		return node.Name
	}
	return "Notify " + node.ID
}
