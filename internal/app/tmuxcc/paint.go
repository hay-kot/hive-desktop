package tmuxcc

import (
	"sync"
	"time"
)

// paintGate holds live output per pane until that pane's snapshot resolves,
// then replays it behind the snapshot. Emitting under the same lock the reader
// appends under is what keeps the two in order.
type paintGate struct {
	mu    sync.Mutex
	emit  func(pane string, data []byte, at time.Time)
	open  bool
	live  map[string]bool
	buf   map[string][]byte
	marks map[string]int
}

func newPaintGate(emit func(pane string, data []byte, at time.Time)) *paintGate {
	return &paintGate{
		emit:  emit,
		live:  map[string]bool{},
		buf:   map[string][]byte{},
		marks: map[string]int{},
	}
}

func (g *paintGate) route(pane string, data []byte, at time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.open || g.live[pane] {
		g.emit(pane, data, at)
		return
	}
	g.buf[pane] = append(g.buf[pane], data...)
}

func (g *paintGate) mark(pane string) {
	g.mu.Lock()
	defer g.mu.Unlock()
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
