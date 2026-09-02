package schedule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCron(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		ok   bool
	}{
		{"five fields", "0 9 * * 5", true},
		{"every minute", "* * * * *", true},
		{"leading and trailing space", "  0 9 * * 5  ", true},
		{"step", "*/15 * * * *", true},
		{"named weekday", "0 9 * * MON", true},
		{"hourly descriptor", "@hourly", true},
		{"daily descriptor", "@daily", true},
		{"weekly descriptor", "@weekly", true},
		{"monthly descriptor", "@monthly", true},
		{"every duration", "@every 1h30m", true},
		{"empty", "", false},
		{"only spaces", "   ", false},
		{"six fields is not standard cron", "0 0 9 * * 5", false},
		{"four fields", "0 9 * *", false},
		{"minute out of range", "60 9 * * 5", false},
		{"unknown descriptor", "@fortnightly", false},
		{"gibberish", "not a cron", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sched, err := ParseCron(tt.expr)
			if !tt.ok {
				require.Error(t, err)
				assert.Nil(t, sched)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, sched)
		})
	}
}

func TestNextOccurrences(t *testing.T) {
	t.Parallel()

	// A Wednesday, so "every Friday at 09:00" has two clear days to run.
	after := time.Date(2026, time.September, 2, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		expr string
		n    int
		want []time.Time
	}{
		{
			name: "weekly",
			expr: "0 9 * * 5",
			n:    2,
			want: []time.Time{
				time.Date(2026, time.September, 4, 9, 0, 0, 0, time.UTC),
				time.Date(2026, time.September, 11, 9, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "hourly descriptor",
			expr: "@hourly",
			n:    2,
			want: []time.Time{
				time.Date(2026, time.September, 2, 11, 0, 0, 0, time.UTC),
				time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "zero count",
			expr: "@hourly",
			n:    0,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NextOccurrences(tt.expr, after, tt.n)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestNextOccurrencesIsStrictlyAfter pins the boundary the planner depends on:
// an occurrence exactly at `after` has already been evaluated, so returning it
// would re-fire the run that just happened.
func TestNextOccurrencesIsStrictlyAfter(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.September, 4, 9, 0, 0, 0, time.UTC)
	got, err := NextOccurrences("0 9 * * 5", at, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, time.Date(2026, time.September, 11, 9, 0, 0, 0, time.UTC), got[0])
}

func TestNextOccurrencesRejectsABadExpression(t *testing.T) {
	t.Parallel()

	_, err := NextOccurrences("not a cron", time.Now(), 3)
	require.Error(t, err)
}
