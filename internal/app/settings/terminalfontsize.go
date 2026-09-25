package settings

// The lower bound is where a cell stops being legible, the upper where a pane
// stops holding a usable grid. useTerminalFont.ts repeats the range for the
// stepper; this pair is what governs the file.
const (
	MinTerminalFontSizePx     = 8
	MaxTerminalFontSizePx     = 64
	DefaultTerminalFontSizePx = 13
)

// TerminalFontSizePx is the size a terminal draws at for a persisted value.
// Out-of-range values are clamped rather than rejected: one bad appearance
// field is not worth refusing to start over.
func TerminalFontSizePx(px int) int {
	if px == 0 {
		return DefaultTerminalFontSizePx
	}
	return min(max(px, MinTerminalFontSizePx), MaxTerminalFontSizePx)
}
