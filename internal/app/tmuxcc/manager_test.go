//go:build !server

package tmuxcc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func probe(version string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return version, nil }
}

func newTestManager(t *testing.T, f *fakeTmux, opts ManagerOptions) *Manager {
	t.Helper()
	if opts.versionProbe == nil {
		opts.versionProbe = probe("tmux 3.7b\n")
	}
	if f != nil {
		opts.newProcess = f.factory()
	}
	m := NewManager(t.Context(), opts)
	t.Cleanup(func() { _ = m.Stop(context.WithoutCancel(t.Context())) })
	return m
}

func TestManagerAvailabilityGate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		probe   func(context.Context) (string, error)
		wantErr bool
	}{
		{"current tmux", probe("tmux 3.7b\n"), false},
		{"exactly the floor", probe("tmux 3.2\n"), false},
		{"patch suffix", probe("tmux 3.2a\n"), false},
		{"prerelease", probe("tmux next-3.4\n"), false},
		{"below the floor", probe("tmux 3.1c\n"), true},
		{"far below the floor", probe("tmux 2.9\n"), true},
		{"unparseable", probe("tmux master\n"), true},
		{"tmux absent", func(context.Context) (string, error) {
			return "", errors.New(`exec: "tmux": executable file not found in $PATH`)
		}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := newTestManager(t, nil, ManagerOptions{versionProbe: tc.probe})
			err := m.Available(t.Context())
			if tc.wantErr {
				require.ErrorIs(t, err, ErrUnavailable)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestManagerCachesASuccessfulProbe(t *testing.T) {
	t.Parallel()

	calls := 0
	m := newTestManager(t, nil, ManagerOptions{versionProbe: func(context.Context) (string, error) {
		calls++
		return "tmux 3.7b", nil
	}})

	require.NoError(t, m.Available(t.Context()))
	require.NoError(t, m.Available(t.Context()))
	require.Equal(t, 1, calls)
}

func TestManagerAttachValidatesInput(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	m := newTestManager(t, f, ManagerOptions{})

	_, err := m.Attach(t.Context(), "hive-demo", 0, 24)
	require.ErrorIs(t, err, ErrInvalidSize)

	_, err = m.Attach(t.Context(), "hive-demo", 80, maxDimension+1)
	require.ErrorIs(t, err, ErrInvalidSize)

	_, err = m.Attach(t.Context(), "", 80, 24)
	require.ErrorIs(t, err, ErrNotAttached)

	require.Empty(t, f.sentCommands(), "a rejected attach never spawns tmux")
}

func TestManagerAttachIsOneClientPerSlug(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	m := newTestManager(t, f, ManagerOptions{})

	first, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, f.countCommands("list-windows"), "the second attach reuses the client")
}

func TestManagerSubscribeRequiresAnAttachedClient(t *testing.T) {
	t.Parallel()

	m := newTestManager(t, nil, ManagerOptions{})
	_, _, err := m.Subscribe("nope")
	require.ErrorIs(t, err, ErrNotAttached)
}

func TestManagerDetachFreesTheSlug(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	m := newTestManager(t, f, ManagerOptions{})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)

	require.NoError(t, m.Detach(t.Context(), "hive-demo"))
	_, ok := m.Client("hive-demo")
	require.False(t, ok)

	require.NoError(t, m.Detach(t.Context(), "hive-demo"), "detaching twice is a no-op")
}

// v1 has no partial resync: a backlog past the bound kills the client and the
// frontend re-attaches for a fresh first paint.
func TestManagerBrokerOverflowTearsDownTheClient(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	m := newTestManager(t, f, ManagerOptions{BufferBytes: 4 << 10})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)

	ch, unsubscribe, err := m.Subscribe("hive-demo")
	require.NoError(t, err)
	defer unsubscribe()

	payload := strings.Repeat("x", 1024)
	for range subscriberQueue + 32 {
		f.emit("%output %1 " + payload)
	}

	events := collect(t, ch, lifecycleIs(LifecycleExited))
	require.Equal(t, "overflow", lastLifecycle(t, events).Message)

	require.Eventually(t, func() bool {
		_, ok := m.Client("hive-demo")
		return !ok
	}, 2*time.Second, 5*time.Millisecond, "the slug is freed for a fresh attach")
}

// Teardown can beat registration: a control stream that ends as the last
// capture-pane is answered runs OnExit while Attach is still returning. The
// manager used to store the corpse afterwards, and the slug stayed poisoned
// until an explicit detach.
func TestManagerDropsAClientThatDiedDuringAttach(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	f.setCapture("%1", "ready")

	gate := &gatedMetrics{blocked: make(chan struct{}), release: make(chan struct{})}
	m := newTestManager(t, f, ManagerOptions{Metrics: gate})

	type attachResult struct {
		windows []Window
		err     error
	}
	done := make(chan attachResult, 1)
	go func() {
		windows, err := m.Attach(t.Context(), "hive-demo", 80, 24)
		done <- attachResult{windows, err}
	}()

	// The attach goroutine is parked inside the first paint, so the whole
	// teardown — OnExit included — runs before Attach returns.
	<-gate.blocked
	f.closeStreams()
	require.Eventually(t, func() bool { return f.killed() > 0 }, 2*time.Second, time.Millisecond)
	close(gate.release)

	got := <-done
	require.ErrorIs(t, got.err, ErrNotAttached)
	require.Nil(t, got.windows)

	_, ok := m.Client("hive-demo")
	require.False(t, ok, "a dead client never owns the slug")
}

// A client that exits after its slug was re-attached must not take the client
// that replaced it down with it.
func TestManagerStaleExitDoesNotEvictItsReplacement(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	m := newTestManager(t, f, ManagerOptions{})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)

	mc, ok := m.managed("hive-demo")
	require.True(t, ok)
	m.remove("hive-demo", mc.gen-1)

	_, ok = m.Client("hive-demo")
	require.True(t, ok, "an exit from an earlier generation is inert")
}

// A WebSocket peer that stopped draining parks the pump on a channel send,
// where Cond.Broadcast cannot reach it. Nothing may outlive Stop.
func TestManagerStopReleasesAStalledSubscriber(t *testing.T) {
	// Deliberately not parallel: it counts this package's live goroutines.
	before := clientGoroutines()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	m := newTestManager(t, f, ManagerOptions{})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)

	_, _, err = m.Subscribe("hive-demo")
	require.NoError(t, err)
	for range 8 {
		f.emit(`%output %1 x\015\012`)
	}

	require.NoError(t, m.Stop(t.Context()))
	require.Eventually(t, func() bool { return clientGoroutines() <= before }, 5*time.Second, 5*time.Millisecond,
		"the pump, the reader and the command worker must all exit")
}

// gatedMetrics parks the first output it is told about, which is the attach
// sequence's first paint. It is how a test gets inside the window between a
// client finishing its attach and the manager registering it.
type gatedMetrics struct {
	fakeMetrics
	once    sync.Once
	blocked chan struct{}
	release chan struct{}
}

func (m *gatedMetrics) BytesStreamed(session, window string, n int) {
	m.once.Do(func() {
		close(m.blocked)
		<-m.release
	})
	m.fakeMetrics.BytesStreamed(session, window, n)
}

func TestManagerStopClosesEveryClient(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	m := newTestManager(t, f, ManagerOptions{})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)

	ch, unsubscribe, err := m.Subscribe("hive-demo")
	require.NoError(t, err)
	defer unsubscribe()

	require.NoError(t, m.Stop(t.Context()))
	require.NoError(t, m.Stop(t.Context()), "Stop is idempotent")

	_, ok := m.Client("hive-demo")
	require.False(t, ok)

	// The subscription channel closing is what shuts a WebSocket write pump
	// down; a hung reader would hang App.Close.
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("subscription stayed open after Stop")
		}
	}
}

func TestManagerAttachAfterStopIsUnavailable(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	m := newTestManager(t, f, ManagerOptions{})

	require.NoError(t, m.Stop(t.Context()))
	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.ErrorIs(t, err, ErrUnavailable)
}
