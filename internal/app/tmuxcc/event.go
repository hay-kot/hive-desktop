package tmuxcc

import "time"

// Event is one item on a client's outbound stream.
type Event interface{ isEvent() }

// WindowEventKind is the tab-set change vocabulary. The values are the wire
// strings the WebSocket adapter emits verbatim.
type WindowEventKind string

const (
	WindowAdded         WindowEventKind = "added"
	WindowClosed        WindowEventKind = "closed"
	WindowRenamed       WindowEventKind = "renamed"
	WindowActiveChanged WindowEventKind = "active-changed"
	WindowResized       WindowEventKind = "resized"
)

// LifecycleKind is the connection-state vocabulary, wire strings likewise.
type LifecycleKind string

const (
	LifecycleAttached LifecycleKind = "attached"
	LifecyclePaused   LifecycleKind = "paused"
	LifecycleResumed  LifecycleKind = "resumed"
	LifecycleExited   LifecycleKind = "exited"
	LifecycleError    LifecycleKind = "error"
)

// WindowChanged reports a change to the tab set.
type WindowChanged struct {
	Kind   WindowEventKind
	Window Window
}

// Output is one window's decoded bytes, always from the window's active pane —
// non-active-pane output is drained and dropped before it reaches the stream.
// PaneID rides along so the deferred panes phase is additive. At is the decode
// timestamp the WebSocket adapter measures send latency against.
type Output struct {
	At       time.Time
	WindowID string
	PaneID   string
	Data     []byte
}

// LifecycleChanged reports attach/pause/resume/exit/error. WindowID is set for
// per-window pause/resume; Message carries an exit reason or error text.
type LifecycleChanged struct {
	Kind     LifecycleKind
	WindowID string
	Message  string
}

func (WindowChanged) isEvent()    {}
func (Output) isEvent()           {}
func (LifecycleChanged) isEvent() {}
