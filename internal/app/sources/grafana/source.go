// Package grafana is the Grafana source connector: the config a
// sources.grafana_metrics node carries, the descriptor declaring what the
// connector supports, the instance the producer drains on a tick, and the stack
// auth that connects a stack in the first place.
package grafana

import (
	"fmt"

	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
)

// Provider is the credentials provider every Grafana credential is filed under.
// A ref is "grafana/<host>-<orgID>": the account half is the stack host and the
// org the token authenticates against, so several stacks — and several orgs on
// one stack — stay isolated.
const Provider = "grafana"

// SourceKind is the inbox source_kind Grafana observations carry.
const SourceKind = "grafana"

// MetricsDescriptor declares the metrics connector. It carries no capabilities:
// a metrics poll emits one keyed message per tick and leans on the generic
// observed/updated classifier, so there is no source-side classification,
// absence confirmation, or batched prefetch to wire. It is Experimental because
// each poll appends an ordinary event whose retention is not yet bounded per
// topic — the documented pure-function requirement is the interim guard.
var MetricsDescriptor = connector.Descriptor{
	Type:      "sources.grafana_metrics",
	Title:     "Grafana metrics source",
	Provider:  Provider,
	Mode:      connector.ModePull,
	Stability: connector.Experimental,
	NewConfig: func() connector.Config { return &MetricsConfig{} },
}

// NewMetricsFactory builds the instance half over the per-stack fetcher
// registry. The node's credential is parsed here; the stack URL and token are
// resolved lazily at poll time, so a node naming a not-yet-connected account
// constructs cleanly and simply fetches nothing until it is connected.
func NewMetricsFactory(fetchers *Fetchers) connector.Factory {
	return connector.Factory{
		New: func(node connector.Node, cfg connector.Config) (connector.Instance, error) {
			config, ok := cfg.(*MetricsConfig)
			if !ok {
				return connector.Instance{}, fmt.Errorf("grafana source %q: config is %T, want *grafana.MetricsConfig", node.ID(), cfg)
			}
			ref, err := config.CredentialRef()
			if err != nil {
				return connector.Instance{}, fmt.Errorf("grafana source %q: %w", node.ID(), err)
			}
			return connector.Instance{
				Type: MetricsDescriptor.Type,
				Node: node,
				Metadata: connector.Metadata{
					ProfileID:  node.FlowID,
					SourceKind: SourceKind,
					// The account distinguishes several Grafana sources within
					// one flow, which is what SourceScope is for.
					SourceScope: ref.Account,
					Policy:      node.Policy,
				},
				Pull: &metricsSource{
					fetcher: fetchers.For(ref),
					dsUID:   config.DatasourceUID,
					expr:    config.Expr,
					title:   config.Title,
					topic:   node.Topic(),
					key:     node.NodeID,
				},
				Config: config,
			}, nil
		},
	}
}
