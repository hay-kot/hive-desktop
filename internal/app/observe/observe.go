// Package observe is the OpenTelemetry API surface the rest of the app calls.
// It holds no SDK: a package that emits gets its Tracer or Meter from the
// global provider, which is the no-op provider until internal/app/telemetry
// registers a real one, and instruments obtained before that registration are
// upgraded in place.
//
// Nothing here abstracts the OTel API. Tracer and Meter return the real
// trace.Tracer and metric.Meter so a call site has every option the API offers;
// what they add is the scope-name convention, which is the only part worth
// centralizing (https://opentelemetry.io/blog/2026/dont-wrap-opentelemetry/).
package observe

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// modulePath prefixes every scope name so a backend groups this app's own
// instrumentation apart from a library's.
const modulePath = "github.com/hay-kot/hive-desktop"

// Tracer returns the tracer for a package, named by its path relative to the
// module root ("/internal/app/store"). Assign it to a package-level var.
func Tracer(pkgpath string) trace.Tracer { return otel.Tracer(modulePath + pkgpath) }

// Meter returns the meter for a package, named the way Tracer names a tracer.
func Meter(pkgpath string) metric.Meter { return otel.Meter(modulePath + pkgpath) }

// Must unwraps an instrument constructor. An instrument's name, unit and
// description are compile-time constants, so an error here is a programming
// mistake the first run catches rather than a runtime condition.
func Must[T any](instrument T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("observe: instrument: %v", err))
	}
	return instrument
}

// RecordError marks a span failed. Without the status a backend shows the span
// as successful with an error event hanging off it, which reads as noise.
func RecordError(span trace.Span, err error, msg ...string) {
	span.RecordError(err)
	if len(msg) > 0 {
		span.SetStatus(codes.Error, msg[0])
		return
	}
	span.SetStatus(codes.Error, err.Error())
}

// StartConditionalSpan starts a span only when ctx already carries a valid one.
// Low-level instrumentation — a SQL query, an HTTP round trip — is worth
// recording as part of the work that caused it and worthless as a root span of
// its own, and a desktop app runs plenty of both with no ambient trace.
func StartConditionalSpan(ctx context.Context, tracer trace.Tracer, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return ctx, trace.SpanFromContext(ctx)
	}
	return tracer.Start(ctx, name, opts...)
}
