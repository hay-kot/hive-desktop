package settings

import "strconv"

// The names appearance.terminal_font_size and
// HIVE_DESKTOP_APPEARANCE_TERMINAL_FONT_SIZE accept beside a pixel count
// (ADR the-terminal-text-size-is-pixels-with-names-as-input).
//
// Input, not a compatibility shim: a name is the readable way to write this by
// hand, so it stays valid.
var terminalFontSizeNames = map[string]int{
	"small":  12,
	"medium": 13,
	"large":  14,
	"xl":     16,
	"xxl":    18,
}

// The lower bound is where a cell stops being legible, the upper where a pane
// stops holding a usable grid. useTerminalFont.ts repeats the range for the
// stepper; this pair is what governs the file.
const (
	MinTerminalFontSizePx     = 8
	MaxTerminalFontSizePx     = 64
	DefaultTerminalFontSizePx = 13
)

// TerminalFontSizePx resolves a persisted value — a name or a pixel count — to
// the size a terminal draws at.
//
// An unusable value reads as the default rather than failing the load: one
// mistyped appearance field is not worth refusing to start over.
func TerminalFontSizePx(value string) int {
	if px, ok := terminalFontSizeNames[value]; ok {
		return px
	}
	px, err := strconv.Atoi(value)
	if err != nil {
		return DefaultTerminalFontSizePx
	}
	return clampTerminalFontSizePx(px)
}

// TerminalFontSizeName is the name for px, when one means exactly it.
func TerminalFontSizeName(px int) (string, bool) {
	for name, size := range terminalFontSizeNames {
		if size == px {
			return name, true
		}
	}
	return "", false
}

// TerminalFontSizeValue is the spelling to persist for px, given what the file
// holds now.
//
// A file spelled with a name keeps names while the size has one, so
// hand-written config survives a trip through the UI. Once the ladder steps off
// the named sizes the file stays numeric: drifting back to a name would rewrite
// a field the user did not touch.
func TerminalFontSizeValue(existing string, px int) string {
	px = clampTerminalFontSizePx(px)
	if _, named := terminalFontSizeNames[existing]; named {
		if name, ok := TerminalFontSizeName(px); ok {
			return name
		}
	}
	return strconv.Itoa(px)
}

func clampTerminalFontSizePx(px int) int {
	return min(max(px, MinTerminalFontSizePx), MaxTerminalFontSizePx)
}
