package ptyterm

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

// sessionOptions is what every window of a session is spawned from.
type sessionOptions struct {
	Dir         string
	Command     []string
	Env         []string
	Cols        int
	Rows        int
	ReplayBytes int
	BufferBytes int
}

// session is a named set of tabs and the one event stream they share. It exists
// only in this process: nothing survives the app, which is the whole difference
// being measured against tmux.
type session struct {
	name   string
	opts   sessionOptions
	events *broker

	// replayMu orders ring writes against replay snapshots. It is separate from
	// mu because it is held across a publish, and mu is taken inside it.
	replayMu sync.Mutex

	mu       sync.Mutex
	windows  map[string]*ptyWindow
	order    []string
	activeID string
	nextID   int
	closed   bool

	onEmpty func(name string)
}

func newSession(name string, opts sessionOptions, onEmpty func(string)) (*session, error) {
	s := &session{
		name:    name,
		opts:    opts,
		events:  newBroker(opts.BufferBytes),
		windows: map[string]*ptyWindow{},
		onEmpty: onEmpty,
	}
	if _, err := s.newWindow(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *session) newWindow() (string, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrNotAttached, s.name)
	}
	s.nextID++
	id := fmt.Sprintf("w%d", s.nextID)
	opts := windowOptions{
		Name:        defaultWindowName(s.opts.Command),
		Dir:         s.opts.Dir,
		Command:     s.opts.Command,
		Env:         s.opts.Env,
		Cols:        s.opts.Cols,
		Rows:        s.opts.Rows,
		ReplayBytes: s.opts.ReplayBytes,
	}
	s.mu.Unlock()

	win, err := spawnWindow(id, opts, s.publishOutput, s.windowExited)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		win.close()
		return "", fmt.Errorf("%w: %s", ErrNotAttached, s.name)
	}
	s.windows[id] = win
	s.order = append(s.order, id)
	s.mu.Unlock()

	s.events.Publish(WindowChanged{Kind: WindowAdded, Window: win.snapshot()})
	s.selectWindow(id)
	return id, nil
}

// publishOutput records a chunk in the window's ring and puts it on the stream
// as one step. Holding replayMu across both is what makes a replay coherent: a
// chunk is either in the snapshot a subscriber opens with or on the stream
// after it, never both and never out of order.
func (s *session) publishOutput(win *ptyWindow, at time.Time, data []byte) {
	s.replayMu.Lock()
	defer s.replayMu.Unlock()
	_, _ = win.ring.Write(data)
	s.events.Publish(Output{At: at, WindowID: win.id, Data: data})
}

// windowExited retires a window whose process left. The last one taking the
// session with it is what makes closing the final tab the same gesture as
// closing the terminal, exactly as it is in tmux.
func (s *session) windowExited(windowID, reason string) {
	s.mu.Lock()
	win, ok := s.windows[windowID]
	if !ok {
		s.mu.Unlock()
		return
	}
	delete(s.windows, windowID)
	s.order = slices.DeleteFunc(s.order, func(id string) bool { return id == windowID })
	empty := len(s.windows) == 0
	closed := s.closed
	next := ""
	if s.activeID == windowID && len(s.order) > 0 {
		next = s.order[0]
	}
	s.mu.Unlock()

	s.events.Publish(WindowChanged{Kind: WindowClosed, Window: win.snapshot()})
	if next != "" {
		s.selectWindow(next)
	}
	if empty && !closed {
		s.events.Publish(LifecycleChanged{Kind: LifecycleExited, WindowID: windowID, Message: reason})
		if s.onEmpty != nil {
			s.onEmpty(s.name)
		}
	}
}

func (s *session) closeWindow(windowID string) error {
	win, err := s.window(windowID)
	if err != nil {
		return err
	}
	// The reader goroutine reports the close through windowExited, so the tab
	// set is retired by exactly one path whether a window was closed here or
	// exited on its own.
	go win.close()
	return nil
}

func (s *session) renameWindow(windowID, name string) error {
	win, err := s.window(windowID)
	if err != nil {
		return err
	}
	win.rename(name)
	s.events.Publish(WindowChanged{Kind: WindowRenamed, Window: win.snapshot()})
	return nil
}

func (s *session) selectWindow(windowID string) {
	s.mu.Lock()
	if _, ok := s.windows[windowID]; !ok {
		s.mu.Unlock()
		return
	}
	s.activeID = windowID
	windows := s.windowsLocked()
	s.mu.Unlock()

	for _, win := range windows {
		win.setActive(win.id == windowID)
	}
	if win, err := s.window(windowID); err == nil {
		s.events.Publish(WindowChanged{Kind: WindowActiveChanged, Window: win.snapshot()})
	}
}

func (s *session) write(windowID string, p []byte) error {
	win, err := s.window(windowID)
	if err != nil {
		return err
	}
	return win.write(p)
}

// resize sizes every window, not just the active one. One pane box renders this
// session, so a background tab drawn at another size would be wrong the moment
// it is selected — and unlike tmux there is no other client whose size has to
// be negotiated with.
func (s *session) resize(cols, rows int) error {
	s.mu.Lock()
	s.opts.Cols, s.opts.Rows = cols, rows
	windows := s.windowsLocked()
	s.mu.Unlock()

	for _, win := range windows {
		if err := win.resize(cols, rows); err != nil {
			continue
		}
		s.events.Publish(WindowChanged{Kind: WindowResized, Window: win.snapshot()})
	}
	return nil
}

func (s *session) listWindows() []Window {
	s.mu.Lock()
	windows := s.windowsLocked()
	s.mu.Unlock()

	out := make([]Window, 0, len(windows))
	for _, win := range windows {
		out = append(out, win.snapshot())
	}
	return out
}

// subscribe opens the event stream and replays every window's ring behind it,
// which is this backend's first paint: the transport that just connected sees
// the screen as it stands before it sees anything new.
//
// The whole sequence runs under replayMu, so a reader publishing concurrently
// waits: its chunk is either already in the snapshot or arrives after it.
func (s *session) subscribe() (<-chan Event, func()) {
	s.replayMu.Lock()
	defer s.replayMu.Unlock()

	ch, unsubscribe := s.events.Subscribe()

	s.mu.Lock()
	windows := s.windowsLocked()
	s.mu.Unlock()

	for _, win := range windows {
		if snapshot := win.ring.Snapshot(); len(snapshot) > 0 {
			s.events.Publish(Output{At: time.Now(), WindowID: win.id, Data: snapshot})
		}
	}
	s.events.Publish(LifecycleChanged{Kind: LifecycleAttached})
	return ch, unsubscribe
}

func (s *session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	windows := s.windowsLocked()
	s.mu.Unlock()

	for _, win := range windows {
		win.close()
	}
}

func (s *session) window(windowID string) (*ptyWindow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	win, ok := s.windows[windowID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownWindow, windowID)
	}
	return win, nil
}

// windowsLocked returns the windows in tab order; the caller holds s.mu.
func (s *session) windowsLocked() []*ptyWindow {
	out := make([]*ptyWindow, 0, len(s.order))
	for _, id := range s.order {
		if win, ok := s.windows[id]; ok {
			out = append(out, win)
		}
	}
	return out
}
