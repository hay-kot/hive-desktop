package tmuxcc

import (
	"sync"
	"time"
)

// defaultBufferBytes bounds the per-session backlog of undelivered output.
const defaultBufferBytes = 8 << 20

// subscriberQueue is zero deliberately: an event leaves the accounted backlog
// only once a subscriber has taken it, so replacing a subscriber cannot strand
// events in the channel it is closing. A re-Subscribe — the WebSocket reconnect
// path — must never hand the emulator a stream with a hole in it.
const subscriberQueue = 0

// finalDelivery bounds how long a closing broker waits for the subscriber to
// take the last lifecycle event. A draining subscriber takes it at once; a
// stalled one must not hold the pump past client teardown, and the transport's
// own socket close is the fallback signal.
const finalDelivery = 250 * time.Millisecond

type subscription struct {
	gen  uint64
	ch   chan Event
	stop chan struct{}
}

// broker fans one client's event stream out to at most one subscriber. The
// reader goroutine calls publish, which never blocks: events queue in a
// byte-bounded backlog that a pump goroutine drains into the subscriber's
// channel. With no subscriber the backlog is what replays first-paint to a
// WebSocket that connects after Attach.
//
// A terminal byte stream cannot drop-oldest without corrupting the emulator,
// so exceeding the bound is fatal: onOverflow tears the client down and the
// frontend re-attaches for a clean resync.
type broker struct {
	mu       sync.Mutex
	cond     *sync.Cond
	buf      []Event
	bytes    int
	max      int
	sub      *subscription
	gen      uint64
	closed   bool
	overflow bool

	// done releases a pump parked on a subscriber that stopped reading:
	// sync.Cond cannot wake a goroutine blocked on a channel send.
	done chan struct{}

	onOverflow   func()
	overflowOnce sync.Once
}

func newBroker(maxBytes int, onOverflow func()) *broker {
	if maxBytes <= 0 {
		maxBytes = defaultBufferBytes
	}
	b := &broker{max: maxBytes, onOverflow: onOverflow, done: make(chan struct{})}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// publish appends ev to the backlog. Only Output counts against the byte
// bound — lifecycle and window events must still reach the subscriber while
// the client is being torn down for overflow.
func (b *broker) publish(ev Event) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	size := eventBytes(ev)
	if b.overflow && size > 0 {
		b.mu.Unlock()
		return
	}
	if b.bytes+size > b.max && size > 0 {
		b.overflow = true
		b.mu.Unlock()
		b.overflowOnce.Do(func() {
			if b.onOverflow != nil {
				go b.onOverflow()
			}
		})
		return
	}
	b.buf = append(b.buf, ev)
	b.bytes += size
	b.mu.Unlock()
	b.cond.Broadcast()
}

// depth reports the buffered output bytes awaiting delivery.
func (b *broker) depth() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bytes
}

// subscribe replaces any existing subscriber, closing its channel, and returns
// a channel that starts with the buffered backlog. The unsubscribe func is
// generation-scoped: a stale one cannot detach its replacement.
func (b *broker) subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	b.gen++
	gen := b.gen
	if b.sub != nil {
		close(b.sub.stop)
	}
	sub := &subscription{gen: gen, ch: make(chan Event, subscriberQueue), stop: make(chan struct{})}
	b.sub = sub
	b.mu.Unlock()
	b.cond.Broadcast()

	go b.pump(sub)
	return sub.ch, func() { b.unsubscribe(gen) }
}

func (b *broker) unsubscribe(gen uint64) {
	b.mu.Lock()
	if b.sub != nil && b.sub.gen == gen {
		close(b.sub.stop)
		b.sub = nil
		b.gen++
	}
	b.mu.Unlock()
	b.cond.Broadcast()
}

// close drains what a live subscriber can still take and closes its channel.
// This is how a WebSocket write pump learns the client is gone.
func (b *broker) close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	close(b.done)
	b.mu.Unlock()
	b.cond.Broadcast()
}

// pump delivers the head of the backlog and only then removes it, so an event
// the subscriber never took stays accounted and replays to its replacement.
func (b *broker) pump(sub *subscription) {
	defer close(sub.ch)
	for {
		b.mu.Lock()
		for len(b.buf) == 0 && b.gen == sub.gen && !b.closed {
			b.cond.Wait()
		}
		if b.gen != sub.gen || len(b.buf) == 0 {
			b.mu.Unlock()
			return
		}
		ev := b.buf[0]
		closing := b.closed
		b.mu.Unlock()

		switch {
		case closing && eventBytes(ev) > 0:
			// Undelivered output is output the re-attach's first paint redraws
			// anyway, so a closing broker never parks on it.
			select {
			case sub.ch <- ev:
			default:
			}
		case closing:
			// The lifecycle event that says *why* the stream ended has no such
			// replacement — overflow is exactly the case where the subscriber
			// is behind — so it waits, bounded.
			if !b.sendFinal(sub, ev) {
				return
			}
		default:
			select {
			case sub.ch <- ev:
			case <-sub.stop:
				return
			case <-b.done:
				// Left in the backlog on purpose: the next pass re-reads it
				// under the closing rules above.
				continue
			}
		}
		if !b.advance(sub.gen, ev) {
			return
		}
	}
}

// advance drops the delivered head. A generation change means a replacement
// pump owns the backlog now, and this one must not consume from it.
func (b *broker) advance(gen uint64, ev Event) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.gen != gen || len(b.buf) == 0 {
		return false
	}
	b.buf[0] = nil
	b.buf = b.buf[1:]
	b.bytes -= eventBytes(ev)
	return true
}

func (b *broker) sendFinal(sub *subscription, ev Event) bool {
	timer := time.NewTimer(finalDelivery)
	defer timer.Stop()
	select {
	case sub.ch <- ev:
		return true
	case <-sub.stop:
		return false
	case <-timer.C:
		return false
	}
}

func eventBytes(ev Event) int {
	if o, ok := ev.(Output); ok {
		return len(o.Data)
	}
	return 0
}
