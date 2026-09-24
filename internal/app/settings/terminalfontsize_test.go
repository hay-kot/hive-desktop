package settings_test

import (
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/stretchr/testify/require"
)

func TestTerminalFontSizePx(t *testing.T) {
	cases := []struct {
		value string
		want  int
	}{
		{"", settings.DefaultTerminalFontSizePx},
		{"medium", 13},
		{"xxl", 18},
		{"15", 15},
		{"64", 64},
		// Out of range is held to the bounds, not refused.
		{"200", settings.MaxTerminalFontSizePx},
		{"1", settings.MinTerminalFontSizePx},
		{"enormous", settings.DefaultTerminalFontSizePx},
		{"13px", settings.DefaultTerminalFontSizePx},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			require.Equal(t, tc.want, settings.TerminalFontSizePx(tc.value))
		})
	}
}

func TestTerminalFontSizeValue(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		px       int
		want     string
	}{
		{"a named file keeps names", "large", 16, "xl"},
		{"a named file goes numeric off the named sizes", "large", 15, "15"},
		{"a numeric file stays numeric on a named size", "15", 14, "14"},
		{"an unset field writes a number", "", 20, "20"},
		{"an unreadable value writes a number", "enormous", 13, "13"},
		{"a size past the bounds is clamped before it is written", "", 500, "64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, settings.TerminalFontSizeValue(tc.existing, tc.px))
		})
	}
}
