package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
)

const (
	// maxBodyBytes truncates a log body. Loki rejects a line over 256 KB, and
	// a desktop log message that large is a dump rather than a message, so the
	// cap sits well under the ceiling instead of at it.
	maxBodyBytes = 32 << 10

	// maxAttrs bounds one record's attributes. Grafana Cloud truncates past
	// its own limits silently; dropping the tail here is at least visible in
	// the code.
	maxAttrs = 64

	// maxAttrValueBytes matches Grafana Cloud's own attribute-value ceiling,
	// so a value is cut here rather than silently on ingest.
	maxAttrValueBytes = 2048
)

// logWriter is a zerolog writer arm that re-emits each encoded event as an
// OTLP log record.
//
// It taps the writer chain rather than a zerolog.Hook because a Hook is handed
// only the level and the message — the accumulated fields are not readable
// from *zerolog.Event. Every writer in a MultiLevelWriter, by contrast,
// receives the same encoded JSON event, which is what the app's two
// ConsoleWriter arms parse to pretty-print. So does this one.
type logWriter struct {
	emit emitter
}

// emitter is a log emit with its context already bound, the way
// credentials.Bind binds a Resolver over a ref. An io.Writer has no context
// parameter, so the alternative is storing one on the writer; a closure built
// where a context is in scope says the same thing without the field.
//
// The bound context is detached from the app's cancellation on purpose: the
// lines written during shutdown, the ones explaining why, are the last thing
// that should be dropped.
type emitter func(otellog.Record)

func bindEmitter(ctx context.Context, logger otellog.Logger) emitter {
	ctx = context.WithoutCancel(ctx)
	return func(rec otellog.Record) { logger.Emit(ctx, rec) }
}

func newLogWriter(ctx context.Context, logger otellog.Logger) io.Writer {
	return &logWriter{emit: bindEmitter(ctx, logger)}
}

// Write always reports the full length and never returns an error. This arm is
// a tap: a record that cannot be parsed or emitted must not surface as a
// logging failure at the call site, which would be an error about an error.
func (w *logWriter) Write(p []byte) (int, error) {
	w.forward(p)
	return len(p), nil
}

func (w *logWriter) forward(p []byte) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(p, &fields); err != nil {
		return
	}

	var rec otellog.Record
	rec.SetObservedTimestamp(time.Now())

	if raw, ok := fields[zerolog.TimestampFieldName]; ok {
		if s, err := decodeString(raw); err == nil {
			if ts, err := time.Parse(zerolog.TimeFieldFormat, s); err == nil {
				rec.SetTimestamp(ts)
			}
		}
	}

	level := ""
	if raw, ok := fields[zerolog.LevelFieldName]; ok {
		level, _ = decodeString(raw)
	}
	rec.SetSeverity(severity(level))
	if level != "" {
		rec.SetSeverityText(level)
	}

	body := ""
	if raw, ok := fields[zerolog.MessageFieldName]; ok {
		body, _ = decodeString(raw)
	}
	rec.SetBody(attribute.StringValue(truncate(body, maxBodyBytes)))

	attrs := make([]attribute.KeyValue, 0, len(fields))
	for key, raw := range fields {
		switch key {
		case zerolog.TimestampFieldName, zerolog.LevelFieldName, zerolog.MessageFieldName:
			continue
		}
		if len(attrs) == maxAttrs {
			break
		}
		attrs = append(attrs, attribute.KeyValue{Key: attribute.Key(key), Value: attrValue(raw)})
	}
	if len(attrs) > 0 {
		rec.AddAttributes(attrs...)
	}

	w.emit(rec)
}

// attrValue keeps a JSON scalar as its own type and renders anything
// structured as its JSON text. A nested object is rare in this app's logs and
// is more useful readable than dropped.
func attrValue(raw json.RawMessage) attribute.Value {
	var any any
	if err := json.Unmarshal(raw, &any); err != nil {
		return attribute.StringValue(truncate(string(raw), maxAttrValueBytes))
	}
	switch v := any.(type) {
	case string:
		return attribute.StringValue(truncate(v, maxAttrValueBytes))
	case bool:
		return attribute.BoolValue(v)
	case float64:
		// zerolog writes integers without a fraction; keep them integral so a
		// backend does not render counts as 3.0.
		if v == float64(int64(v)) {
			return attribute.Int64Value(int64(v))
		}
		return attribute.Float64Value(v)
	case nil:
		return attribute.StringValue("")
	default:
		return attribute.StringValue(truncate(string(raw), maxAttrValueBytes))
	}
}

func decodeString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated " + strconv.Itoa(len(s)-max) + " bytes)"
}

// severity maps zerolog's level names onto the OTel severity scale. An
// unrecognised level is Info rather than unspecified: a record with no
// severity sorts unpredictably in a backend, which is worse than one filed a
// notch off.
func severity(level string) otellog.Severity {
	switch level {
	case zerolog.LevelTraceValue:
		return otellog.SeverityTrace
	case zerolog.LevelDebugValue:
		return otellog.SeverityDebug
	case zerolog.LevelInfoValue:
		return otellog.SeverityInfo
	case zerolog.LevelWarnValue:
		return otellog.SeverityWarn
	case zerolog.LevelErrorValue:
		return otellog.SeverityError
	case zerolog.LevelFatalValue:
		return otellog.SeverityFatal
	case zerolog.LevelPanicValue:
		return otellog.SeverityFatal2
	default:
		return otellog.SeverityInfo
	}
}
