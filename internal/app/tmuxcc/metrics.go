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

	// Boundaries climb to defaultBufferBytes, the broker's own bound. The SDK
	// default tops out at 10 KB, which puts every interesting backlog in the
	// overflow bucket and makes a quantile report the bound rather than the
	// depth.
	bufferDepth = observe.Must(meter.Int64Histogram(
		"tmux.stream.buffer.depth",
		metric.WithDescription("Broker backlog depth measured after a publish."),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(
			1<<10, 4<<10, 16<<10, 64<<10, 256<<10, 1<<20, 2<<20, 4<<20, 8<<20,
		),
	))

	lifecycleTransitions = observe.Must(meter.Int64Counter(
		"tmux.stream.lifecycle",
		metric.WithDescription("Stream pause and resume transitions."),
	))

	// Boundaries are seconds, and this path is measured in tens of
	// microseconds. The SDK default starts at 5, so every observation lands in
	// the first bucket and a quantile interpolates across it -- a 92us p99
	// reports as 4.95s. Explicit boundaries are not optional on a histogram
	// whose unit is seconds.
	frameLatency = observe.Must(meter.Float64Histogram(
		"tmux.stream.frame.latency",
		metric.WithDescription("Delay from tmux %output decode to the frame reaching the transport."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1,
		),
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
