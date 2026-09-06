package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
)

// ServiceName is this app's service.name on every signal.
const ServiceName = "hive-desktop"

// The four resource attributes below are not an arbitrary selection. A backend
// keeps only some resource attributes as queryable dimensions and files the
// rest away — Prometheus puts the rest in a target_info metric you have to
// join against, and Loki puts them in structured metadata rather than the
// stream selector. These four are the ones that survive as dimensions, so they
// are the ones telemetry can be grouped by:
//
//	service.name                 -> job (Prometheus), service_name (Loki)
//	service.instance.id          -> instance (Prometheus), service_instance_id (Loki)
//	deployment.environment.name  -> a label on both
//	service.version              -> a label on Prometheus ONLY
//
// service.version being absent from Loki's promoted set is why the release
// channel, not the version, is the primary separator: it is the only one of
// the two that selects a log stream. Filtering logs by version still works,
// but as a structured-metadata filter after the stream selector:
//
//	{service_name="hive-desktop", deployment_environment_name="dev"} | service_version="1.4.0"
const (
	attrServiceName       = "service.name"
	attrServiceVersion    = "service.version"
	attrServiceInstanceID = "service.instance.id"
	attrDeploymentEnvName = "deployment.environment.name"
)

// newResource builds the identity every signal carries. An empty value is
// omitted rather than sent blank, because an empty attribute still becomes a
// label — an empty `instance` is worse than no `instance`.
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
	// Default carries telemetry.sdk.*; ours wins on any key it also sets.
	return resource.Merge(resource.Default(), resource.NewSchemaless(attrs...))
}

// tokenEnvName is the environment variable the OTLP token is read from. It is
// derived from the credential provider rather than written out, so the name
// cannot drift from the one a stored credential would use.
func tokenEnvName() string { return credentials.EnvOverrideName(CredentialProvider) }
