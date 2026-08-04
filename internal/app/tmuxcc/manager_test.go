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

func probe(version string) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) { return version, nil }
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
		probe   func(context.Context, string) (string, error)
		wantErr bool
	}{
		{"current tmux", probe("tmux 3.7b\n"), false},
		{"exactly the floor", probe("tmux 3.2\n"), false},
		{"patch suffix", probe("tmux 3.2a\n"), false},
		{"prerelease", probe("tmux next-3.4\n"), false},
		{"below the floor", probe("tmux 3.1c\n"), true},
		{"far below the floor", probe("tmux 2.9\n"), true},
		{"unparseable", probe("tmux master\n"), true},
		{"tmux absent", func(context.Context, string) (string, error) {
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
	m := newTestManager(t, nil, ManagerOptions{versionProbe: func(context.Context, string) (string, error) {
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

// A tmux found outside $PATH is only useful if both the probe and the attach
// exec it; a bare "tmux" in either one is the bug this guards (ADR 0039).
func TestManagerRunsTheLocatedBinary(t *testing.T) {
	t.Parallel()

	const located = "/opt/homebrew/bin/tmux"
	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	spawn := f.factory()

	var probed string
	var attached []string
	m := newTestManager(t, nil, ManagerOptions{
		Binary: func() (string, error) { return located, nil },
		versionProbe: func(_ context.Context, binary string) (string, error) {
			probed = binary
			return "tmux 3.7b", nil
		},
		newProcess: func(opts Options) process {
			attached = append(attached, opts.Binary)
			return spawn(opts)
		},
	})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)
	require.Equal(t, located, probed)
	require.Equal(t, []string{located}, attached)
}

func TestManagerUnavailableWhenTmuxIsNotFound(t *testing.T) {
	t.Parallel()

	probes := 0
	m := newTestManager(t, nil, ManagerOptions{
		Binary: func() (string, error) { return "", errors.New("tmux not found") },
		versionProbe: func(context.Context, string) (string, error) {
			probes++
			return "tmux 3.7b", nil
		},
	})

	err := m.Available(t.Context())
	require.ErrorIs(t, err, ErrUnavailable)
	require.Contains(t, err.Error(), "tmux not found")
	require.Zero(t, probes, "nothing to probe until a binary is located")
}

func TestManagerAttachIsOneClientPerSlug(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	m := newTestManager(t, f, ManagerOptions{})

	first, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)
	require.Len(t, first, 1)
	firstClient, ok := m.Client("hive-demo")
	require.True(t, ok)

	second, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)
	require.Equal(t, first, second)
	secondClient, ok := m.Client("hive-demo")
	require.True(t, ok)
	require.Same(t, firstClient, secondClient, "the second attach reuses the client")
	require.Equal(t, 2, f.countCommands("list-windows"), "the second attach re-lists so its caller gets tmux's post-vote sizes")
}

// Re-attaching to a live client is the transport-drop path: tmux never let go,
// so nothing tears the client down, and the stream that replaces the dropped one
// has no first paint of its own to open on unless the attach makes one.
func TestManagerAttachRepaintsALiveClient(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 1 claude")
	f.setCapture("%1", "AGENT RUNNING")
	m := newTestManager(t, f, ManagerOptions{})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)

	// The stream that took the first paint took it out of the backlog with it,
	// which is what leaves the next one with nothing to open on.
	ch, unsubscribe, err := m.Subscribe("hive-demo")
	require.NoError(t, err)
	require.Equal(t, "AGENT RUNNING", outputData(collect(t, ch, lifecycleIs(LifecycleAttached)), "@1"))
	unsubscribe()

	// The re-attach may come from a differently-sized surface — the Agents
	// pane resuming in fullscreen — so the live client renegotiates the
	// caller's size before repainting, rather than keeping the stale vote,
	// and reports the size tmux settled on rather than its stale window set.
	// Height stays 1 so the repaint's snapshot stays a single line.
	f.setWindows("@1 1 %1 200 1 claude")
	windows, err := m.Attach(t.Context(), "hive-demo", 200, 55)
	require.NoError(t, err)
	require.Len(t, windows, 1)
	require.Equal(t, 2, f.countCommands("refresh-client"), "a sized re-attach renegotiates before it repaints")
	require.Equal(t, 200, windows[0].Width, "the re-attach reports tmux's post-vote size")

	resumed, stop, err := m.Subscribe("hive-demo")
	require.NoError(t, err)
	defer stop()
	require.Equal(t, "AGENT RUNNING", outputData(collect(t, resumed, outputContains("@1", "AGENT RUNNING")), "@1"))
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

// wedgedTmux is a control client that cannot be brought down: Kill leaves the
// stream open, so the reader teardown joins on never returns. It stands in for
// the shapes that hang a real one — a tmux child ignoring its kill, a detach
// write parked on a pipe nobody drains.
type wedgedTmux struct {
	*fakeTmux
	released chan struct{}
}

func (w *wedgedTmux) Kill() error { return nil }

func (w *wedgedTmux) Wait() error {
	<-w.released
	return nil
}

func TestManagerStopHonorsItsDeadline(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	// An %error keeps the fake from closing the stream the way a real detach
	// would, which is what leaves this client with no way to finish closing.
	f.failures["detach"] = "no current client"

	wedged := &wedgedTmux{fakeTmux: f, released: make(chan struct{})}
	t.Cleanup(func() { close(wedged.released) })

	m := newTestManager(t, nil, ManagerOptions{newProcess: func(Options) process { return wedged }})
	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	stopped := make(chan error, 1)
	go func() { stopped <- m.Stop(ctx) }()

	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Stop waited past its deadline on a client that cannot come down, which is what hangs App.Close")
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

// fakeTmuxCommands records one-shot tmux invocations and can fail a chosen one,
// standing in for a real server the way fakeTmux stands in for a control client.
type fakeTmuxCommands struct {
	calls    [][]string
	binaries []string
	absent   bool
	failure  error
	windows  []string
}

func (f *fakeTmuxCommands) run(_ context.Context, binary string, args ...string) ([]string, error) {
	f.calls = append(f.calls, args)
	f.binaries = append(f.binaries, binary)
	switch {
	case len(args) > 0 && args[0] == "has-session" && f.absent:
		return nil, errors.New("can't find session")
	case len(args) > 0 && args[0] == "rename-session":
		return nil, f.failure
	case len(args) > 0 && args[0] == "list-windows":
		return f.windows, f.failure
	}
	return nil, nil
}

func TestManagerRenameSessionRenamesTheLiveSession(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{}
	m := newTestManager(t, nil, ManagerOptions{
		Binary:  func() (string, error) { return "/opt/homebrew/bin/tmux", nil },
		runTmux: cmds.run,
	})

	require.NoError(t, m.RenameSession(t.Context(), "hive-demo", "hive-demo-2"))
	require.Equal(t, [][]string{
		{"has-session", "-t", "hive-demo"},
		{"rename-session", "-t", "hive-demo", "hive-demo-2"},
	}, cmds.calls)
	require.Equal(t, []string{"/opt/homebrew/bin/tmux", "/opt/homebrew/bin/tmux"}, cmds.binaries,
		"one-shot commands must run the discovered tmux, not whatever $PATH says")
}

func TestManagerRenameSessionTreatsAnAbsentSessionAsSuccess(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{absent: true}
	m := newTestManager(t, nil, ManagerOptions{runTmux: cmds.run})

	// A hive session that was never spawned, or whose tmux server restarted,
	// still has to be renamable.
	require.NoError(t, m.RenameSession(t.Context(), "hive-demo", "hive-demo-2"))
	require.Len(t, cmds.calls, 1, "no rename is attempted for a session that is not there")
}

func TestManagerRenameSessionReportsARefusedRename(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{failure: errors.New("duplicate session: hive-demo-2")}
	m := newTestManager(t, nil, ManagerOptions{runTmux: cmds.run})

	err := m.RenameSession(t.Context(), "hive-demo", "hive-demo-2")
	require.ErrorContains(t, err, "duplicate session")
}

func TestManagerRenameSessionIsANoOpWithoutUsableTmux(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{}
	m := newTestManager(t, nil, ManagerOptions{versionProbe: probe("tmux 2.9\n"), runTmux: cmds.run})

	require.NoError(t, m.RenameSession(t.Context(), "hive-demo", "hive-demo-2"))
	require.Empty(t, cmds.calls)
}

func TestManagerRenameSessionValidatesSlugs(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{}
	m := newTestManager(t, nil, ManagerOptions{runTmux: cmds.run})

	require.ErrorIs(t, m.RenameSession(t.Context(), "", "hive-demo"), ErrInvalidName)
	require.ErrorIs(t, m.RenameSession(t.Context(), "hive-demo", ""), ErrInvalidName)
	require.NoError(t, m.RenameSession(t.Context(), "hive-demo", "hive-demo"), "renaming to the same slug is nothing to do")
	require.Empty(t, cmds.calls)
}

func TestManagerListAllWindowsAsksTmuxOnceForEverySlug(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{windows: []string{
		"hive-demo @1 1 %1 120 40 claude",
		"hive-demo @2 0 %2 120 40 shell",
		"hive-other @5 1 %5 80 24 my window",
	}}
	m := newTestManager(t, nil, ManagerOptions{
		Binary:  func() (string, error) { return "/opt/homebrew/bin/tmux", nil },
		runTmux: cmds.run,
	})

	windows, err := m.ListAllWindows(t.Context(), []string{"hive-demo", "hive-other"})
	require.NoError(t, err)
	require.Equal(t, map[string][]Window{
		"hive-demo": {
			{ID: "@1", Active: true, ActivePane: "%1", Width: 120, Height: 40, Name: "claude"},
			{ID: "@2", Active: false, ActivePane: "%2", Width: 120, Height: 40, Name: "shell"},
		},
		// A window name may contain spaces, which is why the session goes first.
		"hive-other": {{ID: "@5", Active: true, ActivePane: "%5", Width: 80, Height: 24, Name: "my window"}},
	}, windows)
	// One spawn for the whole server, and no has-session probes: the sweep this
	// serves used to cost two of each per session.
	require.Equal(t, [][]string{{"list-windows", "-a", "-F", sessionWindowsFormat}}, cmds.calls)
	// The one-shot must exec the binary the probe resolved, not a bare "tmux"
	// $PATH may not have (ADR 0039).
	require.Equal(t, []string{"/opt/homebrew/bin/tmux"}, cmds.binaries)
}

func TestManagerListAllWindowsIgnoresSessionsNobodyAskedFor(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{windows: []string{
		"hive-demo @1 1 %1 120 40 claude",
		"someone-elses-session @9 1 %9 80 24 vim",
	}}
	m := newTestManager(t, nil, ManagerOptions{runTmux: cmds.run})

	windows, err := m.ListAllWindows(t.Context(), []string{"hive-demo"})
	require.NoError(t, err)
	require.Equal(t, map[string][]Window{
		"hive-demo": {{ID: "@1", Active: true, ActivePane: "%1", Width: 120, Height: 40, Name: "claude"}},
	}, windows)
}

func TestManagerListAllWindowsAnswersFromTheAttachedClient(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	// The server-wide listing is stale for an attached slug — the live client is
	// what carries the sizes tmux settled on for it.
	cmds := &fakeTmuxCommands{windows: []string{"hive-demo @1 1 %1 80 24 claude"}}
	m := newTestManager(t, f, ManagerOptions{runTmux: cmds.run})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)

	windows, err := m.ListAllWindows(t.Context(), []string{"hive-demo"})
	require.NoError(t, err)
	require.Equal(t, map[string][]Window{
		"hive-demo": {{ID: "@1", Active: true, ActivePane: "%1", Width: 120, Height: 40, Name: "claude"}},
	}, windows)
}

func TestManagerListAllWindowsTreatsAnAbsentSessionAsNoWindows(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{}
	m := newTestManager(t, nil, ManagerOptions{runTmux: cmds.run})

	windows, err := m.ListAllWindows(t.Context(), []string{"hive-demo"})
	require.NoError(t, err)
	require.Empty(t, windows)
}

func TestManagerListAllWindowsTreatsADeadServerAsNoWindows(t *testing.T) {
	t.Parallel()

	// No tmux server running at all is what this answers, and it is a normal
	// state for a sidebar full of sessions nobody has started yet.
	cmds := &fakeTmuxCommands{failure: errors.New("no server running")}
	m := newTestManager(t, nil, ManagerOptions{runTmux: cmds.run})

	windows, err := m.ListAllWindows(t.Context(), []string{"hive-demo"})
	require.NoError(t, err)
	require.Empty(t, windows)
}

func TestManagerHasSessionProbesTmux(t *testing.T) {
	t.Parallel()

	present := &fakeTmuxCommands{}
	exists, err := newTestManager(t, nil, ManagerOptions{runTmux: present.run}).HasSession(t.Context(), "hive-demo")
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, [][]string{{"has-session", "-t", "hive-demo"}}, present.calls)

	// A server that is not running, or has no such session, is what the caller
	// is asking about — not a failure to report.
	absent := &fakeTmuxCommands{absent: true}
	exists, err = newTestManager(t, nil, ManagerOptions{runTmux: absent.run}).HasSession(t.Context(), "hive-demo")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestManagerHasSessionAnswersFromTheAttachedClient(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	cmds := &fakeTmuxCommands{absent: true}
	m := newTestManager(t, f, ManagerOptions{runTmux: cmds.run})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)

	exists, err := m.HasSession(t.Context(), "hive-demo")
	require.NoError(t, err)
	require.True(t, exists, "a live control client is proof the session is there")
	require.Empty(t, cmds.calls, "a warm re-attach costs no probe")
}

func TestManagerKillSessionDropsTheClientWithTheSession(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	cmds := &fakeTmuxCommands{}
	m := newTestManager(t, f, ManagerOptions{runTmux: cmds.run})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)

	killed, err := m.KillSession(t.Context(), "hive-demo")
	require.NoError(t, err)
	require.True(t, killed)
	require.Equal(t, [][]string{{"kill-session", "-t", "hive-demo"}}, cmds.calls,
		"the live client answers the existence probe, so only the kill reaches tmux")
	// Dropped synchronously: the child exits on its own moments later, and a
	// re-attach in between must not be handed the dying client.
	_, ok := m.Client("hive-demo")
	require.False(t, ok)
}

func TestManagerKillSessionTreatsAnAbsentSessionAsNothingToDo(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{absent: true}
	m := newTestManager(t, nil, ManagerOptions{runTmux: cmds.run})

	killed, err := m.KillSession(t.Context(), "hive-demo")
	require.NoError(t, err)
	require.False(t, killed)
	require.Equal(t, [][]string{{"has-session", "-t", "hive-demo"}}, cmds.calls, "nothing is killed for a session that is not there")
}

func TestManagerHasSessionValidatesInput(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{}

	_, err := newTestManager(t, nil, ManagerOptions{runTmux: cmds.run}).HasSession(t.Context(), "")
	require.ErrorIs(t, err, ErrNotAttached)

	old := newTestManager(t, nil, ManagerOptions{versionProbe: probe("tmux 2.9\n"), runTmux: cmds.run})
	_, err = old.HasSession(t.Context(), "hive-demo")
	require.ErrorIs(t, err, ErrUnavailable)

	require.Empty(t, cmds.calls)
}

func TestManagerListAllWindowsRequiresAUsableTmux(t *testing.T) {
	t.Parallel()

	cmds := &fakeTmuxCommands{}

	// An empty set is a sidebar with nothing in it, not a bad request.
	windows, err := newTestManager(t, nil, ManagerOptions{runTmux: cmds.run}).ListAllWindows(t.Context(), nil)
	require.NoError(t, err)
	require.Empty(t, windows)

	old := newTestManager(t, nil, ManagerOptions{versionProbe: probe("tmux 2.9\n"), runTmux: cmds.run})
	_, err = old.ListAllWindows(t.Context(), []string{"hive-demo"})
	require.ErrorIs(t, err, ErrUnavailable)

	require.Empty(t, cmds.calls)
}

func TestManagerRenameSessionDropsTheClientOnTheOldSlug(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	cmds := &fakeTmuxCommands{}
	m := newTestManager(t, f, ManagerOptions{runTmux: cmds.run})

	_, err := m.Attach(t.Context(), "hive-demo", 80, 24)
	require.NoError(t, err)
	_, ok := m.Client("hive-demo")
	require.True(t, ok)

	require.NoError(t, m.RenameSession(t.Context(), "hive-demo", "hive-demo-2"))

	// The client addressed the session by its old name; leaving it registered
	// would leave every later command pointed at a name tmux dropped.
	_, ok = m.Client("hive-demo")
	require.False(t, ok)
}
