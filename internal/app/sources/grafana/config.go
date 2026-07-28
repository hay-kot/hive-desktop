package grafana

import (
	"fmt"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
)

// MetricsConfig is a Grafana metrics source node's configuration. The framework
// decodes it strictly and calls Validate for the cross-field rules a schema
// cannot state.
//
// credential is a direct field, not one promoted from an embedded base: the
// provider-enforcement test scans a config's own tagged fields for it, and an
// embedded field would be invisible there. There is deliberately no url field —
// the stack URL is bound to the account at connect time, so a node cannot pair
// an account's token with an arbitrary host.
type MetricsConfig struct {
	// Credential names the stack this source fetches as, "grafana/<account>".
	// A ref and never a token: flows/ is dotfiles-managed, so an embedded token
	// would be a token in a git repo.
	Credential string `json:"credential" yaml:"credential" jsonschema:"title=Credential,description=The connected Grafana stack to fetch as, as 'grafana/<account>'."`
	// DatasourceUID selects which Prometheus-compatible datasource the query
	// runs against, by its stable uid.
	DatasourceUID string `json:"datasource_uid" yaml:"datasource_uid" jsonschema:"title=Datasource UID,description=The uid of the Prometheus-compatible datasource to query."`
	// Expr is the PromQL query run once per poll.
	Expr string `json:"expr" yaml:"expr" jsonschema:"title=Query,description=A PromQL expression, e.g. 'up' or 'sum(rate(http_requests_total[5m]))'."`
	// Title is the feed item's title. Optional; empty falls back to the query.
	Title string `json:"title,omitempty" yaml:"title,omitempty" jsonschema:"title=Title,description=The feed item's title. Defaults to the query when empty."`
}

// Validate rejects a config a metrics poll could not run: a missing or
// wrong-provider credential, no datasource, or no query.
func (c *MetricsConfig) Validate() error {
	if _, err := c.CredentialRef(); err != nil {
		return err
	}
	if strings.TrimSpace(c.DatasourceUID) == "" {
		return fmt.Errorf("grafana source: datasource_uid is required")
	}
	if strings.TrimSpace(c.Expr) == "" {
		return fmt.Errorf("grafana source: expr is required")
	}
	return nil
}

// CredentialRef is the parsed credential ref. A ref naming another provider is
// rejected here rather than at fetch time: "github/octocat" on a Grafana source
// is a config mistake, and failing it at load says so.
func (c *MetricsConfig) CredentialRef() (credentials.Ref, error) {
	return parseGrafanaRef(c.Credential)
}

// parseGrafanaRef parses a "grafana/<account>" credential ref, rejecting a
// missing ref or one that names another provider. Shared by every Grafana
// connector's config so the provider check is written once.
func parseGrafanaRef(credential string) (credentials.Ref, error) {
	if strings.TrimSpace(credential) == "" {
		return credentials.Ref{}, fmt.Errorf("grafana source: credential is required (e.g. %q)", Provider+"/grafana.example.com-1")
	}
	ref, err := credentials.ParseRef(credential)
	if err != nil {
		return credentials.Ref{}, fmt.Errorf("grafana source: %w", err)
	}
	if ref.Provider != Provider {
		return credentials.Ref{}, fmt.Errorf("grafana source: credential %q is not a %s credential", credential, Provider)
	}
	return ref, nil
}
