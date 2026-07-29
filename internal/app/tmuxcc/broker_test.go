package tmuxcc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func outputEvent(window, data string) Output {
	return Output{At: time.Now(), WindowID: window, PaneID: "%1", Data: []byte(data), Render: true}
}

func requireOutput(t *testing.T, ev Event) Output {
	t.Helper()
	out, ok := ev.(Output)
	require.True(t, ok, "expected Output, got %#v", ev)
	return out
}

func requireLifecycle(t *testing.T, ev Event) LifecycleChanged {
	t.Helper()
	lc, ok := ev.(LifecycleChanged)
	require.True(t, ok, "expected LifecycleChanged, got %#v", ev)
	return lc
}

func receive(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		require.True(t, ok, "channel closed")
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return nil
	}
}

// Output produced before anyone subscribes is what carries first paint to a
// WebSocket that connects after Attach.
func TestBrokerReplaysBacklogOnSubscribe(t *testing.T) {
	t.Parallel()

	b := newBroker(0, nil)
	b.publish(outputEvent("@1", "first"))
	b.publish(outputEvent("@1", "second"))
	require.Equal(t, len("firstsecond"), b.depth())

	ch, unsubscribe := b.subscribe()
	defer unsubscribe()

	require.Equal(t, outputEvent("@1", "first").Data, requireOutput(t, receive(t, ch)).Data)
	require.Equal(t, outputEvent("@1", "second").Data, requireOutput(t, receive(t, ch)).Data)
}

func TestBrokerSubscribeReplacesPrevious(t *testing.T) {
	t.Parallel()

	b := newBroker(0, nil)
	first, _ := b.subscribe()
	second, unsubscribe := b.subscribe()
	defer unsubscribe()

	select {
	case _, ok := <-first:
		require.False(t, ok, "the replaced channel closes")
	case <-time.After(2 * time.Second):
		t.Fatal("the replaced channel stayed open")
	}

	b.publish(outputEvent("@1", "live"))
	require.Equal(t, []byte("live"), requireOutput(t, receive(t, second)).Data)
}

// A stale unsubscribe belongs to a generation that is already gone; acting on
// it would detach the subscriber that replaced it.
func TestBrokerStaleUnsubscribeIsInert(t *testing.T) {
	t.Parallel()

	b := newBroker(0, nil)
	_, staleUnsubscribe := b.subscribe()
	current, unsubscribe := b.subscribe()
	defer unsubscribe()

	staleUnsubscribe()

	b.publish(outputEvent("@1", "live"))
	require.Equal(t, []byte("live"), requireOutput(t, receive(t, current)).Data)
}

func TestBrokerUnsubscribeClosesChannel(t *testing.T) {
	t.Parallel()

	b := newBroker(0, nil)
	ch, unsubscribe := b.subscribe()
	unsubscribe()

	select {
	case _, ok := <-ch:
		require.False(t, ok)
	case <-time.After(2 * time.Second):
		t.Fatal("channel stayed open after unsubscribe")
	}
}

// A terminal stream cannot drop bytes, so exceeding the bound tears the
// client down instead — the frontend re-attaches for a clean resync.
func TestBrokerOverflowIsFatal(t *testing.T) {
	t.Parallel()

	overflowed := make(chan struct{})
	b := newBroker(64, func() { close(overflowed) })

	b.publish(outputEvent("@1", "0123456789"))
	b.publish(outputEvent("@1", string(make([]byte, 128))))

	select {
	case <-overflowed:
	case <-time.After(2 * time.Second):
		t.Fatal("overflow was not reported")
	}

	// Lifecycle events still get through: the teardown overflow triggers has
	// to reach the subscriber.
	ch, unsubscribe := b.subscribe()
	defer unsubscribe()
	b.publish(LifecycleChanged{Kind: LifecycleExited, Message: "overflow"})

	require.Equal(t, outputEvent("@1", "0123456789").Data, requireOutput(t, receive(t, ch)).Data)
	require.Equal(t,
		LifecycleChanged{Kind: LifecycleExited, Message: "overflow"},
		requireLifecycle(t, receive(t, ch)))
}

// Overflow closes the broker with the subscriber queue full — by definition,
// since a full queue is what let the backlog grow. The exit reason still has to
// arrive, or the frontend sees a bare socket close with nothing to report.
func TestBrokerCloseDeliversLifecycleBehindAFullQueue(t *testing.T) {
	t.Parallel()

	b := newBroker(0, nil)
	ch, unsubscribe := b.subscribe()
	defer unsubscribe()

	for range subscriberQueue * 2 {
		b.publish(outputEvent("@1", "x"))
	}
	b.publish(LifecycleChanged{Kind: LifecycleExited, Message: "overflow"})
	b.close()

	for {
		ev, ok := <-ch
		if !ok {
			t.Fatal("the channel closed before the exit reason arrived")
		}
		if lc, isLifecycle := ev.(LifecycleChanged); isLifecycle {
			require.Equal(t, LifecycleChanged{Kind: LifecycleExited, Message: "overflow"}, lc)
			return
		}
	}
}

func TestBrokerCloseDrainsThenClosesChannel(t *testing.T) {
	t.Parallel()

	b := newBroker(0, nil)
	ch, unsubscribe := b.subscribe()
	defer unsubscribe()

	b.publish(LifecycleChanged{Kind: LifecycleExited, Message: "detached"})
	b.close()

	require.Equal(t, LifecycleExited, requireLifecycle(t, receive(t, ch)).Kind)
	select {
	case _, ok := <-ch:
		require.False(t, ok)
	case <-time.After(2 * time.Second):
		t.Fatal("channel stayed open after close")
	}
}

// The reader goroutine is the one calling publish; if it ever blocked, a
// pending command's %end reply would deadlock behind it.
func TestBrokerPublishNeverBlocks(t *testing.T) {
	t.Parallel()

	b := newBroker(1<<20, nil)
	_, unsubscribe := b.subscribe()
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		for range subscriberQueue * 4 {
			b.publish(outputEvent("@1", "x"))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked on an unread subscriber")
	}
}
