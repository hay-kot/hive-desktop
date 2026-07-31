package ptyterm

import "time"

// Event is one item on a session's outbound stream. The vocabulary is
// deliberately the one tmuxcc emits: both backends serve the same wire frames,
// so the frontend renders either without knowing which is behind it.
type Event interface{ isEvent() }

// WindowEventKind is the tab-set change vocabulary.
type WindowEventKind string

const (
	WindowAdded         WindowEventKind = "added"
	WindowClosed        WindowEventKind = "closed"
	WindowRenamed       WindowEventKind = "renamed"
	WindowActiveChanged WindowEventKind = "active-changed"
	WindowResized       WindowEventKind = "resized"
)

// LifecycleKind is the connection-state vocabulary.
type LifecycleKind string

const (
	LifecycleAttached LifecycleKind = "attached"
	LifecycleExited   LifecycleKind = "exited"
	LifecycleError    LifecycleKind = "error"
)

// Window is one tab: a PTY with a shell on the far end. It has no panes, so
// nothing here distinguishes a window from the thing rendering inside it.
type Window struct {
	ID     string
	Name   string
	Active bool
	Width  int
	Height int
}

// WindowChanged reports a change to the tab set.
type WindowChanged struct {
	Kind   WindowEventKind
	Window Window
}

// Output is one window's bytes, straight off the PTY. At is the read
// timestamp a transport measures send latency against.
type Output struct {
	At       time.Time
	WindowID string
	Data     []byte
}

// LifecycleChanged reports attach/exit/error. There is no paused/resumed pair:
// a PTY has no flow-control handshake to report, and a slow consumer is
// answered by coalescing rather than by pausing the producer.
type LifecycleChanged struct {
	Kind     LifecycleKind
	WindowID string
	Message  string
}

func (WindowChanged) isEvent()    {}
func (Output) isEvent()           {}
func (LifecycleChanged) isEvent() {}
