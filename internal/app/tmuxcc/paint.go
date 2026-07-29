package tmuxcc

import (
	"sync"
	"time"
)

// paintGate holds live output per pane until that pane's snapshot resolves,
// then replays it behind the snapshot. Emitting under the same lock the reader
// appends under is what keeps the two in order.
//
// Before the attach sequence completes every pane is held; afterwards only a
// pane being snapshotted is, which is how a window created later gets a first
// paint without stalling the rest of the session.
type paintGate struct {
	mu    sync.Mutex
	emit  func(pane string, data []byte, at time.Time)
	open  bool
	live  map[string]bool
	held  map[string]bool
	buf   map[string][]byte
	marks map[string]int
}

func newPaintGate(emit func(pane string, data []byte, at time.Time)) *paintGate {
	return &paintGate{
		emit:  emit,
		live:  map[string]bool{},
		held:  map[string]bool{},
		buf:   map[string][]byte{},
		marks: map[string]int{},
	}
}

func (g *paintGate) route(pane string, data []byte, at time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held[pane] || (!g.open && !g.live[pane]) {
		g.buf[pane] = append(g.buf[pane], data...)
		return
	}
	g.emit(pane, data, at)
}

// hold starts buffering a pane that has never been painted and reports whether
// it took the gate. Everything from here to mark is discarded as already inside
// the snapshot the caller is about to request.
func (g *paintGate) hold(pane string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.live[pane] || g.held[pane] {
		return false
	}
	g.held[pane] = true
	return true
}

// mark holds the pane and records the point the snapshot command was sent at:
// output buffered before it is inside the snapshot, output after it is replayed
// behind the snapshot.
func (g *paintGate) mark(pane string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.held[pane] = true
	g.marks[pane] = len(g.buf[pane])
}

func (g *paintGate) release(pane string, painted []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if len(painted) > 0 {
		g.emit(pane, painted, now)
	}
	if tail := g.buf[pane][g.marks[pane]:]; len(tail) > 0 {
		g.emit(pane, tail, now)
	}
	delete(g.buf, pane)
	delete(g.marks, pane)
	delete(g.held, pane)
	g.live[pane] = true
}

func (g *paintGate) openAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for pane, data := range g.buf {
		if len(data) > 0 {
			g.emit(pane, data, now)
		}
	}
	g.buf = map[string][]byte{}
	g.marks = map[string]int{}
	g.open = true
}
