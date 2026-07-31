package ptyterm

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The shell every test spawns: `sh` reads a line and echoes it back, which is
// all these tests need and is present on every platform the package supports.
func testManager(t *testing.T) *Manager {
	t.Helper()
	if !platformSupported() {
		t.Skip("PTYs are unavailable on this build or platform")
	}
	m := NewManager(ManagerOptions{Shell: []string{"/bin/sh"}})
	t.Cleanup(func() { _ = m.Stop(t.Context()) })
	return m
}

// awaitOutput reads the stream until the window's accumulated bytes contain
// want, and fails with what it did see instead.
func awaitOutput(t *testing.T, events <-chan Event, want string) string {
	t.Helper()
	var seen strings.Builder
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("stream closed before %q; saw %q", want, seen.String())
			}
			if out, isOutput := ev.(Output); isOutput {
				seen.Write(out.Data)
				if strings.Contains(seen.String(), want) {
					return seen.String()
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q; saw %q", want, seen.String())
		}
	}
}

func TestStartAttachAndEcho(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()

	started, err := m.Start(ctx, "demo", t.TempDir())
	require.NoError(t, err)
	require.True(t, started)

	again, err := m.Start(ctx, "demo", t.TempDir())
	require.NoError(t, err)
	require.False(t, again, "a live slug is left alone rather than respawned")

	windows, err := m.Attach(ctx, "demo", 100, 30)
	require.NoError(t, err)
	require.Len(t, windows, 1)
	require.Equal(t, 100, windows[0].Width)
	require.Equal(t, 30, windows[0].Height)

	events, unsubscribe, err := m.Subscribe("demo")
	require.NoError(t, err)
	defer unsubscribe()

	require.NoError(t, m.Write("demo", windows[0].ID, []byte("echo hive-pty-ok\n")))
	awaitOutput(t, events, "hive-pty-ok")
}

func TestAttachNeverSpawns(t *testing.T) {
	m := testManager(t)
	_, err := m.Attach(t.Context(), "nothing-here", 80, 24)
	require.ErrorIs(t, err, ErrNotAttached)
	require.False(t, m.HasSession("nothing-here"))
}

func TestResizeReachesTheProcess(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()

	_, err := m.Start(ctx, "sized", t.TempDir())
	require.NoError(t, err)
	windows, err := m.Attach(ctx, "sized", 80, 24)
	require.NoError(t, err)

	events, unsubscribe, err := m.Subscribe("sized")
	require.NoError(t, err)
	defer unsubscribe()

	require.NoError(t, m.Resize("sized", 132, 43))
	// The shell reads the size from the tty rather than from anything we told
	// it, so this asserts the ioctl landed, not that our bookkeeping agrees.
	require.NoError(t, m.Write("sized", windows[0].ID, []byte("stty size\n")))
	awaitOutput(t, events, "43 132")
}

func TestTabsAreIndependentProcesses(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()

	_, err := m.Start(ctx, "tabs", t.TempDir())
	require.NoError(t, err)
	first, err := m.Attach(ctx, "tabs", 80, 24)
	require.NoError(t, err)

	second, err := m.NewWindow("tabs")
	require.NoError(t, err)
	require.NotEqual(t, first[0].ID, second)

	windows := m.ListWindows("tabs")
	require.Len(t, windows, 2)
	require.Equal(t, second, activeID(windows), "a new window becomes the active one")

	events, unsubscribe, err := m.Subscribe("tabs")
	require.NoError(t, err)
	defer unsubscribe()

	require.NoError(t, m.Write("tabs", second, []byte("echo second-tab\n")))
	awaitOutput(t, events, "second-tab")

	require.NoError(t, m.CloseWindow("tabs", second))
	require.Eventually(t, func() bool { return len(m.ListWindows("tabs")) == 1 }, 5*time.Second, 20*time.Millisecond)
}

// A session whose last window exits takes the session with it, so the slug can
// be started again rather than being held by a shell that is gone.
func TestLastWindowExitEndsTheSession(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()

	_, err := m.Start(ctx, "ending", t.TempDir())
	require.NoError(t, err)
	windows, err := m.Attach(ctx, "ending", 80, 24)
	require.NoError(t, err)

	events, unsubscribe, err := m.Subscribe("ending")
	require.NoError(t, err)
	defer unsubscribe()

	require.NoError(t, m.Write("ending", windows[0].ID, []byte("exit\n")))
	require.True(t, awaitLifecycle(t, events, LifecycleExited))
	require.Eventually(t, func() bool { return !m.HasSession("ending") }, 5*time.Second, 20*time.Millisecond)
}

// Re-subscribing replays the ring, which is this backend's first paint: a
// transport that reconnects sees what the screen already held.
func TestResubscribeReplaysScrollback(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()

	_, err := m.Start(ctx, "replay", t.TempDir())
	require.NoError(t, err)
	windows, err := m.Attach(ctx, "replay", 80, 24)
	require.NoError(t, err)

	events, unsubscribe, err := m.Subscribe("replay")
	require.NoError(t, err)
	require.NoError(t, m.Write("replay", windows[0].ID, []byte("echo remembered-line\n")))
	awaitOutput(t, events, "remembered-line")
	unsubscribe()

	replayed, unsubscribe2, err := m.Subscribe("replay")
	require.NoError(t, err)
	defer unsubscribe2()
	awaitOutput(t, replayed, "remembered-line")
}

func TestKillEndsTheSession(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()

	_, err := m.Start(ctx, "doomed", t.TempDir())
	require.NoError(t, err)

	killed, err := m.Kill("doomed")
	require.NoError(t, err)
	require.True(t, killed)
	require.False(t, m.HasSession("doomed"))

	killed, err = m.Kill("doomed")
	require.NoError(t, err)
	require.False(t, killed, "a slug with no session is success with nothing done")
}

func TestSizeBounds(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()
	_, err := m.Start(ctx, "bounds", t.TempDir())
	require.NoError(t, err)

	require.ErrorIs(t, m.Resize("bounds", 0, 24), ErrInvalidSize)
	require.ErrorIs(t, m.Resize("bounds", 80, maxDimension+1), ErrInvalidSize)
	_, err = m.Attach(ctx, "bounds", 80, 0)
	require.ErrorIs(t, err, ErrInvalidSize)

	windows, err := m.Attach(ctx, "bounds", 0, 0)
	require.NoError(t, err, "0x0 is the caller saying it has measured nothing")
	require.Len(t, windows, 1)
}

func activeID(windows []Window) string {
	for _, w := range windows {
		if w.Active {
			return w.ID
		}
	}
	return ""
}

func awaitLifecycle(t *testing.T, events <-chan Event, want LifecycleKind) bool {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return false
			}
			if life, isLifecycle := ev.(LifecycleChanged); isLifecycle && life.Kind == want {
				return true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for lifecycle %q", want)
		}
	}
}
