package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
)

const ServiceName = "hive-desktop"

// These four are the resource attributes a backend keeps as a queryable
// dimension; everything else lands in target_info (Prometheus) or structured
// metadata (Loki). service.version is promoted by Prometheus but not by Loki,
// which is why deployment.environment.name separates builds.
const (
	attrServiceName       = "service.name"
	attrServiceVersion    = "service.version"
	attrServiceInstanceID = "service.instance.id"
	attrDeploymentEnvName = "deployment.environment.name"
)

// newResource omits an empty value rather than sending it blank: an empty
// `instance` label is worse than no label.
func newResource(opts Options) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{attribute.String(attrServiceName, ServiceName)}
	for key, value := range map[string]string{
		attrServiceVersion:    opts.Version,
		attrServiceInstanceID: opts.Instance,
		attrDeploymentEnvName: opts.Environment,
	} {
		if value != "" {
			attrs = append(attrs, attribute.String(key, value))
		}
	}
	return resource.Merge(resource.Default(), resource.NewSchemaless(attrs...))
}

func tokenEnvName() string { return credentials.EnvOverrideName(CredentialProvider) }
