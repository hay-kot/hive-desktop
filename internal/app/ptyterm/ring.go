package ptyterm

import "sync"

// ring is a window's replay buffer: the last max bytes the PTY produced, kept
// so a re-attach can restore the screen.
//
// This is what stands in for tmux's capture-pane, and it is not the same thing.
// capture-pane re-renders the visible grid from tmux's own emulator, so a
// snapshot is always a coherent screen. A byte ring is the raw stream with its
// head cut off, so a replay can begin inside an escape sequence and can carry
// alternate-screen transitions the terminal will re-run. It restores scrollback
// the pane never had to redraw, which capture-pane cannot, and it costs a
// process to hold it.
type ring struct {
	mu   sync.Mutex
	buf  []byte
	max  int
	full bool
	head int
}

func newRing(max int) *ring {
	if max <= 0 {
		max = defaultReplayBytes
	}
	return &ring{buf: make([]byte, 0, min(max, 64<<10)), max: max}
}

func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(p) >= r.max {
		// The write alone overruns the ring: keep its tail and start over.
		r.buf = append(r.buf[:0], p[len(p)-r.max:]...)
		r.full = true
		r.head = 0
		return len(p), nil
	}
	if !r.full {
		r.buf = append(r.buf, p...)
		if len(r.buf) >= r.max {
			r.full = true
			r.head = len(r.buf) - r.max
			r.buf = r.buf[r.head:]
			r.head = 0
		}
		return len(p), nil
	}
	// Steady state: overwrite from the head, wrapping once.
	n := copy(r.buf[r.head:], p)
	if n < len(p) {
		copy(r.buf, p[n:])
	}
	r.head = (r.head + len(p)) % r.max
	return len(p), nil
}

// Snapshot returns the buffered bytes oldest-first.
func (r *ring) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return append([]byte(nil), r.buf...)
	}
	out := make([]byte, 0, len(r.buf))
	out = append(out, r.buf[r.head:]...)
	return append(out, r.buf[:r.head]...)
}
