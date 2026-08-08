package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/sources/canonical"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
	"github.com/hay-kot/hive-desktop/internal/app/sources/grafana/client"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

type AlertsConfig struct {
	Credential string `json:"credential" yaml:"credential" jsonschema:"title=Credential,description=The connected Grafana stack to fetch as, as 'grafana/<account>'."`
	// Matchers narrow the fetch server-side. A shared stack can hold five
	// figures of active alerts, so pulling the lot and discarding it in a
	// function node is what makes this node unusable at exactly the scale an
	// alerts feed is worth having.
	Matchers []string `json:"matchers,omitempty" yaml:"matchers,omitempty" jsonschema:"title=Label matchers,description=Alertmanager label matchers, e.g. 'squad=platform' or 'severity=~critical|warning'. An alert must match every one."`
}

func (c *AlertsConfig) Validate() error {
	if _, err := c.CredentialRef(); err != nil {
		return err
	}
	for _, matcher := range c.Matchers {
		if err := validateMatcher(matcher); err != nil {
			return err
		}
	}
	return nil
}

// matcherOperators are Alertmanager's label matcher operators, longest first so
// "!=" is recognized before the "=" inside it.
var matcherOperators = []string{"!=", "=~", "!~", "="}

// validateMatcher checks a matcher is addressable — a non-empty label name and
// a recognized operator — and nothing more. The endpoint owns the syntax, so
// re-implementing its value rules here would only reject matchers it accepts.
//
// The scan is left to right rather than operator by operator: a value may
// contain an operator ("path=/a!=b"), and only the leftmost one separates the
// label name from it.
func validateMatcher(matcher string) error {
	trimmed := strings.TrimSpace(matcher)
	if trimmed == "" {
		return fmt.Errorf("grafana alerts: a matcher is empty; remove the blank line")
	}
	for i := range trimmed {
		for _, op := range matcherOperators {
			if !strings.HasPrefix(trimmed[i:], op) {
				continue
			}
			if strings.TrimSpace(trimmed[:i]) == "" {
				return fmt.Errorf("grafana alerts: matcher %q has no label name before %q", matcher, op)
			}
			return nil
		}
	}
	return fmt.Errorf("grafana alerts: matcher %q needs one of =, !=, =~, !~ (e.g. %q)", matcher, "squad=platform")
}

func (c *AlertsConfig) CredentialRef() (credentials.Ref, error) {
	return parseGrafanaRef(c.Credential)
}

// AlertsDescriptor declares the alerts connector. Unlike metrics it classifies
// and confirms absence: the Alertmanager response is the complete firing set
// for the node's matchers, so an alert that leaves it is authoritatively
// resolved, not merely unseen. Editing matchers therefore reconciles items out,
// which is correct — they are no longer in the set this node claims.
var AlertsDescriptor = connector.Descriptor{
	Type:          "sources.grafana_alerts",
	Title:         "Grafana alerts source",
	ProviderTitle: "Grafana",
	Provider:      Provider,
	Mode:          connector.ModePull,
	Stability:     connector.Stable,
	Capabilities:  connector.CapClassify | connector.CapConfirmAbsence,
	NewConfig:     func() connector.Config { return &AlertsConfig{} },
}

func NewAlertsFactory(fetchers *Fetchers) connector.Factory {
	return connector.Factory{
		New: func(node connector.Node, cfg connector.Config) (connector.Instance, error) {
			config, ok := cfg.(*AlertsConfig)
			if !ok {
				return connector.Instance{}, fmt.Errorf("grafana alerts %q: config is %T, want *grafana.AlertsConfig", node.ID(), cfg)
			}
			ref, err := config.CredentialRef()
			if err != nil {
				return connector.Instance{}, fmt.Errorf("grafana alerts %q: %w", node.ID(), err)
			}
			return connector.Instance{
				Type: AlertsDescriptor.Type,
				Node: node,
				Metadata: connector.Metadata{
					ProfileID:   node.FlowID,
					SourceKind:  SourceKind,
					SourceScope: ref.Account,
					Policy:      node.Policy,
				},
				Pull:       &alertsSource{fetcher: fetchers.For(ref), matchers: config.Matchers, topic: node.Topic()},
				Classifier: alertsClassifier{},
				Absence:    alertsAbsence{},
				Config:     config,
			}, nil
		},
	}
}

// alertsSource polls one node's firing alerts, emitting one message per alert
// keyed by fingerprint so each alert maps to its own durable item.
type alertsSource struct {
	fetcher  *fetcher
	matchers []string
	topic    string
}

var _ connector.PullSource = (*alertsSource)(nil)

// alertPayload is what a firing alert emits. title and state are canonical
// fields the ingest boundary and classifier read; labels and annotations ride
// along for a function node to route on.
type alertPayload struct {
	Title       string            `json:"title"`
	State       string            `json:"state"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	StartsAt    string            `json:"startsAt,omitempty"`
}

func (s *alertsSource) Produce(ctx context.Context, emit func(store.Msg) error) error {
	alerts, err := s.fetcher.Alerts(ctx, s.matchers)
	if err != nil {
		return fmt.Errorf("grafana alerts: %w", err)
	}
	for _, alert := range alerts {
		body, err := json.Marshal(firingPayload(alert))
		if err != nil {
			return fmt.Errorf("grafana alerts: encoding %q: %w", alert.Fingerprint, err)
		}
		if err := emit(store.Msg{Key: alert.Fingerprint, Topic: s.topic, SourceKind: SourceKind, Payload: body}); err != nil {
			return err
		}
	}
	return nil
}

// firingPayload builds the payload for a currently firing alert. State is
// always "firing" here — the resolved state is minted by the absence confirmer
// when the alert leaves the firing set.
func firingPayload(alert client.Alert) alertPayload {
	title := strings.TrimSpace(alert.Annotations["summary"])
	if title == "" {
		title = strings.TrimSpace(alert.Labels["alertname"])
	}
	if title == "" {
		title = "Grafana alert"
	}
	return alertPayload{
		Title:       title,
		State:       stateFiring,
		Labels:      alert.Labels,
		Annotations: alert.Annotations,
		StartsAt:    alert.StartsAt,
	}
}

const (
	stateFiring   = "firing"
	stateResolved = "resolved"
)

// alertsClassifier maps an alert's state to lifecycle: firing is active,
// resolved is terminal.
type alertsClassifier struct{}

var _ store.Classifier = alertsClassifier{}

func (alertsClassifier) Classify(previous *store.Observation, current store.Observation) store.Classification {
	state := canonical.State(current.Payload)
	lifecycle := store.LifecycleActive
	if state == stateResolved {
		lifecycle = store.LifecycleTerminal
	}
	out := store.Classification{
		Kind:          stateFiring,
		Transition:    store.TransitionNone,
		Attention:     store.AttentionActivity,
		Lifecycle:     lifecycle,
		SourceState:   state,
		OccurrenceKey: current.ExternalID + "@" + strconv.FormatInt(current.ObservedAt, 10),
		Summary:       current.Title,
	}
	if previous == nil {
		return out
	}
	switch prev := canonical.State(previous.Payload); {
	case prev != stateResolved && state == stateResolved:
		out.Kind, out.Summary, out.Transition, out.ArchivedReason = stateResolved, "Resolved", store.TransitionEnteredTerminal, stateResolved
	case prev == stateResolved && state != stateResolved:
		out.Kind, out.Summary, out.Transition = stateFiring, "Firing again", store.TransitionLeftTerminal
	default:
		out.Kind, out.Attention = "updated", store.AttentionTrivial
	}
	return out
}

// alertsAbsence marks every alert that left the firing set as resolved. The
// Alertmanager response is the complete firing set, so an absent alert is
// authoritatively resolved, not merely unseen — every verdict is terminal.
type alertsAbsence struct{}

var _ store.AbsenceConfirmer = alertsAbsence{}

func (alertsAbsence) ConfirmAbsence(_ context.Context, previous []store.Observation) (map[string]store.AbsenceVerdict, error) {
	verdicts := make(map[string]store.AbsenceVerdict, len(previous))
	for _, prev := range previous {
		resolved := prev
		resolved.Payload = canonical.WithState(prev.Payload, stateResolved)
		verdicts[prev.ExternalID] = store.AbsenceVerdict{Current: &resolved, Terminal: true}
	}
	return verdicts, nil
}
