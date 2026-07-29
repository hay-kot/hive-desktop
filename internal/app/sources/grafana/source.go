// Package grafana is the Grafana source connector: the metrics and alerts source
// nodes and the stack auth that connects a stack.
package grafana

import (
	"fmt"

	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
)

// Provider is the credentials provider every Grafana credential is filed under.
// A ref is "grafana/<host>-<orgID>", so several orgs on one stack stay isolated.
const Provider = "grafana"

const SourceKind = "grafana"

// MetricsDescriptor declares the metrics connector. It carries no capabilities:
// a metrics poll emits one keyed message per tick and leans on the generic
// classifier. Experimental because per-topic retention is not yet bounded.
var MetricsDescriptor = connector.Descriptor{
	Type:          "sources.grafana_metrics",
	Title:         "Grafana metrics source",
	ProviderTitle: "Grafana",
	Provider:      Provider,
	Mode:          connector.ModePull,
	Stability:     connector.Experimental,
	NewConfig:     func() connector.Config { return &MetricsConfig{} },
}

// NewMetricsFactory builds the instance half over the per-stack fetcher
// registry. The credential is resolved lazily at poll time, so a node naming a
// not-yet-connected account still constructs cleanly.
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
					ProfileID:   node.FlowID,
					SourceKind:  SourceKind,
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
