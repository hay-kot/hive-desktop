package tmux

import "github.com/hay-kot/hive-desktop/internal/hivecore/core/terminal"

// paneState holds mutable polling state for an agent pane.
type paneState struct {
	paneContent       string
	cachedStatus      terminal.Status
	lastCaptureActive int64
}
