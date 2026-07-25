package wailsui

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// TestParseOffset covers the one genuinely new error path phase 2 creates in
// this adapter. The int64↔string encoding used to live inside the core
// service, where nothing tested it either; relocating it here is exactly the
// kind of move that drops a parse-error path silently.
func TestParseOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want int64
		ok   bool
	}{
		{name: "zero", raw: "0", want: 0, ok: true},
		{name: "typical", raw: "4096", want: 4096, ok: true},
		{name: "max int64 round-trips", raw: strconv.FormatInt(math.MaxInt64, 10), want: math.MaxInt64, ok: true},
		{name: "negative is rejected rather than coerced", raw: "-1"},
		{name: "non-numeric is rejected rather than coerced to 0", raw: "tail"},
		{name: "empty is rejected", raw: ""},
		{name: "float is rejected", raw: "12.5"},
		{name: "overflow is rejected", raw: "9223372036854775808"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseOffset(tt.raw, "event log tail")
			if tt.ok {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
				return
			}
			require.Error(t, err)
			assert.Equal(t, app.KindInvalid, app.KindOf(err), "a malformed offset is the caller's mistake")
			assert.Zero(t, got)
		})
	}
}

// TestEventLogTailOffsetEncoding proves the round trip the frontend depends
// on: the core deals in int64, and JavaScript cannot hold one, so the wire
// carries a decimal string.
func TestEventLogTailOffsetEncoding(t *testing.T) {
	t.Parallel()

	encoded := strconv.FormatInt(math.MaxInt64, 10)
	decoded, err := parseOffset(encoded, "event log tail")
	require.NoError(t, err)
	assert.Equal(t, int64(math.MaxInt64), decoded, "the encoding must not lose precision at the boundary JS would")
}
