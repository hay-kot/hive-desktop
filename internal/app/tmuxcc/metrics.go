package tmuxcc

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hay-kot/hive-desktop/internal/app/observe"
)

// The data plane's instruments.
//
// None of them is labelled by session slug or window id. Both are per-user and
// effectively unbounded, and a metric attribute is where that cost is paid on
// every export for as long as the series lives. Which session streamed the
// bytes is a log or a span question; how much this install streams is the
// metric question.
var (
	meter = observe.Meter("/internal/app/tmuxcc")

	streamedBytes = observe.Must(meter.Int64Counter(
		"tmux.stream.bytes",
		metric.WithDescription("Bytes decoded from tmux %output and forwarded to the broker."),
		metric.WithUnit("By"),
	))

	bufferDepth = observe.Must(meter.Int64Histogram(
		"tmux.stream.buffer.depth",
		metric.WithDescription("Broker backlog depth measured after a publish."),
		metric.WithUnit("By"),
	))

	lifecycleTransitions = observe.Must(meter.Int64Counter(
		"tmux.stream.lifecycle",
		metric.WithDescription("Stream pause and resume transitions."),
	))

	frameLatency = observe.Must(meter.Float64Histogram(
		"tmux.stream.frame.latency",
		metric.WithDescription("Delay from tmux %output decode to the frame reaching the transport."),
		metric.WithUnit("s"),
	))
)

// ObserveFrameLatency records how long an output frame took to travel from this
// package's decode to the wire. Only the transport knows when the send
// happened, so it is the one that measures and reports it.
func ObserveFrameLatency(ctx context.Context, d time.Duration) {
	frameLatency.Record(ctx, d.Seconds())
}

// Built once rather than per call: metric.WithAttributes allocates, and these
// two are the whole domain of the attribute.
var (
	statePaused  = metric.WithAttributes(attribute.String("state", "paused"))
	stateResumed = metric.WithAttributes(attribute.String("state", "resumed"))
)
