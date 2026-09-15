package configwatch

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hay-kot/hive-desktop/internal/app/configstate"
	"github.com/hay-kot/hive-desktop/internal/app/observe"
)

var (
	meter     = observe.Meter("/internal/app/configwatch")
	watchMode = observe.Must(meter.Int64Gauge(
		"config.watch.mode",
		metric.WithDescription("Configuration detection mode by source."),
	))
	watchErrors = observe.Must(meter.Int64Counter(
		"config.watch.error",
		metric.WithDescription("Configuration watcher errors by bounded kind."),
	))
)

func recordMode(ctx context.Context, source configstate.Source, mode string) {
	for _, candidate := range []string{"notify", "poll", "unavailable"} {
		value := int64(0)
		if candidate == mode {
			value = 1
		}
		watchMode.Record(ctx, value, metric.WithAttributes(
			attribute.String("source", string(source)),
			attribute.String("mode", candidate),
		))
	}
}

type watchErrorKind string

const (
	watchErrorOverflow     watchErrorKind = "overflow"
	watchErrorRegistration watchErrorKind = "registration"
	watchErrorEventStream  watchErrorKind = "event_stream"
	watchErrorScan         watchErrorKind = "scan"
)

func recordWatchError(ctx context.Context, kind watchErrorKind) {
	watchErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", string(kind))))
}
