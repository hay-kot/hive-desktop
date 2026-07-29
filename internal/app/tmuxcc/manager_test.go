//go:build !server

package tmuxcc

import (
	"context"
	"errors"
	"strings"
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
	f.setWindows("@1 1 %1 claude")
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
	f.setWindows("@1 1 %1 claude")
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
