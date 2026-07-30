package tmuxcc

import "sync"

// Window is the app-side model of one tmux window.
type Window struct {
	ID         string // tmux window id, e.g. "@275"
	Name       string
	Active     bool
	ActivePane string // tmux pane id, e.g. "%512"
	// Width and Height are tmux's own size for this window — whichever attached
	// client tmux's window-size option picked, not necessarily ours. A renderer
	// that draws at any other size mangles the pane's cursor-addressed output.
	// 0 means not known yet.
	Width  int
	Height int
}

// controller holds the window set of one attached session and derives events
// from notifications. Only the active pane of a window is rendered; other
// panes' output is still consumed so tmux never stalls on us.
type controller struct {
	mu      sync.Mutex
	order   []string
	windows map[string]Window
	panes   map[string]string // pane id -> window id
}

func newController() *controller {
	return &controller{windows: map[string]Window{}, panes: map[string]string{}}
}

// apply folds one notification into the window set and returns the events it
// produces. Output is routed by the client instead: it is buffered by pane
// until first-paint completes, at which point the window set is known.
// apply is called on the reader goroutine and never blocks.
func (c *controller) apply(n Notification) []Event {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch v := n.(type) {
	case WindowAddNotification:
		if _, ok := c.windows[v.Window]; ok {
			return nil
		}
		w := Window{ID: v.Window}
		c.windows[v.Window] = w
		c.order = append(c.order, v.Window)
		return []Event{WindowChanged{Kind: WindowAdded, Window: w}}

	case WindowCloseNotification:
		w, ok := c.windows[v.Window]
		if !ok {
			return nil
		}
		c.removeLocked(v.Window)
		return []Event{WindowChanged{Kind: WindowClosed, Window: w}}

	case WindowRenamedNotification:
		w, ok := c.windows[v.Window]
		if !ok {
			return nil
		}
		w.Name = v.Name
		c.windows[v.Window] = w
		return []Event{WindowChanged{Kind: WindowRenamed, Window: w}}

	case WindowPaneChanged:
		w, ok := c.windows[v.Window]
		if !ok {
			return nil
		}
		w.ActivePane = v.Pane
		c.windows[v.Window] = w
		c.panes[v.Pane] = v.Window
		return []Event{WindowChanged{Kind: WindowActiveChanged, Window: w}}

	case SessionWindowChanged:
		if _, ok := c.windows[v.Window]; !ok {
			return nil
		}
		c.setActiveLocked(v.Window)
		return []Event{WindowChanged{Kind: WindowActiveChanged, Window: c.windows[v.Window]}}

	case LayoutChanged:
		w, ok := c.windows[v.Window]
		if !ok || v.Width == 0 || v.Height == 0 || (w.Width == v.Width && w.Height == v.Height) {
			return nil
		}
		w.Width, w.Height = v.Width, v.Height
		c.windows[v.Window] = w
		return []Event{WindowChanged{Kind: WindowResized, Window: w}}

	case PauseNotification:
		return []Event{LifecycleChanged{Kind: LifecyclePaused, WindowID: c.panes[v.Pane]}}

	case ContinueNotification:
		return []Event{LifecycleChanged{Kind: LifecycleResumed, WindowID: c.panes[v.Pane]}}

	default:
		return nil
	}
}

// reconcile replaces the window set with the authoritative list-windows
// snapshot and reports what changed. One window yields at most one event: every
// window event carries the whole window, so a consumer reads the current size
// off whichever kind it gets.
func (c *controller) reconcile(next []Window) []Event {
	c.mu.Lock()
	defer c.mu.Unlock()

	var events []Event
	seen := make(map[string]bool, len(next))
	for _, w := range next {
		seen[w.ID] = true
		prev, existed := c.windows[w.ID]
		c.storeLocked(w)
		switch {
		case !existed:
			events = append(events, WindowChanged{Kind: WindowAdded, Window: w})
		case prev.Name != w.Name:
			events = append(events, WindowChanged{Kind: WindowRenamed, Window: w})
		case prev.Active != w.Active || prev.ActivePane != w.ActivePane:
			events = append(events, WindowChanged{Kind: WindowActiveChanged, Window: w})
		case prev.Width != w.Width || prev.Height != w.Height:
			events = append(events, WindowChanged{Kind: WindowResized, Window: w})
		}
	}
	for _, id := range append([]string(nil), c.order...) {
		if seen[id] {
			continue
		}
		w := c.windows[id]
		c.removeLocked(id)
		events = append(events, WindowChanged{Kind: WindowClosed, Window: w})
	}
	return events
}

// set installs the initial window set without deriving events — Attach returns
// the snapshot to its caller directly.
func (c *controller) set(next []Window) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range next {
		c.storeLocked(w)
	}
}

// Windows returns a snapshot of the current window set in tmux index order.
func (c *controller) Windows() []Window {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Window, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.windows[id])
	}
	return out
}

// windowForPane resolves a pane to the window that owns it. Compare the
// result's ActivePane to decide whether that pane is the one being rendered.
func (c *controller) windowForPane(pane string) (Window, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.windows[c.panes[pane]]
	return w, ok
}

func (c *controller) byID(id string) (Window, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.windows[id]
	return w, ok
}

// storeLocked keeps the pane index additive: a pane that stops being the one
// rendered still resolves to its window, so its output is drained rather than
// dropped as unroutable. tmux never reuses a pane id.
func (c *controller) storeLocked(w Window) {
	if _, existed := c.windows[w.ID]; !existed {
		c.order = append(c.order, w.ID)
	}
	c.windows[w.ID] = w
	if w.ActivePane != "" {
		c.panes[w.ActivePane] = w.ID
	}
}

func (c *controller) removeLocked(id string) {
	w := c.windows[id]
	delete(c.panes, w.ActivePane)
	delete(c.windows, id)
	for i, existing := range c.order {
		if existing == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

func (c *controller) setActiveLocked(id string) {
	for wid, w := range c.windows {
		active := wid == id
		if w.Active != active {
			w.Active = active
			c.windows[wid] = w
		}
	}
}
