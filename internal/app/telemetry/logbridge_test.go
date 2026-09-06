package telemetry

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// captureExporter records what the bridge emits so a test can assert on the
// record rather than on the JSON that produced it.
type captureExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *captureExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range records {
		e.records = append(e.records, records[i].Clone())
	}
	return nil
}

func (e *captureExporter) Shutdown(context.Context) error   { return nil }
func (e *captureExporter) ForceFlush(context.Context) error { return nil }

func (e *captureExporter) all() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]sdklog.Record(nil), e.records...)
}

// newBridge returns a zerolog logger whose only writer arm is the bridge, plus
// the exporter it lands in.
func newBridge(t *testing.T) (zerolog.Logger, *captureExporter) {
	t.Helper()

	exp := &captureExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exp)))
	t.Cleanup(func() { _ = lp.Shutdown(context.WithoutCancel(t.Context())) })

	w := newLogWriter(t.Context(), lp.Logger(ScopeName))
	return zerolog.New(w).With().Timestamp().Logger().Level(zerolog.TraceLevel), exp
}

func attrs(r sdklog.Record) map[string]attribute.Value {
	out := map[string]attribute.Value{}
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		out[string(kv.Key)] = kv.Value
		return true
	})
	return out
}

// The whole reason the bridge is a writer arm and not a zerolog.Hook: a Hook
// cannot read the event's fields, and the fields are most of the value.
func TestBridgeCarriesFields(t *testing.T) {
	logger, exp := newBridge(t)

	logger.Warn().
		Str("path", "/api/terminal").
		Int("count", 3).
		Bool("mounted", true).
		Float64("ratio", 1.5).
		Msg("terminal WebSocket stream mounted")

	records := exp.all()
	require.Len(t, records, 1)
	rec := records[0]

	assert.Equal(t, "terminal WebSocket stream mounted", rec.Body().AsString())
	assert.Equal(t, otellog.SeverityWarn, rec.Severity())
	assert.Equal(t, "warn", rec.SeverityText())
	assert.False(t, rec.Timestamp().IsZero(), "the event's own timestamp should be carried, not just the observed one")

	got := attrs(rec)
	assert.Equal(t, "/api/terminal", got["path"].AsString())
	assert.Equal(t, int64(3), got["count"].AsInt64())
	assert.True(t, got["mounted"].AsBool())
	assert.InDelta(t, 1.5, got["ratio"].AsFloat64(), 0.0001)

	// The three fields that became first-class record properties must not also
	// show up as attributes.
	for _, key := range []string{zerolog.LevelFieldName, zerolog.TimestampFieldName, zerolog.MessageFieldName} {
		assert.NotContains(t, got, key)
	}
}

// zerolog writes whole numbers without a fraction; a count should not come out
// the far side as 3.0.
func TestBridgeKeepsIntegersIntegral(t *testing.T) {
	logger, exp := newBridge(t)
	logger.Info().Int64("bytes", 4096).Msg("streamed")

	records := exp.all()
	require.Len(t, records, 1)
	assert.Equal(t, attribute.INT64, attrs(records[0])["bytes"].Type())
}

func TestBridgeMapsSeverity(t *testing.T) {
	tests := []struct {
		level string
		want  otellog.Severity
	}{
		{zerolog.LevelTraceValue, otellog.SeverityTrace},
		{zerolog.LevelDebugValue, otellog.SeverityDebug},
		{zerolog.LevelInfoValue, otellog.SeverityInfo},
		{zerolog.LevelWarnValue, otellog.SeverityWarn},
		{zerolog.LevelErrorValue, otellog.SeverityError},
		{zerolog.LevelFatalValue, otellog.SeverityFatal},
		{zerolog.LevelPanicValue, otellog.SeverityFatal2},
		// An unrecognised level is filed as Info rather than left unspecified,
		// which would sort unpredictably in a backend.
		{"mystery", otellog.SeverityInfo},
		{"", otellog.SeverityInfo},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			assert.Equal(t, tt.want, severity(tt.level))
		})
	}
}

// A tap on the log pipeline must never fail the write it is observing: an
// error here would be an error about an error.
func TestBridgeSwallowsUnparseableWrites(t *testing.T) {
	exp := &captureExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exp)))
	t.Cleanup(func() { _ = lp.Shutdown(context.WithoutCancel(t.Context())) })

	w := newLogWriter(t.Context(), lp.Logger(ScopeName))

	n, err := w.Write([]byte("not json at all"))
	require.NoError(t, err)
	assert.Equal(t, len("not json at all"), n)
	assert.Empty(t, exp.all())
}

func TestBridgeTruncatesOversizedBody(t *testing.T) {
	logger, exp := newBridge(t)
	logger.Info().Msg(strings.Repeat("x", maxBodyBytes+512))

	records := exp.all()
	require.Len(t, records, 1)

	body := records[0].Body().AsString()
	assert.Less(t, len(body), maxBodyBytes+512)
	assert.Contains(t, body, "truncated")
}

func TestBridgeBoundsAttributeCount(t *testing.T) {
	logger, exp := newBridge(t)

	event := logger.Info()
	for i := range maxAttrs + 20 {
		event = event.Int("f"+strconv.Itoa(i), i)
	}
	event.Msg("wide")

	records := exp.all()
	require.Len(t, records, 1)
	assert.Equal(t, maxAttrs, records[0].AttributesLen())
}

func TestBridgeParsesEventTimestamp(t *testing.T) {
	logger, exp := newBridge(t)

	before := time.Now().Add(-time.Second)
	logger.Info().Msg("now")

	records := exp.all()
	require.Len(t, records, 1)
	assert.True(t, records[0].Timestamp().After(before))
	assert.False(t, records[0].ObservedTimestamp().IsZero())
}
