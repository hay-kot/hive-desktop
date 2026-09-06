package sourcehttp

import (
	"go.opentelemetry.io/otel/metric"

	"github.com/hay-kot/hive-desktop/internal/app/observe"
)

var meter = observe.Meter("/internal/app/sources/sourcehttp")

// rateLimitRemaining is the number a polling app most needs and the one this
// package was already throwing away into a debug log line. It is a gauge rather
// than a counter because a provider's window resets: the useful reading is the
// last one, not a rate.
//
// The source name is the only attribute. It is a bounded set — one value per
// configured connector — which is what separates it from the request URL.
var rateLimitGauge = observe.Must(meter.Int64Gauge(
	"source.ratelimit.remaining",
	metric.WithDescription("Requests left in the provider's rate-limit window, as of the last response that reported one."),
))
