package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
	"github.com/hay-kot/hive-desktop/internal/app/sources/grafana/client"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

type IRMAlertsConfig struct {
	Credential string `json:"credential" yaml:"credential" jsonschema:"title=Credential,description=The connected Grafana stack to fetch as, as 'grafana/<account>'."`
	// Integration is an IRM integration's public primary key. It is the unit a
	// squad's upstream routes hang off, so it is what pins a feed to one
	// squad's alerts.
	Integration string `json:"integration,omitempty" yaml:"integration,omitempty" jsonschema:"title=Integration,description=An IRM integration id to scope to, e.g. 'CFRPV98RPR1U8'. Empty fetches every integration."`
	Team        string `json:"team,omitempty"        yaml:"team,omitempty"        jsonschema:"title=Team,description=An IRM team id to scope to. Empty fetches every team."`
}

func (c *IRMAlertsConfig) Validate() error {
	_, err := c.CredentialRef()
	return err
}

func (c *IRMAlertsConfig) CredentialRef() (credentials.Ref, error) {
	return parseGrafanaRef(c.Credential)
}

// IRMAlertsDescriptor declares the IRM alert groups connector — the sibling of
// the Alertmanager one, over the object on-call actually routes.
//
// It is a distinct connector rather than a mode of sources.grafana_alerts
// because the two see different sets: an alert evaluated in another Mimir and
// POSTed straight to an IRM integration never reaches the stack's Alertmanager,
// and IRM groups related alerts where the Alertmanager lists instances (ADR
// grafana-irm-alert-groups-are-a-sibling-source-node-over-the-oncall-public-api).
var IRMAlertsDescriptor = connector.Descriptor{
	Type:          "sources.grafana_irm_alerts",
	Title:         "Grafana IRM alerts source",
	ProviderTitle: "Grafana",
	Provider:      Provider,
	Mode:          connector.ModePull,
	Stability:     connector.Experimental,
	Capabilities:  connector.CapClassify | connector.CapConfirmAbsence,
	NewConfig:     func() connector.Config { return &IRMAlertsConfig{} },
}

func NewIRMAlertsFactory(fetchers *Fetchers) connector.Factory {
	return connector.Factory{
		New: func(node connector.Node, cfg connector.Config) (connector.Instance, error) {
			config, ok := cfg.(*IRMAlertsConfig)
			if !ok {
				return connector.Instance{}, fmt.Errorf("grafana irm alerts %q: config is %T, want *grafana.IRMAlertsConfig", node.ID(), cfg)
			}
			ref, err := config.CredentialRef()
			if err != nil {
				return connector.Instance{}, fmt.Errorf("grafana irm alerts %q: %w", node.ID(), err)
			}
			return connector.Instance{
				Type: IRMAlertsDescriptor.Type,
				Node: node,
				Metadata: connector.Metadata{
					ProfileID:   node.FlowID,
					SourceKind:  SourceKind,
					SourceScope: ref.Account,
					Policy:      node.Policy,
				},
				Pull: &irmAlertsSource{
					fetcher: fetchers.For(ref),
					query: client.AlertGroupQuery{
						States:        activeAlertGroupStates,
						IntegrationID: strings.TrimSpace(config.Integration),
						TeamID:        strings.TrimSpace(config.Team),
					},
					topic: node.Topic(),
				},
				Classifier: irmAlertsClassifier{},
				Absence:    irmAlertsAbsence{},
				Config:     config,
			}, nil
		},
	}
}

// The lifecycle an alert group is emitted with. Upstream calls a new group
// "new"; it is renamed on the way in so both Grafana source nodes speak one
// vocabulary and a function node can route on `state` without caring which of
// them produced the item.
const (
	stateAcknowledged = "acknowledged"
	stateSilenced     = "silenced"
	upstreamStateNew  = "new"
)

// activeAlertGroupStates is the set a poll asks for. Resolved is deliberately
// absent: a group leaving this set is what mints the resolved item, so fetching
// resolved groups would only re-observe what absence already settled — and the
// upstream default window is the last 30 days, which would drag a month of
// closed alerts into the feed.
var activeAlertGroupStates = []string{upstreamStateNew, stateAcknowledged, stateSilenced}

// irmAlertsSource polls one node's alert groups, emitting one message per group
// keyed by its IRM id so each group maps to its own durable item.
type irmAlertsSource struct {
	fetcher *fetcher
	query   client.AlertGroupQuery
	topic   string
}

var _ connector.PullSource = (*irmAlertsSource)(nil)

// irmAlertPayload is what an alert group emits. title, state and url are
// canonical fields the ingest boundary reads; the rest rides along for a
// function node to route on.
type irmAlertPayload struct {
	Title          string            `json:"title"`
	State          string            `json:"state"`
	URL            string            `json:"url,omitempty"`
	Severity       string            `json:"severity,omitempty"`
	Integration    string            `json:"integration,omitempty"`
	Team           string            `json:"team,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	AlertsCount    int               `json:"alertsCount"`
	CreatedAt      string            `json:"createdAt,omitempty"`
	AcknowledgedAt string            `json:"acknowledgedAt,omitempty"`
	SilencedAt     string            `json:"silencedAt,omitempty"`
}

func (s *irmAlertsSource) Produce(ctx context.Context, emit func(store.Msg) error) error {
	groups, err := s.fetcher.AlertGroups(ctx, s.query)
	if err != nil {
		return fmt.Errorf("grafana irm alerts: %w", err)
	}
	for _, group := range groups {
		body, err := json.Marshal(alertGroupPayload(group))
		if err != nil {
			return fmt.Errorf("grafana irm alerts: encoding %q: %w", group.ID, err)
		}
		if err := emit(store.Msg{Key: group.ID, Topic: s.topic, SourceKind: SourceKind, Payload: body}); err != nil {
			return err
		}
	}
	return nil
}

func alertGroupPayload(group client.AlertGroup) irmAlertPayload {
	labels := group.LabelMap()
	title := strings.TrimSpace(group.Title)
	if title == "" {
		title = "Grafana IRM alert"
	}
	return irmAlertPayload{
		Title:          title,
		State:          alertGroupState(group.State),
		URL:            group.URL(),
		Severity:       labels["severity"],
		Integration:    group.IntegrationID,
		Team:           group.TeamID,
		Labels:         labels,
		AlertsCount:    group.AlertsCount,
		CreatedAt:      group.CreatedAt,
		AcknowledgedAt: group.AcknowledgedAt,
		SilencedAt:     group.SilencedAt,
	}
}

// alertGroupState maps an upstream state onto the emitted vocabulary. An
// unrecognized state passes through rather than being forced to firing: a state
// this build has not seen is better read literally than mislabelled.
func alertGroupState(state string) string {
	normalized := strings.ToLower(strings.TrimSpace(state))
	if normalized == upstreamStateNew || normalized == "" {
		return stateFiring
	}
	return normalized
}

// irmAlertsClassifier maps an alert group's state to lifecycle. Resolved is
// terminal; everything else is active.
//
// Unlike the Alertmanager classifier, a state change between two active states
// is real activity rather than noise: acknowledged means a human picked the
// alert up, which is the triage signal this connector exists to carry. Only a
// re-observation at an unchanged state is trivial.
type irmAlertsClassifier struct{}

var _ store.Classifier = irmAlertsClassifier{}

func (irmAlertsClassifier) Classify(previous *store.Observation, current store.Observation) store.Classification {
	state := alertState(current.Payload)
	lifecycle := store.LifecycleActive
	if state == stateResolved {
		lifecycle = store.LifecycleTerminal
	}
	out := store.Classification{
		Kind:          state,
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
	switch prev := alertState(previous.Payload); {
	case prev != stateResolved && state == stateResolved:
		out.Summary, out.Transition, out.ArchivedReason = "Resolved", store.TransitionEnteredTerminal, stateResolved
	case prev == stateResolved && state != stateResolved:
		out.Summary, out.Transition = "Firing again", store.TransitionLeftTerminal
	case prev != state:
		out.Summary = alertGroupSummary(state)
	default:
		out.Kind, out.Attention = "updated", store.AttentionTrivial
	}
	return out
}

func alertGroupSummary(state string) string {
	switch state {
	case stateAcknowledged:
		return "Acknowledged"
	case stateSilenced:
		return "Silenced"
	case stateFiring:
		return "Firing"
	default:
		return "State changed"
	}
}

// irmAlertsAbsence marks every alert group that left the active set as
// resolved. The listing is the complete active set for the node's scope, so an
// absent group is authoritatively resolved rather than merely unseen — every
// verdict is terminal.
type irmAlertsAbsence struct{}

var _ store.AbsenceConfirmer = irmAlertsAbsence{}

func (irmAlertsAbsence) ConfirmAbsence(_ context.Context, previous []store.Observation) (map[string]store.AbsenceVerdict, error) {
	verdicts := make(map[string]store.AbsenceVerdict, len(previous))
	for _, prev := range previous {
		resolved := prev
		resolved.Payload = withResolvedState(prev.Payload)
		verdicts[prev.ExternalID] = store.AbsenceVerdict{Current: &resolved, Terminal: true}
	}
	return verdicts, nil
}
